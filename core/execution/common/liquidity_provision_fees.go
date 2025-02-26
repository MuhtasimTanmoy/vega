// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package common

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/liquidity/v2"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/logging"

	"golang.org/x/exp/constraints"
)

func (m *MarketLiquidity) NewTransfer(partyID string, transferType types.TransferType, amount *num.Uint) *types.Transfer {
	return &types.Transfer{
		Owner: partyID,
		Amount: &types.FinancialAmount{
			Asset:  m.asset,
			Amount: amount.Clone(),
		},
		Type:      transferType,
		MinAmount: amount.Clone(),
		Market:    m.marketID,
	}
}

type FeeTransfer struct {
	transfers         []*types.Transfer
	totalFeesPerParty map[string]*num.Uint
}

func NewFeeTransfer(transfers []*types.Transfer, totalFeesPerParty map[string]*num.Uint) FeeTransfer {
	return FeeTransfer{
		transfers:         transfers,
		totalFeesPerParty: totalFeesPerParty,
	}
}

func (ft FeeTransfer) Transfers() []*types.Transfer {
	return ft.transfers
}

func (ft FeeTransfer) TotalFeesAmountPerParty() map[string]*num.Uint {
	return ft.totalFeesPerParty
}

// AllocateFees distributes fee from a market fee account to LP fee accounts.
func (m *MarketLiquidity) AllocateFees(ctx context.Context) error {
	acc, err := m.collateral.GetMarketLiquidityFeeAccount(m.marketID, m.asset)
	if err != nil {
		return fmt.Errorf("failed to get market liquidity fee account: %w", err)
	}

	// We can't distribute any share when no balance.
	if acc.Balance.IsZero() {
		return nil
	}

	// Get equity like shares per party.
	sharesPerLp := m.equityShares.AllShares()
	if len(sharesPerLp) == 0 {
		return nil
	}

	scoresPerLp := m.liquidityEngine.GetAverageLiquidityScores()
	// Multiplies each equity like share with corresponding score.
	updatedShares := m.updateSharesWithLiquidityScores(sharesPerLp, scoresPerLp)

	feeTransfer := m.fee.BuildLiquidityFeeAllocationTransfer(updatedShares, acc)
	if feeTransfer == nil {
		return nil
	}

	ledgerMovements, err := m.transferFees(ctx, feeTransfer)
	if err != nil {
		return fmt.Errorf("failed to transfer fees: %w", err)
	}

	m.liquidityEngine.RegisterAllocatedFeesPerParty(feeTransfer.TotalFeesAmountPerParty())

	if len(ledgerMovements) > 0 {
		m.broker.Send(events.NewLedgerMovements(ctx, ledgerMovements))
	}

	return nil
}

func (m *MarketLiquidity) processBondPenalties(
	ctx context.Context,
	partyIDs []string,
	penaltiesPerParty map[string]*liquidity.SlaPenalty,
) {
	ledgerMovements := make([]*types.LedgerMovement, 0, len(partyIDs))

	for _, partyID := range partyIDs {
		penalty := penaltiesPerParty[partyID]

		provision := m.liquidityEngine.LiquidityProvisionByPartyID(partyID)

		// bondPenalty x commitmentAmount.
		amount := penalty.Bond.Mul(provision.CommitmentAmount.ToDecimal())
		amountUint, _ := num.UintFromDecimal(amount)

		if amountUint.IsZero() {
			continue
		}

		transfer := m.NewTransfer(partyID, types.TransferTypeSLAPenaltyBondApply, amountUint)

		bondLedgerMovement, err := m.bondUpdate(ctx, transfer)
		if err != nil {
			m.log.Panic("failed to apply SLA penalties to bond account", logging.Error(err))
		}

		ledgerMovements = append(ledgerMovements, bondLedgerMovement)
	}

	if len(ledgerMovements) > 0 {
		m.broker.Send(events.NewLedgerMovements(ctx, ledgerMovements))
	}
}

func (m *MarketLiquidity) getAccruedPerPartyAndTotalFees(partyIDs []string) (map[string]*num.Uint, *num.Uint) {
	perParty := map[string]*num.Uint{}
	total := num.UintZero()

	for _, partyID := range partyIDs {
		liquidityFeeAcc, err := m.collateral.GetPartyLiquidityFeeAccount(m.marketID, partyID, m.asset)
		if err != nil {
			m.log.Panic("failed to get party liquidity fee account", logging.Error(err))
		}

		perParty[partyID] = liquidityFeeAcc.Balance.Clone()
		total.AddSum(liquidityFeeAcc.Balance)
	}

	return perParty, total
}

