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

package settlement_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	bmocks "code.vegaprotocol.io/vega/core/broker/mocks"
	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/settlement"
	"code.vegaprotocol.io/vega/core/settlement/mocks"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/libs/proto"
	"code.vegaprotocol.io/vega/logging"
	snapshot "code.vegaprotocol.io/vega/protos/vega/snapshot/v1"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEngine struct {
	*settlement.SnapshotEngine
	ctrl      *gomock.Controller
	prod      *mocks.MockProduct
	positions []*mocks.MockMarketPosition
	tsvc      *mocks.MockTimeService
	broker    *bmocks.MockBroker
	market    string
}

type posValue struct {
	party string
	price *num.Uint // absolute Mark price
	size  int64
}

type marginVal struct {
	events.MarketPosition
	asset, marketID                               string
	margin, orderMargin, general, marginShortFall uint64
}

func TestMarketExpiry(t *testing.T) {
	t.Run("Settle at market expiry - success", testSettleExpiredSuccess)
	t.Run("Settle at market expiry - error", testSettleExpiryFail)
	t.Run("Settle at market expiry - rounding", testSettleRoundingSuccess)
}

func TestMarkToMarket(t *testing.T) {
	t.Run("No settle positions if none were on channel", testMarkToMarketEmpty)
	t.Run("Settle positions are pushed onto the slice channel in order", testMarkToMarketOrdered)
	t.Run("Trade adds new party to market, no MTM settlement because markPrice is the same", testAddNewParty)
	// add this test case because we had a runtime panic on the trades map earlier
	t.Run("Trade adds new party, immediately closing out with themselves", testAddNewPartySelfTrade)
	t.Run("Test MTM settle when the network is closed out", testMTMNetworkZero)
	t.Run("Test settling a funding period", testSettlingAFundingPeriod)
	t.Run("Test settling a funding period with rounding error", testSettlingAFundingPeriodRoundingError)
	t.Run("Test settling a funding period with small win amounts (zero transfers)", testSettlingAFundingPeriodExcessSmallLoss)
	t.Run("Test settling a funding period with rounding error (single party rounding)", testSettlingAFundingPeriodExcessLoss)
	t.Run("Test settling a funding period with zero win amounts (loss exceeds 1)", testSettlingAFundingPeriodExcessLostExceedsOneLoss)
}

func TestMTMWinDistribution(t *testing.T) {
	t.Run("A MTM loss party with a loss of value 1, with several parties needing a win", testMTMWinOneExcess)
	t.Run("Distribute win excess in a scenario where no transfer amount is < 1", testMTMWinNoZero)
	t.Run("Distribute loss excess in a scenario where no transfer amount is < 1", testMTMWinWithZero)
}

func testSettlingAFundingPeriod(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	ctx := context.Background()

	testPositions := []testPos{
		{
			party: "party1",
			size:  10,
		},
		{
			party: "party2",
			size:  -10,
		},
		{
			party: "party3",
			size:  0,
		},
	}
	positions := make([]events.MarketPosition, 0, len(testPositions))
	for _, p := range testPositions {
		positions = append(positions, p)
	}

	// 0 funding paymenet produces 0 transfers
	transfers, round := engine.SettleFundingPeriod(ctx, positions, num.IntZero())
	assert.Len(t, transfers, 0)
	assert.Nil(t, round)

	// no positions produces no transfers

	// positive funding payement, shorts pay long
	fundingPayment, _ := num.IntFromString("10", 10)
	transfers, round = engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.Len(t, transfers, 2)
	require.True(t, round.IsZero())
	assert.Equal(t, "100", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "100", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)

	// negative funding payement, long pays short, also expect loss to come before win
	fundingPayment, _ = num.IntFromString("-10", 10)
	transfers, round = engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.True(t, round.IsZero())
	require.Len(t, transfers, 2)
	assert.Equal(t, "100", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "100", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)

	// no positions produces no transfers
	transfers, round = engine.SettleFundingPeriod(ctx, []events.MarketPosition{}, fundingPayment)
	require.Nil(t, round)
	assert.Len(t, transfers, 0)
}

func testSettlingAFundingPeriodRoundingError(t *testing.T) {
	engine := getTestEngineWithFactor(t, 100)
	defer engine.Finish()
	ctx := context.Background()

	testPositions := []testPos{
		{
			party: "party1",
			size:  1000010,
		},
		{
			party: "party3",
			size:  -1000005,
		},
		{
			party: "party4",
			size:  -1000005,
		},
	}

	positions := make([]events.MarketPosition, 0, len(testPositions))
	for _, p := range testPositions {
		positions = append(positions, p)
	}

	fundingPayment, _ := num.IntFromString("10", 10)
	transfers, round := engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.Len(t, transfers, 3)
	assert.Equal(t, "100001", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "100000", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)
	assert.Equal(t, "100000", transfers[2].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[2].Transfer().Type)

	// here 100001 will be sent to the settlement account, but only 200000 we be paid out due to rounding,
	// so we expect a remainder of 1
	require.Equal(t, "1", round.String())
}

