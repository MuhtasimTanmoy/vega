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

package banking

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"code.vegaprotocol.io/vega/core/assets"
	"code.vegaprotocol.io/vega/core/broker"
	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/core/validators"
	"code.vegaprotocol.io/vega/libs/crypto"
	"code.vegaprotocol.io/vega/libs/num"
	vgproto "code.vegaprotocol.io/vega/libs/proto"
	"code.vegaprotocol.io/vega/logging"
	"code.vegaprotocol.io/vega/protos/vega"
	proto "code.vegaprotocol.io/vega/protos/vega"
	snapshot "code.vegaprotocol.io/vega/protos/vega/snapshot/v1"

	"github.com/emirpasic/gods/sets/treeset"
	"golang.org/x/exp/maps"
)

//go:generate go run github.com/golang/mock/mockgen -destination mocks/mocks.go -package mocks code.vegaprotocol.io/vega/core/banking Assets,Notary,Collateral,Witness,TimeService,EpochService,Topology,MarketActivityTracker,ERC20BridgeView,EthereumEventSource

var (
	ErrWrongAssetTypeUsedInBuiltinAssetChainEvent = errors.New("non builtin asset used for builtin asset chain event")
	ErrWrongAssetTypeUsedInERC20ChainEvent        = errors.New("non ERC20 for ERC20 chain event")
	ErrWrongAssetUsedForERC20Withdraw             = errors.New("non erc20 asset used for lock withdraw")
	ErrInvalidWithdrawalState                     = errors.New("invalid withdrawal state")
	ErrNotMatchingWithdrawalForReference          = errors.New("invalid reference for withdrawal chain event")
	ErrWithdrawalNotReady                         = errors.New("withdrawal not ready")
	ErrNotEnoughFundsToTransfer                   = errors.New("not enough funds to transfer")
)

type Assets interface {
	Get(assetID string) (*assets.Asset, error)
	Enable(ctx context.Context, assetID string) error
	ApplyAssetUpdate(ctx context.Context, assetID string) error
}

// Notary ...

type Notary interface {
	StartAggregate(resID string, kind types.NodeSignatureKind, signature []byte)
	IsSigned(ctx context.Context, id string, kind types.NodeSignatureKind) ([]types.NodeSignature, bool)
	OfferSignatures(kind types.NodeSignatureKind, f func(resources string) []byte)
}

// Collateral engine.
type Collateral interface {
	Deposit(ctx context.Context, party, asset string, amount *num.Uint) (*types.LedgerMovement, error)
	Withdraw(ctx context.Context, party, asset string, amount *num.Uint) (*types.LedgerMovement, error)
	EnableAsset(ctx context.Context, asset types.Asset) error
	GetPartyGeneralAccount(party, asset string) (*types.Account, error)
	GetPartyVestedRewardAccount(partyID, asset string) (*types.Account, error)
	TransferFunds(ctx context.Context,
		transfers []*types.Transfer,
		accountTypes []types.AccountType,
		references []string,
		feeTransfers []*types.Transfer,
		feeTransfersAccountTypes []types.AccountType,
	) ([]*types.LedgerMovement, error)
	GovernanceTransferFunds(ctx context.Context, transfers []*types.Transfer, accountTypes []types.AccountType, references []string) ([]*types.LedgerMovement, error)
	PropagateAssetUpdate(ctx context.Context, asset types.Asset) error
	GetSystemAccountBalance(asset, market string, accountType types.AccountType) (*num.Uint, error)
}

// Witness provide foreign chain resources validations.
type Witness interface {
	StartCheck(validators.Resource, func(interface{}, bool), time.Time) error
	RestoreResource(validators.Resource, func(interface{}, bool)) error
}

// TimeService provide the time of the vega node using the tm time.
type TimeService interface {
	GetTimeNow() time.Time
}

// Epochervice ...
type EpochService interface {
	NotifyOnEpoch(f func(context.Context, types.Epoch), r func(context.Context, types.Epoch))
}

// Topology ...
type Topology interface {
	IsValidator() bool
}