func (m *MarketLiquidity) distributeFeesAndCalculateBonuses(
	ctx context.Context,
	partyIDs []string,
	slaPenalties liquidity.SlaPenalties,
) map[string]num.Decimal {
	perPartAccruedFees, totalAccruedFees := m.getAccruedPerPartyAndTotalFees(partyIDs)

	allTransfers := FeeTransfer{
		transfers:         []*types.Transfer{},
		totalFeesPerParty: perPartAccruedFees,
	}

	bonusPerParty := map[string]num.Decimal{}
	totalBonuses := num.DecimalZero()

	for _, partyID := range partyIDs {
		accruedFeeAmount := perPartAccruedFees[partyID]

		// if all parties have a full penalty then transfer all accrued fees to insurance pool.
		if slaPenalties.AllPartiesHaveFullFeePenalty && !accruedFeeAmount.IsZero() {
			transfer := m.NewTransfer(partyID, types.TransferTypeSLAPenaltyLpFeeApply, accruedFeeAmount)
			allTransfers.transfers = append(allTransfers.transfers, transfer)
			continue
		}

		penalty := slaPenalties.PenaltiesPerParty[partyID]
		oneMinusPenalty := num.DecimalOne().Sub(penalty.Fee)

		// transfers fees after penalty is applied.
		// (1-feePenalty) x accruedFeeAmount.
		netDistributionAmount := oneMinusPenalty.Mul(accruedFeeAmount.ToDecimal())
		netDistributionAmountUint, _ := num.UintFromDecimal(netDistributionAmount)

		if !netDistributionAmountUint.IsZero() {
			netFeeDistributeTransfer := m.NewTransfer(partyID, types.TransferTypeLiquidityFeeNetDistribute, netDistributionAmountUint)
			allTransfers.transfers = append(allTransfers.transfers, netFeeDistributeTransfer)
		}

		// transfer unpaid accrued fee to bonus account
		// accruedFeeAmount - netDistributionAmountUint
		unpaidFees := num.UintZero().Sub(accruedFeeAmount, netDistributionAmountUint)
		if !unpaidFees.IsZero() {
			unpaidFeesTransfer := m.NewTransfer(partyID, types.TransferTypeLiquidityFeeUnpaidCollect, unpaidFees)
			allTransfers.transfers = append(allTransfers.transfers, unpaidFeesTransfer)
		}

		bonus := num.DecimalZero()
		// this is just to avoid panic.
		if !totalAccruedFees.IsZero() {
			// calculate bonus.
			// (1-feePenalty) x (accruedFeeAmount/totalAccruedFees).
			bonus = oneMinusPenalty.Mul(accruedFeeAmount.ToDecimal().Div(totalAccruedFees.ToDecimal()))
		}

		totalBonuses = totalBonuses.Add(bonus)
		bonusPerParty[partyID] = bonus
	}

	m.marketActivityTracker.UpdateFeesFromTransfers(m.asset, m.marketID, allTransfers.transfers)

	// transfer all the fees.
	ledgerMovements, err := m.transferFees(ctx, allTransfers)
	if err != nil {
		m.log.Panic("failed to transfer fees from LP's fees accounts", logging.Error(err))
	}

	if len(ledgerMovements) > 0 {
		m.broker.Send(events.NewLedgerMovements(ctx, ledgerMovements))
	}

	if !totalBonuses.IsZero() {
		// normalize bonuses.
		for party, bonus := range bonusPerParty {
			// bonus / totalBonuses.
			bonusPerParty[party] = bonus.Div(totalBonuses)
		}
	}

	return bonusPerParty
}

func (m *MarketLiquidity) distributePerformanceBonuses(
	ctx context.Context,
	partyIDs []string,
	bonuses map[string]num.Decimal,
) {
	bonusDistributionAcc, err := m.collateral.GetLiquidityFeesBonusDistributionAccount(m.marketID, m.asset)
	if err != nil {
		m.log.Panic("failed to get bonus distribution account", logging.Error(err))
	}

	bonusTransfers := FeeTransfer{
		transfers: []*types.Transfer{},
	}

	remainingBalance := bonusDistributionAcc.Balance.Clone()
	for _, partyID := range partyIDs {
		bonus := bonuses[partyID]

		// if bonus is 0 there is no need to process.
		if bonus.IsZero() {
			continue
		}

		amountD := bonus.Mul(bonusDistributionAcc.Balance.ToDecimal())
		amount, _ := num.UintFromDecimal(amountD)

		if !amount.IsZero() {
			transfer := m.NewTransfer(partyID, types.TransferTypeSlaPerformanceBonusDistribute, amount)
			bonusTransfers.transfers = append(bonusTransfers.transfers, transfer)
		}

		remainingBalance.Sub(remainingBalance, amount)
	}

	// in case of remaining balance choose pseudo random provider to receive it.
	if !remainingBalance.IsZero() {
		keys := sortedKeys(bonuses)

		rand.Seed(remainingBalance.BigInt().Int64())
		randIndex := rand.Intn(len(keys))
		selectedParty := keys[randIndex]

		transfer := m.NewTransfer(selectedParty, types.TransferTypeSlaPerformanceBonusDistribute, remainingBalance)
		bonusTransfers.transfers = append(bonusTransfers.transfers, transfer)
	}

	m.marketActivityTracker.UpdateFeesFromTransfers(m.asset, m.marketID, bonusTransfers.transfers)
	ledgerMovements, err := m.transferFees(ctx, bonusTransfers)
	if err != nil {
		m.log.Panic("failed to distribute SLA bonuses", logging.Error(err))
	}

	if len(ledgerMovements) > 0 {
		m.broker.Send(events.NewLedgerMovements(ctx, ledgerMovements))
	}
}

func (m *MarketLiquidity) distributeFeesBonusesAndApplyPenalties(
	ctx context.Context,
	slaPenalties liquidity.SlaPenalties,
) {
	// No LP penalties available so no need to continue.
	// This could happen during opening auction.
	if len(slaPenalties.PenaltiesPerParty) < 1 {
		return
	}

	partyIDs := sortedKeys(slaPenalties.PenaltiesPerParty)

	// first process bond penalties.
	m.processBondPenalties(ctx, partyIDs, slaPenalties.PenaltiesPerParty)

	// then distribute fees and calculate bonus.
	bonusPerParty := m.distributeFeesAndCalculateBonuses(ctx, partyIDs, slaPenalties)

	// lastly distribute performance bonus.
	m.distributePerformanceBonuses(ctx, partyIDs, bonusPerParty)
}

func sortedKeys[K constraints.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	return keys
}