func testSettlingAFundingPeriodExcessLostExceedsOneLoss(t *testing.T) {
	engine := getTestEngineWithFactor(t, 100)
	defer engine.Finish()
	ctx := context.Background()

	testPositions := []testPos{
		{
			party: "party1",
			size:  23,
		},
		{
			party: "party3",
			size:  -3,
		},
		{
			party: "party4",
			size:  -5,
		},
		{
			party: "party5",
			size:  -5,
		},
		{
			party: "party6",
			size:  -5,
		},
		{
			party: "party7",
			size:  -5,
		},
	}

	positions := make([]events.MarketPosition, 0, len(testPositions))
	for _, p := range testPositions {
		positions = append(positions, p)
	}

	fundingPayment, _ := num.IntFromString("10", 10)
	transfers, round := engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.Len(t, transfers, 6)
	assert.Equal(t, "2", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "0", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)
	assert.Equal(t, "0", transfers[2].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[2].Transfer().Type)
	assert.Equal(t, "0", transfers[3].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[3].Transfer().Type)
	assert.Equal(t, "0", transfers[4].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[4].Transfer().Type)
	assert.Equal(t, "0", transfers[5].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[5].Transfer().Type)

	require.Equal(t, "2", round.String())
}

func testSettlingAFundingPeriodExcessSmallLoss(t *testing.T) {
	engine := getTestEngineWithFactor(t, 100)
	defer engine.Finish()
	ctx := context.Background()

	testPositions := []testPos{
		{
			party: "party1",
			size:  11,
		},
		{
			party: "party3",
			size:  -3,
		},
		{
			party: "party4",
			size:  -3,
		},
		{
			party: "party5",
			size:  -5,
		},
	}

	positions := make([]events.MarketPosition, 0, len(testPositions))
	for _, p := range testPositions {
		positions = append(positions, p)
	}

	fundingPayment, _ := num.IntFromString("10", 10)
	transfers, round := engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.Len(t, transfers, 4)
	assert.Equal(t, "1", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "0", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)
	assert.Equal(t, "0", transfers[2].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[2].Transfer().Type)
	assert.Equal(t, "0", transfers[3].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[3].Transfer().Type)

	require.Equal(t, "1", round.String())
}

func testSettlingAFundingPeriodExcessLoss(t *testing.T) {
	engine := getTestEngineWithFactor(t, 100)
	defer engine.Finish()
	ctx := context.Background()

	testPositions := []testPos{
		{
			party: "party1",
			size:  1000010,
		},
		{
			party: "party3",
			size:  -1000005,
		},
	}

	positions := make([]events.MarketPosition, 0, len(testPositions))
	for _, p := range testPositions {
		positions = append(positions, p)
	}

	fundingPayment, _ := num.IntFromString("10", 10)
	transfers, round := engine.SettleFundingPeriod(ctx, positions, fundingPayment)
	require.Len(t, transfers, 2)
	assert.Equal(t, "100001", transfers[0].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingLoss, transfers[0].Transfer().Type)
	assert.Equal(t, "100000", transfers[1].Transfer().Amount.Amount.String())
	assert.Equal(t, types.TransferTypePerpFundingWin, transfers[1].Transfer().Type)

	// here 100001 will be sent to the settlement account, but only 200000 we be paid out due to rounding,
	// so we expect a remainder of 1
	require.Equal(t, "1", round.String())
}

func testMTMWinNoZero(t *testing.T) {
	// cheat by setting the factor to some specific value, makes it easier to create a scenario where win/loss amounts don't match
	engine := getTestEngineWithFactor(t, 1)
	defer engine.Finish()

	price := num.NewUint(100000)
	one := num.NewUint(1)
	ctx := context.Background()

	initPos := []testPos{
		{
			price: price.Clone(),
			party: "party1",
			size:  10,
		},
		{
			price: price.Clone(),
			party: "party2",
			size:  23,
		},
		{
			price: price.Clone(),
			party: "party3",
			size:  -32,
		},
		{
			price: price.Clone(),
			party: "party4",
			size:  1,
		},
		{
			price: price.Clone(),
			party: "party5",
			size:  -29,
		},
		{
			price: price.Clone(),
			party: "party6",
			size:  27,
		},
	}

	init := make([]events.MarketPosition, 0, len(initPos))
	for _, p := range initPos {
		init = append(init, p)
	}

	newPrice := num.Sum(price, one, one, one)
	somePrice := num.Sum(price, one)
	newParty := testPos{
		size:  30,
		price: newPrice.Clone(),
		party: "party4",
	}

	trades := []*types.Trade{
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[0].party,
			Price:  somePrice.Clone(),
		},
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[1].party,
			Price:  somePrice.Clone(),
		},
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[2].party,
			Price:  newPrice.Clone(),
		},
	}
	updates := make([]events.MarketPosition, 0, len(initPos)+2)
	for _, trade := range trades {
		for i, p := range initPos {
			if p.party == trade.Seller {
				p.size -= int64(trade.Size)
			}
			p.price = trade.Price.Clone()
			initPos[i] = p
		}
	}
	for _, p := range initPos {
		updates = append(updates, p)
	}
	updates = append(updates, newParty)
	engine.Update(init)
	for _, trade := range trades {
		engine.AddTrade(trade)
	}
	transfers := engine.SettleMTM(ctx, newPrice.Clone(), updates)
	require.NotEmpty(t, transfers)
}