type MarketActivityTracker interface {
	CalculateMetricForIndividuals(ds *vega.DispatchStrategy) []*types.PartyContributionScore
	CalculateMetricForTeams(ds *vega.DispatchStrategy) ([]*types.PartyContributionScore, map[string][]*types.PartyContributionScore)
	GetMarketsWithEligibleProposer(asset string, markets []string, payoutAsset string, funder string) []*types.MarketContributionScore
	MarkPaidProposer(asset, market, payoutAsset string, marketsInScope []string, funder string)
	MarketTrackedForAsset(market, asset string) bool
	TeamStatsForMarkets(allMarketsForAssets, onlyTheseMarkets []string) map[string]map[string]*num.Uint
}

type EthereumEventSource interface {
	UpdateCollateralStartingBlock(uint64)
}

const (
	pendingState uint32 = iota
	okState
	rejectedState
)

var defaultValidationDuration = 30 * 24 * time.Hour

type dispatchStrategyCacheEntry struct {
	ds       *proto.DispatchStrategy
	refCount int
}

type Engine struct {
	cfg            Config
	log            *logging.Logger
	timeService    TimeService
	broker         broker.Interface
	col            Collateral
	witness        Witness
	notary         Notary
	assets         Assets
	top            Topology
	ethEventSource EthereumEventSource

	assetActs        map[string]*assetAction
	seen             *treeset.Set
	lastSeenEthBlock uint64 // the block height of the latest ERC20 chain event
	withdrawals      map[string]withdrawalRef
	withdrawalCnt    *big.Int
	deposits         map[string]*types.Deposit

	currentEpoch uint64
	bss          *bankingSnapshotState

	marketActivityTracker MarketActivityTracker

	// transfer fee related stuff
	scheduledTransfers         map[int64][]scheduledTransfer
	transferFeeFactor          num.Decimal
	minTransferQuantumMultiple num.Decimal
	maxQuantumAmount           num.Decimal

	feeDiscountDecayFraction        num.Decimal
	feeDiscountMinimumTrackedAmount num.Decimal

	// assetID -> partyID -> fee discount
	pendingPerAssetAndPartyFeeDiscountUpdates map[string]map[string]*num.Uint
	feeDiscountPerPartyAndAsset               map[partyAssetKey]*num.Uint

	scheduledGovernanceTransfers    map[int64][]*types.GovernanceTransfer
	recurringGovernanceTransfers    []*types.GovernanceTransfer
	recurringGovernanceTransfersMap map[string]*types.GovernanceTransfer

	// a hash of a dispatch strategy to the dispatch strategy details
	hashToStrategy map[string]*dispatchStrategyCacheEntry

	// recurring transfers in the order they were created
	recurringTransfers []*types.RecurringTransfer
	// transfer id to recurringTransfers
	recurringTransfersMap map[string]*types.RecurringTransfer

	bridgeState *bridgeState
	bridgeView  ERC20BridgeView

	minWithdrawQuantumMultiple num.Decimal

	maxGovTransferQunatumMultiplier num.Decimal
	maxGovTransferFraction          num.Decimal
}

type withdrawalRef struct {
	w   *types.Withdrawal
	ref *big.Int
}

func New(
	log *logging.Logger,
	cfg Config,
	col Collateral,
	witness Witness,
	tsvc TimeService,
	assets Assets,
	notary Notary,
	broker broker.Interface,
	top Topology,
	marketActivityTracker MarketActivityTracker,
	bridgeView ERC20BridgeView,
	ethEventSource EthereumEventSource,
) (e *Engine) {
	log = log.Named(namedLogger)
	log.SetLevel(cfg.Level.Get())

	return &Engine{
		cfg:                             cfg,
		log:                             log,
		timeService:                     tsvc,
		broker:                          broker,
		col:                             col,
		witness:                         witness,
		assets:                          assets,
		notary:                          notary,
		top:                             top,
		ethEventSource:                  ethEventSource,
		assetActs:                       map[string]*assetAction{},
		seen:                            treeset.NewWithStringComparator(),
		withdrawals:                     map[string]withdrawalRef{},
		deposits:                        map[string]*types.Deposit{},
		withdrawalCnt:                   big.NewInt(0),
		bss:                             &bankingSnapshotState{},
		scheduledTransfers:              map[int64][]scheduledTransfer{},
		recurringTransfers:              []*types.RecurringTransfer{},
		recurringTransfersMap:           map[string]*types.RecurringTransfer{},
		scheduledGovernanceTransfers:    map[int64][]*types.GovernanceTransfer{},
		recurringGovernanceTransfers:    []*types.GovernanceTransfer{},
		recurringGovernanceTransfersMap: map[string]*types.GovernanceTransfer{},
		transferFeeFactor:               num.DecimalZero(),
		minTransferQuantumMultiple:      num.DecimalZero(),
		minWithdrawQuantumMultiple:      num.DecimalZero(),
		marketActivityTracker:           marketActivityTracker,
		hashToStrategy:                  map[string]*dispatchStrategyCacheEntry{},
		bridgeState: &bridgeState{
			active: true,
		},
		feeDiscountPerPartyAndAsset:               map[partyAssetKey]*num.Uint{},
		pendingPerAssetAndPartyFeeDiscountUpdates: map[string]map[string]*num.Uint{},
		bridgeView: bridgeView,
	}
}

func (e *Engine) OnMaxFractionChanged(ctx context.Context, f num.Decimal) error {
	e.maxGovTransferFraction = f
	return nil
}

func (e *Engine) OnMaxAmountChanged(ctx context.Context, f num.Decimal) error {
	e.maxGovTransferQunatumMultiplier = f
	return nil
}

func (e *Engine) OnMinWithdrawQuantumMultiple(ctx context.Context, f num.Decimal) error {
	e.minWithdrawQuantumMultiple = f
	return nil
}

// ReloadConf updates the internal configuration.
func (e *Engine) ReloadConf(cfg Config) {
	e.log.Info("reloading configuration")
	if e.log.GetLevel() != cfg.Level.Get() {
		e.log.Info("updating log level",
			logging.String("old", e.log.GetLevel().String()),
			logging.String("new", cfg.Level.String()),
		)
		e.log.SetLevel(cfg.Level.Get())
	}

	e.cfg = cfg
}

func (e *Engine) OnEpoch(ctx context.Context, ep types.Epoch) {
	switch ep.Action {
	case proto.EpochAction_EPOCH_ACTION_START:
		e.currentEpoch = ep.Seq
		e.cleanupStaleDispatchStrategies()
	case proto.EpochAction_EPOCH_ACTION_END:
		e.distributeRecurringTransfers(ctx, e.currentEpoch)
		e.distributeRecurringGovernanceTransfers(ctx)
		e.applyPendingFeeDiscountsUpdates(ctx)
		e.sendTeamsStats(ctx, ep.Seq)
	default:
		e.log.Panic("epoch action should never be UNSPECIFIED", logging.String("epoch", ep.String()))
	}
}

func (e *Engine) OnTick(ctx context.Context, _ time.Time) {
	assetActionKeys := make([]string, 0, len(e.assetActs))
	for k := range e.assetActs {
		assetActionKeys = append(assetActionKeys, k)
	}
	sort.Strings(assetActionKeys)

	// iterate over asset actions deterministically
	for _, k := range assetActionKeys {
		v := e.assetActs[k]
		state := v.state.Load()
		if state == pendingState {
			continue
		}

		// get the action reference to ensure it's not a duplicate
		ref := v.getRef()
		refKey, err := getRefKey(ref)
		if err != nil {
			e.log.Error("failed to serialise ref",
				logging.String("asset-class", ref.Asset),
				logging.String("tx-hash", ref.Hash),
				logging.String("action", v.String()))
			continue
		}

		switch state {
		case okState:
			// check if this transaction have been seen before then
			if e.seen.Contains(refKey) {
				// do nothing of this transaction, just display an error
				e.log.Error("chain event reference a transaction already processed",
					logging.String("asset-class", ref.Asset),
					logging.String("tx-hash", ref.Hash),
					logging.String("action", v.String()))
			} else {
				// first time we seen this transaction, let's add iter
				e.seen.Add(refKey)
				if err := e.finalizeAction(ctx, v); err != nil {
					e.log.Error("unable to finalize action",
						logging.String("action", v.String()),
						logging.Error(err))
				}
			}

		case rejectedState:
			e.log.Error("network rejected banking action",
				logging.String("action", v.String()))
		}
		// delete anyway the action
		// at this point the action was either rejected, so we do no need
		// need to keep waiting for its validation, or accepted. in the case
		// it's accepted it's then sent to the given collateral function
		// (deposit, withdraw, allowlist), then an error can occur down the
		// line in the collateral but if that happened there's no way for
		// us to recover for this event, so we have no real reason to keep
		// it in memory
		delete(e.assetActs, k)
	}

	e.notary.OfferSignatures(
		types.NodeSignatureKindAssetWithdrawal, e.offerERC20NotarySignatures)

	// then process all scheduledTransfers
	if err := e.distributeScheduledTransfers(ctx); err != nil {
		e.log.Error("could not process scheduled transfers",
			logging.Error(err),
		)
	}

	// process governance transfers
	e.distributeScheduledGovernanceTransfers(ctx)
}