func testMTMWinOneExcess(t *testing.T) {
	engine := getTestEngineWithFactor(t, 1)
	defer engine.Finish()

	price := num.NewUint(10000)
	one := num.NewUint(1)
	ctx := context.Background()

	initPos := []testPos{
		{
			price: price.Clone(),
			party: "party1",
			size:  10,
		},
		{
			price: price.Clone(),
			party: "party2",
			size:  20,
		},
		{
			price: price.Clone(),
			party: "party3",
			size:  -29,
		},
		{
			price: price.Clone(),
			party: "party4",
			size:  1,
		},
		{
			price: price.Clone(),
			party: "party5",
			size:  -1,
		},
		{
			price: price.Clone(),
			party: "party5",
			size:  1,
		},
	}

	init := make([]events.MarketPosition, 0, len(initPos))
	for _, p := range initPos {
		init = append(init, p)
	}

	newPrice := num.Sum(price, one)
	newParty := testPos{
		size:  30,
		price: newPrice.Clone(),
		party: "party4",
	}

	trades := []*types.Trade{
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[0].party,
			Price:  newPrice.Clone(),
		},
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[1].party,
			Price:  newPrice.Clone(),
		},
		{
			Size:   10,
			Buyer:  newParty.party,
			Seller: initPos[2].party,
			Price:  newPrice.Clone(),
		},
	}
	updates := make([]events.MarketPosition, 0, len(initPos)+2)
	for _, trade := range trades {
		for i, p := range initPos {
			if p.party == trade.Seller {
				p.size -= int64(trade.Size)
			}
			p.price = trade.Price.Clone()
			initPos[i] = p
		}
	}
	for _, p := range initPos {
		updates = append(updates, p)
	}
	updates = append(updates, newParty)
	engine.Update(init)
	for _, trade := range trades {
		engine.AddTrade(trade)
	}
	transfers := engine.SettleMTM(ctx, newPrice.Clone(), updates)
	require.NotEmpty(t, transfers)
}

func testSettleRoundingSuccess(t *testing.T) {
	engine := getTestEngineWithFactor(t, 10)
	defer engine.Finish()
	// these are mark prices, product will provide the actual value
	// total wins = 554, total losses = 555
	pr := num.NewUint(1000)
	data := []posValue{
		{
			party: "party1",
			price: pr,   // winning
			size:  1101, // 5 * 110.1 = 550.5 -> 550
		},
		{
			party: "party2",
			price: pr,   // losing
			size:  -550, // 5 * 55 = 275
		},
		{
			party: "party3",
			price: pr,   // losing
			size:  -560, // 5 * 56 = 280
		},
		{
			party: "party4",
			price: pr, // winning
			size:  9,  // 5 * .9 = 4.5-> 4
		},
	}
	expect := []*types.Transfer{
		{
			Owner: data[1].party,
			Amount: &types.FinancialAmount{
				Amount: num.NewUint(275),
			},
			Type: types.TransferTypeLoss,
		},
		{
			Owner: data[2].party,
			Amount: &types.FinancialAmount{
				Amount: num.NewUint(280),
			},
			Type: types.TransferTypeLoss,
		},
		{
			Owner: data[0].party,
			Amount: &types.FinancialAmount{
				Amount: num.NewUint(550),
			},
			Type: types.TransferTypeWin,
		},
		{
			Owner: data[3].party,
			Amount: &types.FinancialAmount{
				Amount: num.NewUint(4),
			},
			Type: types.TransferTypeWin,
		},
	}
	oraclePrice := num.NewUint(1005)
	settleF := func(price *num.Uint, settlementData *num.Uint, size num.Decimal) (*types.FinancialAmount, bool, num.Decimal, error) {
		amt, neg := num.UintZero().Delta(oraclePrice, price)
		if size.IsNegative() {
			size = size.Neg()
			neg = !neg
		}

		amount, rem := num.UintFromDecimalWithFraction(amt.ToDecimal().Mul(size))
		return &types.FinancialAmount{
			Amount: amount,
		}, neg, rem, nil
	}
	positions := engine.getExpiryPositions(data...)
	// we expect settle calls for each position
	engine.prod.EXPECT().Settle(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(settleF).AnyTimes()
	// ensure positions are set
	engine.Update(positions)
	// now settle:
	got, round, err := engine.Settle(time.Now(), oraclePrice)
	assert.NoError(t, err)
	assert.Equal(t, len(expect), len(got))
	assert.True(t, round.EQ(num.NewUint(1)))
	for i, p := range got {
		e := expect[i]
		assert.Equal(t, e.Type, p.Type)
		assert.Equal(t, e.Amount.Amount, p.Amount.Amount)
	}
}

func testSettleExpiredSuccess(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	// these are mark prices, product will provide the actual value
	pr := num.NewUint(1000)
	data := []posValue{ // {{{2
		{
			party: "party1",
			price: pr, // winning
			size:  10,
		},
		{
			party: "party2",
			price: pr, // losing
			size:  -5,
		},
		{
			party: "party3",
			price: pr, // losing
			size:  -5,
		},
	}
	half := num.NewUint(500)
	expect := []*types.Transfer{
		{
			Owner: data[1].party,
			Amount: &types.FinancialAmount{
				Amount: half,
			},
			Type: types.TransferTypeLoss,
		},
		{
			Owner: data[2].party,
			Amount: &types.FinancialAmount{
				Amount: half,
			},
			Type: types.TransferTypeLoss,
		},
		{
			Owner: data[0].party,
			Amount: &types.FinancialAmount{
				Amount: pr,
			},
			Type: types.TransferTypeWin,
		},
	} // }}}
	oraclePrice := num.NewUint(1100)
	settleF := func(price *num.Uint, settlementData *num.Uint, size num.Decimal) (*types.FinancialAmount, bool, num.Decimal, error) {
		amt, neg := num.UintZero().Delta(oraclePrice, price)
		if size.IsNegative() {
			size = size.Neg()
			neg = !neg
		}

		amount, rem := num.UintFromDecimalWithFraction(amt.ToDecimal().Mul(size))
		return &types.FinancialAmount{
			Amount: amount,
		}, neg, rem, nil
	}
	positions := engine.getExpiryPositions(data...)
	// we expect settle calls for each position
	engine.prod.EXPECT().Settle(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(settleF).AnyTimes()
	// ensure positions are set
	engine.Update(positions)
	// now settle:
	got, _, err := engine.Settle(time.Now(), oraclePrice)
	assert.NoError(t, err)
	assert.Equal(t, len(expect), len(got))
	for i, p := range got {
		e := expect[i]
		assert.Equal(t, e.Type, p.Type)
		assert.Equal(t, e.Amount.Amount, p.Amount.Amount)
	}
}

func testSettleExpiryFail(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	// these are mark prices, product will provide the actual value
	data := []posValue{
		{
			party: "party1",
			price: num.NewUint(1000),
			size:  10,
		},
	}
	errExp := errors.New("product.Settle error")
	positions := engine.getExpiryPositions(data...)
	engine.prod.EXPECT().Settle(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil, false, num.DecimalZero(), errExp)
	engine.Update(positions)
	empty, _, err := engine.Settle(time.Now(), num.UintZero())
	assert.Empty(t, empty)
	assert.Error(t, err)
	assert.Equal(t, errExp, err)
}

func testMarkToMarketEmpty(t *testing.T) {
	markPrice := num.NewUint(10000)
	// there's only 1 trade to test here
	data := posValue{
		price: markPrice,
		size:  1,
		party: "test",
	}
	engine := getTestEngine(t)
	defer engine.Finish()
	pos := mocks.NewMockMarketPosition(engine.ctrl)
	pos.EXPECT().Party().AnyTimes().Return(data.party)
	pos.EXPECT().Size().AnyTimes().Return(data.size)
	pos.EXPECT().Price().AnyTimes().Return(markPrice)
	engine.Update([]events.MarketPosition{pos})
	result := engine.SettleMTM(context.Background(), markPrice, []events.MarketPosition{pos})
	assert.Empty(t, result)
}

func testAddNewPartySelfTrade(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	markPrice := num.NewUint(1000)
	t1 := testPos{
		price: markPrice.Clone(),
		party: "party1",
		size:  5,
	}
	init := []events.MarketPosition{
		t1,
		testPos{
			price: markPrice.Clone(),
			party: "party2",
			size:  -5,
		},
	}
	// let's not change the markPrice
	// just add a party to the market, buying from an existing party
	trade := &types.Trade{
		Buyer:  "party3",
		Seller: "party3",
		Price:  markPrice.Clone(),
		Size:   1,
	}
	// the first party is the seller
	// so these are the new positions after the trade
	t1.size--
	positions := []events.MarketPosition{
		t1,
		init[1],
		testPos{
			party: "party3",
			size:  0,
			price: markPrice.Clone(),
		},
	}
	engine.Update(init)
	engine.AddTrade(trade)
	noTransfers := engine.SettleMTM(context.Background(), markPrice, positions)
	assert.Len(t, noTransfers, 1)
	assert.Nil(t, noTransfers[0].Transfer())
}

func TestNetworkPartyCloseout(t *testing.T) {
	engine := getTestEngine(t)
	currentMP := num.NewUint(1000)
	// network trade
	nTrade := &types.Trade{
		Buyer:       types.NetworkParty,
		Seller:      types.NetworkParty,
		Price:       currentMP.Clone(),
		MarketPrice: currentMP.Clone(),
		Size:        10,
	}
	nPosition := testPos{
		party: types.NetworkParty,
		size:  10,
		price: currentMP.Clone(),
	}
	sellPrice := num.NewUint(990)
	// now trade with health party, at some different price
	// trigger a loss for the network.
	cTrade := &types.Trade{
		Buyer:  "party1",
		Seller: types.NetworkParty,
		Size:   2,
		Price:  sellPrice.Clone(),
	}
	init := []events.MarketPosition{
		nPosition,
		testPos{
			party: "party1",
			size:  0,
			price: currentMP.Clone(),
		},
	}
	positions := []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  8,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party1",
			size:  2,
			price: currentMP.Clone(),
		},
	}
	engine.Update(init)
	engine.AddTrade(nTrade)
	engine.AddTrade(cTrade)
	transfers := engine.SettleMTM(context.Background(), currentMP, positions)
	assert.NotEmpty(t, transfers)
	assert.Len(t, transfers, 2)
}