func (e *Engine) onCheckDone(i interface{}, valid bool) {
	aa, ok := i.(*assetAction)
	if !ok {
		return
	}

	newState := rejectedState
	if valid {
		newState = okState
	}
	aa.state.Store(newState)
}

func (e *Engine) getWithdrawalFromRef(ref *big.Int) (*types.Withdrawal, error) {
	// sort withdraws to check deterministically
	withdrawalsK := make([]string, 0, len(e.withdrawals))
	for k := range e.withdrawals {
		withdrawalsK = append(withdrawalsK, k)
	}
	sort.Strings(withdrawalsK)

	for _, k := range withdrawalsK {
		v := e.withdrawals[k]
		if v.ref.Cmp(ref) == 0 {
			return v.w, nil
		}
	}

	return nil, ErrNotMatchingWithdrawalForReference
}

func (e *Engine) finalizeAction(ctx context.Context, aa *assetAction) error {
	switch {
	case aa.IsBuiltinAssetDeposit():
		dep := e.deposits[aa.id]
		return e.finalizeDeposit(ctx, dep)
	case aa.IsERC20Deposit():
		dep := e.deposits[aa.id]
		return e.finalizeDeposit(ctx, dep)
	case aa.IsERC20AssetList():
		return e.finalizeAssetList(ctx, aa.erc20AL.VegaAssetID)
	case aa.IsERC20AssetLimitsUpdated():
		return e.finalizeAssetLimitsUpdated(ctx, aa.erc20AssetLimitsUpdated.VegaAssetID)
	case aa.IsERC20BridgeStopped():
		e.bridgeState.NewBridgeStopped(aa.blockHeight, aa.logIndex)
		return nil
	case aa.IsERC20BridgeResumed():
		e.bridgeState.NewBridgeResumed(aa.blockHeight, aa.logIndex)
		return nil
	default:
		return ErrUnknownAssetAction
	}
}

func (e *Engine) finalizeAssetList(ctx context.Context, assetID string) error {
	asset, err := e.assets.Get(assetID)
	if err != nil {
		e.log.Error("invalid asset id used to finalise asset list",
			logging.Error(err),
			logging.AssetID(assetID))
		return nil
	}
	if err := e.assets.Enable(ctx, assetID); err != nil {
		e.log.Error("unable to enable asset",
			logging.Error(err),
			logging.AssetID(assetID))
		return err
	}
	return e.col.EnableAsset(ctx, *asset.ToAssetType())
}

func (e *Engine) finalizeAssetLimitsUpdated(ctx context.Context, assetID string) error {
	asset, err := e.assets.Get(assetID)
	if err != nil {
		e.log.Error("invalid asset id used to finalise asset list",
			logging.Error(err),
			logging.AssetID(assetID))
		return nil
	}
	if err := e.assets.ApplyAssetUpdate(ctx, assetID); err != nil {
		e.log.Error("couldn't apply asset update",
			logging.Error(err),
			logging.AssetID(assetID))
		return err
	}
	return e.col.PropagateAssetUpdate(ctx, *asset.ToAssetType())
}

func (e *Engine) finalizeDeposit(ctx context.Context, d *types.Deposit) error {
	defer func() {
		e.broker.Send(events.NewDepositEvent(ctx, *d))
		// whatever happens, the deposit is in its final state (cancelled or finalized)
		delete(e.deposits, d.ID)
	}()
	res, err := e.col.Deposit(ctx, d.PartyID, d.Asset, d.Amount)
	if err != nil {
		d.Status = types.DepositStatusCancelled
		return err
	}

	d.Status = types.DepositStatusFinalized
	d.CreditDate = e.timeService.GetTimeNow().UnixNano()
	e.broker.Send(events.NewLedgerMovements(ctx, []*types.LedgerMovement{res}))
	return nil
}