func TestNetworkCloseoutZero(t *testing.T) {
	engine := getTestEngine(t)
	currentMP := num.NewUint(1000)
	// network trade
	nTrade := &types.Trade{
		Buyer:       types.NetworkParty,
		Seller:      types.NetworkParty,
		Price:       currentMP.Clone(),
		MarketPrice: currentMP.Clone(),
		Size:        10,
	}
	nPosition := testPos{
		party: types.NetworkParty,
		size:  10,
		price: currentMP.Clone(),
	}
	sellPrice := num.NewUint(999)
	// now trade with health party, at some different price
	// trigger a loss for the network.
	cTrades := []*types.Trade{
		{
			Buyer:  "party1",
			Seller: types.NetworkParty,
			Size:   1,
			Price:  sellPrice.Clone(),
		},
		{
			Buyer:  "party2",
			Seller: types.NetworkParty,
			Size:   1,
			Price:  sellPrice.Clone(),
		},
	}
	init := []events.MarketPosition{
		nPosition,
		testPos{
			party: "party1",
			size:  0,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party2",
			size:  0,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -5,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: currentMP.Clone(),
		},
	}
	positions := []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  8,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party1",
			size:  1,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party2",
			size:  1,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -5,
			price: currentMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: currentMP.Clone(),
		},
	}
	engine.Update(init)
	engine.AddTrade(nTrade)
	for _, cTrade := range cTrades {
		engine.AddTrade(cTrade)
	}
	transfers := engine.SettleMTM(context.Background(), currentMP, positions)
	assert.NotEmpty(t, transfers)
	// now that the network has an established long position, make short positions close out and mark to market
	// party 3 closes their position, lowering the mark price
	newMP := num.NewUint(990)
	trade := &types.Trade{
		Buyer:  "party3",
		Seller: "party1",
		Price:  newMP,
		Size:   1,
	}
	positions = []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  8,
			price: newMP.Clone(),
		},
		testPos{
			party: "party1",
			size:  0,
			price: newMP.Clone(),
		},
		testPos{
			party: "party2",
			size:  1,
			price: newMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -4,
			price: newMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: newMP.Clone(),
		},
	}
	engine.AddTrade(trade)
	transfers = engine.SettleMTM(context.Background(), newMP, positions)
	assert.NotEmpty(t, transfers)
	// now make it look like party2 got distressed because of this MTM settlement
	nTrade = &types.Trade{
		Price:       newMP.Clone(),
		MarketPrice: newMP.Clone(),
		Size:        1,
		Buyer:       types.NetworkParty,
		Seller:      types.NetworkParty,
	}
	positions = []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  9,
			price: newMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -4,
			price: newMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: newMP.Clone(),
		},
	}
	engine.AddTrade(nTrade)
	transfers = engine.SettleMTM(context.Background(), newMP, positions)
	assert.NotEmpty(t, transfers)
	newMP = num.NewUint(995)
	positions = []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  9,
			price: newMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -4,
			price: newMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: newMP.Clone(),
		},
	}
	transfers = engine.SettleMTM(context.Background(), newMP.Clone(), positions)
	assert.NotEmpty(t, transfers)
	// now the same, but network loses
	newMP = num.NewUint(990)
	positions = []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  9,
			price: newMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  -4,
			price: newMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  -5,
			price: newMP.Clone(),
		},
	}
	transfers = engine.SettleMTM(context.Background(), newMP.Clone(), positions)
	assert.NotEmpty(t, transfers)
	// assume no trades occurred, but the mark price has changed (shouldn't happen, but this could end up with a situation where network profits without trading)
	// network disposes of its position and profits
	disposePrice := num.NewUint(1010)
	trades := []*types.Trade{
		{
			Seller: types.NetworkParty,
			Buyer:  "party3",
			Price:  disposePrice.Clone(),
			Size:   4,
		},
		{
			Seller: types.NetworkParty,
			Buyer:  "party4",
			Price:  disposePrice.Clone(),
			Size:   5,
		},
	}
	positions = []events.MarketPosition{
		testPos{
			party: types.NetworkParty,
			size:  0,
			price: newMP.Clone(),
		},
		testPos{
			party: "party3",
			size:  0,
			price: newMP.Clone(),
		},
		testPos{
			party: "party4",
			size:  0,
			price: newMP.Clone(),
		},
	}
	for _, tr := range trades {
		engine.AddTrade(tr)
	}
	transfers = engine.SettleMTM(context.Background(), num.NewUint(1000), positions)
	assert.NotEmpty(t, transfers)
}

func testAddNewParty(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	markPrice := num.NewUint(1000)
	t1 := testPos{
		price: markPrice.Clone(),
		party: "party1",
		size:  5,
	}
	init := []events.MarketPosition{
		t1,
		testPos{
			price: markPrice.Clone(),
			party: "party2",
			size:  -5,
		},
	}
	// let's not change the markPrice
	// just add a party to the market, buying from an existing party
	trade := &types.Trade{
		Buyer:  "party3",
		Seller: t1.party,
		Price:  markPrice.Clone(),
		Size:   1,
	}
	// the first party is the seller
	// so these are the new positions after the trade
	t1.size--
	positions := []events.MarketPosition{
		t1,
		init[1],
		testPos{
			party: "party3",
			size:  1,
			price: markPrice.Clone(),
		},
	}
	engine.Update(init)
	engine.AddTrade(trade)
	noTransfers := engine.SettleMTM(context.Background(), markPrice, positions)
	assert.Len(t, noTransfers, 2)
	for _, v := range noTransfers {
		assert.Nil(t, v.Transfer())
	}
}

// This tests MTM results put losses first, trades tested are Long going longer, short going shorter
// and long going short, short going long, and a third party who's not trading at all.
func testMarkToMarketOrdered(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	pr := num.NewUint(10000)
	positions := []posValue{
		{
			price: pr,
			size:  1,
			party: "party1", // mocks will create 2 parties (long & short)
		},
		{
			price: pr.Clone(),
			size:  -1,
			party: "party2",
		},
	}
	markPrice := pr.Clone()
	markPrice = markPrice.Add(markPrice, num.NewUint(1000))
	neutral := testPos{
		party: "neutral",
		size:  5,
		price: pr.Clone(),
	}
	init := []events.MarketPosition{
		neutral,
		testPos{
			price: neutral.price.Clone(),
			party: "party1",
			size:  1,
		},
		testPos{
			price: neutral.price.Clone(),
			party: "party2",
			size:  -1,
		},
	}
	short, long := make([]events.MarketPosition, 0, 3), make([]events.MarketPosition, 0, 3)
	// the SettleMTM data must contain the new mark price already
	neutral.price = markPrice.Clone()
	short = append(short, neutral)
	long = append(long, neutral)
	// we have a long and short trade example
	trades := map[string]*types.Trade{
		"long": {
			Price: markPrice,
			Size:  1,
		},
		// to go short, the trade has to be 2
		"short": {
			Price: markPrice,
			Size:  2,
		},
	}
	// creates trades and event slices we'll be needing later on
	for _, p := range positions {
		if p.size > 0 {
			trades["long"].Buyer = p.party
			trades["short"].Seller = p.party
			long = append(long, testPos{
				party: p.party,
				price: markPrice.Clone(),
				size:  p.size + int64(trades["long"].Size),
			})
			short = append(short, testPos{
				party: p.party,
				price: markPrice.Clone(),
				size:  p.size - int64(trades["short"].Size),
			})
		} else {
			trades["long"].Seller = p.party
			trades["short"].Buyer = p.party
			long = append(long, testPos{
				party: p.party,
				price: markPrice.Clone(),
				size:  p.size - int64(trades["long"].Size),
			})
			short = append(short, testPos{
				party: p.party,
				price: markPrice.Clone(),
				size:  p.size + int64(trades["short"].Size),
			})
		}
	}
	updates := map[string][]events.MarketPosition{
		"long":  long,
		"short": short,
	}
	// set up the engine, ready to run the scenario's
	// for each data-set we reset the state in the engine, then we check the MTM is performed
	// correctly
	for k, trade := range trades {
		engine.Update(init)
		engine.AddTrade(trade)
		update := updates[k]
		transfers := engine.SettleMTM(context.Background(), markPrice, update)
		assert.NotEmpty(t, transfers)
		assert.Equal(t, 3, len(transfers))
		// start with losses, end with wins
		assert.Equal(t, types.TransferTypeMTMLoss, transfers[0].Transfer().Type)
		assert.Equal(t, types.TransferTypeMTMWin, transfers[len(transfers)-1].Transfer().Type)
		assert.Equal(t, "party2", transfers[0].Party()) // we expect party2 to have a loss
	}

	state, _, _ := engine.GetState(engine.market)
	engineLoad := getTestEngine(t)
	var pl snapshot.Payload
	require.NoError(t, proto.Unmarshal(state, &pl))
	payload := types.PayloadFromProto(&pl)

	_, err := engineLoad.LoadState(context.Background(), payload)
	require.NoError(t, err)

	state2, _, _ := engineLoad.GetState(engine.market)
	require.True(t, bytes.Equal(state, state2))
}

func testMTMNetworkZero(t *testing.T) {
	t.Skip("not implemented yet")
	engine := getTestEngine(t)
	defer engine.Finish()
	markPrice := num.NewUint(1000)
	init := []events.MarketPosition{
		testPos{
			price: markPrice.Clone(),
			party: "party1",
			size:  5,
		},
		testPos{
			price: markPrice.Clone(),
			party: "party2",
			size:  -5,
		},
		testPos{
			price: markPrice.Clone(),
			party: "party3",
			size:  10,
		},
		testPos{
			price: markPrice.Clone(),
			party: "party4",
			size:  -10,
		},
	}
	// initialise the engine with the positions above
	engine.Update(init)
	// assume party 4 is distressed, network has to trade and buy 10
	// ensure the network loses in this scenario: the price has gone up
	cPrice := num.Sum(markPrice, num.NewUint(1))
	trade := &types.Trade{
		Buyer:  types.NetworkParty,
		Seller: "party1",
		Size:   5, // party 1 only has 5 on the book, let's pretend we can close him our
		Price:  cPrice.Clone(),
	}
	engine.AddTrade(trade)
	engine.AddTrade(&types.Trade{
		Buyer:  types.NetworkParty,
		Seller: "party3",
		Size:   2,
		Price:  cPrice.Clone(),
	})
	engine.AddTrade(&types.Trade{
		Buyer:  types.NetworkParty,
		Seller: "party2",
		Size:   3,
		Price:  cPrice.Clone(), // party 2 is going from -5 to -8
	})
	// the new positions of the parties who have traded with the network...
	positions := []events.MarketPosition{
		testPos{
			party: "party1", // party 1 was 5 long, sold 5 to network, so closed out
			price: markPrice.Clone(),
			size:  0,
		},
		testPos{
			party: "party3",
			size:  8, // long 10, sold 2
			price: markPrice.Clone(),
		},
		testPos{
			party: "party2",
			size:  -8,
			price: markPrice.Clone(), // party 2 was -5, shorted an additional 3 => -8
		},
	}
	// new markprice is cPrice
	noTransfers := engine.SettleMTM(context.Background(), cPrice, positions)
	assert.Len(t, noTransfers, 3)
	hasNetwork := false
	for i, v := range noTransfers {
		assert.NotNil(t, v.Transfer())
		if v.Party() == types.NetworkParty {
			// network hás to lose
			require.Equal(t, types.TransferTypeMTMLoss, v.Transfer().Type)
			// network loss should be at the start of the slice
			require.Equal(t, 0, i)
			hasNetwork = true
		}
	}
	require.True(t, hasNetwork)
}