func (e *Engine) finalizeWithdraw(
	ctx context.Context, w *types.Withdrawal,
) error {
	// always send the withdrawal event, don't delete it from the map because we
	// may still receive events
	defer func() {
		e.broker.Send(events.NewWithdrawalEvent(ctx, *w))
	}()

	res, err := e.col.Withdraw(ctx, w.PartyID, w.Asset, w.Amount.Clone())
	if err != nil {
		w.Status = types.WithdrawalStatusRejected
		return err
	}

	w.Status = types.WithdrawalStatusFinalized
	e.broker.Send(events.NewLedgerMovements(ctx, []*types.LedgerMovement{res}))
	return nil
}

func (e *Engine) newWithdrawal(
	id, partyID, asset string,
	amount *num.Uint,
	wext *types.WithdrawExt,
) (w *types.Withdrawal, ref *big.Int) {
	partyID = strings.TrimPrefix(partyID, "0x")
	asset = strings.TrimPrefix(asset, "0x")
	now := e.timeService.GetTimeNow()

	// reference needs to be an int, deterministic for the contracts
	ref = big.NewInt(0).Add(e.withdrawalCnt, big.NewInt(now.Unix()))
	e.withdrawalCnt.Add(e.withdrawalCnt, big.NewInt(1))
	w = &types.Withdrawal{
		ID:           id,
		Status:       types.WithdrawalStatusOpen,
		PartyID:      partyID,
		Asset:        asset,
		Amount:       amount,
		Ext:          wext,
		CreationDate: now.UnixNano(),
		Ref:          ref.String(),
	}
	return
}

func (e *Engine) newDeposit(
	id, partyID, asset string,
	amount *num.Uint,
	txHash string,
) *types.Deposit {
	partyID = strings.TrimPrefix(partyID, "0x")
	asset = strings.TrimPrefix(asset, "0x")
	return &types.Deposit{
		ID:           id,
		Status:       types.DepositStatusOpen,
		PartyID:      partyID,
		Asset:        asset,
		Amount:       amount,
		CreationDate: e.timeService.GetTimeNow().UnixNano(),
		TxHash:       txHash,
	}
}

func (e *Engine) GetDispatchStrategy(hash string) *proto.DispatchStrategy {
	ds, ok := e.hashToStrategy[hash]
	if !ok {
		e.log.Warn("could not find dispatch strategy in banking engine", logging.String("hash", hash))
		return nil
	}

	return ds.ds
}

// sendTeamsStats sends the teams statistics, which only account for games.
// This is located here not because this is where it should be, but because
// we don't know where to put it, as we need to have access to the dispatch
// strategy.
func (e *Engine) sendTeamsStats(ctx context.Context, seq uint64) {
	onlyTheseMarkets := map[string]interface{}{}
	allMarketsForAssets := map[string]interface{}{}
	for _, ds := range e.hashToStrategy {
		if ds.ds.EntityScope == proto.EntityScope_ENTITY_SCOPE_TEAMS {
			if len(ds.ds.Markets) != 0 {
				// If there is no markets specified, then we need gather data from
				// all markets tied to this asset.
				allMarketsForAssets[ds.ds.AssetForMetric] = nil
			} else {
				for _, market := range ds.ds.Markets {
					onlyTheseMarkets[market] = nil
				}
			}
		}
	}

	if len(allMarketsForAssets) == 0 && len(onlyTheseMarkets) == 0 {
		return
	}

	allMarketsForAssetsS := maps.Keys(allMarketsForAssets)
	slices.Sort(allMarketsForAssetsS)
	onlyTheseMarketsS := maps.Keys(onlyTheseMarkets)
	slices.Sort(onlyTheseMarketsS)

	teamsStats := e.marketActivityTracker.TeamStatsForMarkets(allMarketsForAssetsS, onlyTheseMarketsS)

	if len(teamsStats) > 0 {
		e.broker.Send(events.NewTeamsStatsUpdatedEvent(ctx, seq, teamsStats))
	}
}

func getRefKey(ref snapshot.TxRef) (string, error) {
	buf, err := vgproto.Marshal(&ref)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(crypto.Hash(buf)), nil
}

func newPendingState() *atomic.Uint32 {
	state := &atomic.Uint32{}
	state.Store(pendingState)
	return state
}