func testMTMWinWithZero(t *testing.T) {
	// cheat by setting the factor to some specific value, makes it easier to create a scenario where win/loss amounts don't match
	engine := getTestEngineWithFactor(t, 5)
	defer engine.Finish()

	price := num.NewUint(100000)
	one := num.NewUint(1)
	ctx := context.Background()

	initPos := []testPos{
		{
			price: price.Clone(),
			party: "party1",
			size:  1,
		},
		{
			price: price.Clone(),
			party: "party2",
			size:  2,
		},
		{
			price: price.Clone(),
			party: "party3",
			size:  -3,
		},
		{
			price: price.Clone(),
			party: "party4",
			size:  1,
		},
		{
			price: price.Clone(),
			party: "party5",
			size:  -3,
		},
		{
			price: price.Clone(),
			party: "party6",
			size:  2,
		},
	}

	init := make([]events.MarketPosition, 0, len(initPos))
	for _, p := range initPos {
		init = append(init, p)
	}

	newPrice := num.Sum(price, one, one, one)
	somePrice := num.Sum(price, one)
	newParty := testPos{
		size:  3,
		price: newPrice.Clone(),
		party: "party4",
	}

	trades := []*types.Trade{
		{
			Size:   1,
			Buyer:  newParty.party,
			Seller: initPos[0].party,
			Price:  somePrice.Clone(),
		},
		{
			Size:   1,
			Buyer:  newParty.party,
			Seller: initPos[1].party,
			Price:  somePrice.Clone(),
		},
		{
			Size:   1,
			Buyer:  newParty.party,
			Seller: initPos[2].party,
			Price:  newPrice.Clone(),
		},
	}
	updates := make([]events.MarketPosition, 0, len(initPos)+2)
	for _, trade := range trades {
		for i, p := range initPos {
			if p.party == trade.Seller {
				p.size -= int64(trade.Size)
			}
			p.price = trade.Price.Clone()
			initPos[i] = p
		}
	}
	for _, p := range initPos {
		updates = append(updates, p)
	}
	updates = append(updates, newParty)
	engine.Update(init)
	for _, trade := range trades {
		engine.AddTrade(trade)
	}
	transfers := engine.SettleMTM(ctx, newPrice.Clone(), updates)
	require.NotEmpty(t, transfers)
}

// {{{.
func (te *testEngine) getExpiryPositions(positions ...posValue) []events.MarketPosition {
	te.positions = make([]*mocks.MockMarketPosition, 0, len(positions))
	mpSlice := make([]events.MarketPosition, 0, len(positions))
	for _, p := range positions {
		pos := mocks.NewMockMarketPosition(te.ctrl)
		// these values should only be obtained once, and assigned internally
		pos.EXPECT().Party().MinTimes(1).AnyTimes().Return(p.party)
		pos.EXPECT().Size().MinTimes(1).AnyTimes().Return(p.size)
		pos.EXPECT().Price().Times(1).Return(p.price)
		te.positions = append(te.positions, pos)
		mpSlice = append(mpSlice, pos)
	}
	return mpSlice
}

func (te *testEngine) getMockMarketPositions(data []posValue) ([]settlement.MarketPosition, []events.MarketPosition) {
	raw, evts := make([]settlement.MarketPosition, 0, len(data)), make([]events.MarketPosition, 0, len(data))
	for _, pos := range data {
		mock := mocks.NewMockMarketPosition(te.ctrl)
		mock.EXPECT().Party().MinTimes(1).Return(pos.party)
		mock.EXPECT().Size().MinTimes(1).Return(pos.size)
		mock.EXPECT().Price().MinTimes(1).Return(pos.price)
		raw = append(raw, mock)
		evts = append(evts, mock)
	}
	return raw, evts
}

func TestRemoveDistressedNoTrades(t *testing.T) {
	engine := getTestEngine(t)
	defer engine.Finish()
	engine.prod.EXPECT().Settle(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(markPrice *num.Uint, settlementData *num.Uint, size num.Decimal) (*types.FinancialAmount, bool, num.Decimal, error) {
		return &types.FinancialAmount{Amount: num.UintZero()}, false, num.DecimalZero(), nil
	})

	data := []posValue{
		{
			party: "testparty1",
			price: num.NewUint(1234),
			size:  100,
		},
		{
			party: "testparty2",
			price: num.NewUint(1235),
			size:  0,
		},
	}
	raw, evts := engine.getMockMarketPositions(data)
	// margin evt
	marginEvts := make([]events.Margin, 0, len(raw))
	for _, pe := range raw {
		marginEvts = append(marginEvts, marginVal{
			MarketPosition: pe,
		})
	}

	assert.False(t, engine.HasTraded())
	engine.Update(evts)
	engine.RemoveDistressed(context.Background(), marginEvts)
	assert.False(t, engine.HasTraded())
}

func TestConcurrent(t *testing.T) {
	const N = 10

	engine := getTestEngine(t)
	defer engine.Finish()
	engine.prod.EXPECT().Settle(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(markPrice *num.Uint, settlementData *num.Uint, size num.Decimal) (*types.FinancialAmount, bool, num.Decimal, error) {
		return &types.FinancialAmount{Amount: num.UintZero()}, false, num.DecimalZero(), nil
	})

	cfg := engine.Config
	cfg.Level.Level = logging.DebugLevel
	engine.ReloadConf(cfg)
	cfg.Level.Level = logging.InfoLevel
	engine.ReloadConf(cfg)

	var wg sync.WaitGroup

	now := time.Now()
	wg.Add(N * 3)
	for i := 0; i < N; i++ {
		data := []posValue{
			{
				party: "testparty1",
				price: num.NewUint(1234),
				size:  100,
			},
			{
				party: "testparty2",
				price: num.NewUint(1235),
				size:  0,
			},
		}
		raw, evts := engine.getMockMarketPositions(data)
		// margin evt
		marginEvts := make([]events.Margin, 0, len(raw))
		for _, pe := range raw {
			marginEvts = append(marginEvts, marginVal{
				MarketPosition: pe,
			})
		}

		go func() {
			defer wg.Done()
			// Update requires posMu
			engine.Update(evts)
		}()
		go func() {
			defer wg.Done()
			// RemoveDistressed requires posMu and closedMu
			engine.RemoveDistressed(context.Background(), marginEvts)
		}()
		go func() {
			defer wg.Done()
			// Settle requires posMu
			_, _, err := engine.Settle(now, num.UintZero())
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

// Finish - call finish on controller, remove test state (positions).
func (te *testEngine) Finish() {
	te.ctrl.Finish()
	te.positions = nil
}

// Quick mock implementation of the events.MarketPosition interface.
type testPos struct {
	party                         string
	size, buy, sell               int64
	price                         *num.Uint
	buySumProduct, sellSumProduct uint64
}

func (t testPos) AverageEntryPrice() *num.Uint {
	return num.UintZero()
}

func (t testPos) Party() string {
	return t.party
}

func (t testPos) Size() int64 {
	return t.size
}

func (t testPos) Buy() int64 {
	return t.buy
}

func (t testPos) Sell() int64 {
	return t.sell
}

func (t testPos) Price() *num.Uint {
	if t.price == nil {
		return num.UintZero()
	}
	return t.price
}

func (t testPos) BuySumProduct() *num.Uint {
	return num.NewUint(t.buySumProduct)
}

func (t testPos) SellSumProduct() *num.Uint {
	return num.NewUint(t.sellSumProduct)
}

func (t testPos) VWBuy() *num.Uint {
	if t.buy == 0 {
		return num.UintZero()
	}
	return num.NewUint(t.buySumProduct / uint64(t.buy))
}

func (t testPos) VWSell() *num.Uint {
	if t.sell == 0 {
		return num.UintZero()
	}
	return num.NewUint(t.sellSumProduct / uint64(t.sell))
}

func (t testPos) ClearPotentials() {}

func getTestEngineWithFactor(t *testing.T, f float64) *testEngine {
	t.Helper()
	ctrl := gomock.NewController(t)
	conf := settlement.NewDefaultConfig()
	prod := mocks.NewMockProduct(ctrl)
	tsvc := mocks.NewMockTimeService(ctrl)
	tsvc.EXPECT().GetTimeNow().AnyTimes()
	broker := bmocks.NewMockBroker(ctrl)
	broker.EXPECT().SendBatch(gomock.Any()).AnyTimes()
	market := "BTC/DEC19"
	prod.EXPECT().GetAsset().AnyTimes().Do(func() string { return "BTC" })
	return &testEngine{
		SnapshotEngine: settlement.NewSnapshotEngine(logging.NewTestLogger(), conf, prod, market, tsvc, broker, num.NewDecimalFromFloat(f)),
		ctrl:           ctrl,
		prod:           prod,
		tsvc:           tsvc,
		broker:         broker,
		positions:      nil,
		market:         market,
	}
}

func getTestEngine(t *testing.T) *testEngine {
	t.Helper()
	return getTestEngineWithFactor(t, 1)
} // }}}

func (m marginVal) Asset() string {
	return m.asset
}

func (m marginVal) MarketID() string {
	return m.marketID
}

func (m marginVal) MarginBalance() *num.Uint {
	return num.NewUint(m.margin)
}

func (m marginVal) OrderMarginBalance() *num.Uint {
	return num.NewUint(m.orderMargin)
}

func (m marginVal) GeneralBalance() *num.Uint {
	return num.NewUint(m.general)
}

func (m marginVal) GeneralAccountBalance() *num.Uint {
	return num.NewUint(m.general)
}

func (m marginVal) BondBalance() *num.Uint {
	return num.UintZero()
}

func (m marginVal) MarginShortFall() *num.Uint {
	return num.NewUint(m.marginShortFall)
}

//  vim: set ts=4 sw=4 tw=0 foldlevel=1 foldmethod=marker noet :
