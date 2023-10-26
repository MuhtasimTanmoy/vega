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

package referral

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	snapshotpb "code.vegaprotocol.io/vega/protos/vega/snapshot/v1"
)

const MaximumWindowLength uint64 = 100

var (
	ErrIsAlreadyAReferee = func(party types.PartyID) error {
		return fmt.Errorf("party %q has already been referred", party)
	}

	ErrIsAlreadyAReferrer = func(party types.PartyID) error {
		return fmt.Errorf("party %q is already a referrer", party)
	}

	ErrUnknownReferralCode = func(code types.ReferralSetID) error {
		return fmt.Errorf("no referral set for referral code %q", code)
	}

	ErrNotEligibleForReferralRewards = func(party string, balance, required *num.Uint) error {
		return fmt.Errorf("party %q not eligible for referral rewards, staking balance required of %s got %s", party, required.String(), balance.String())
	}

	ErrNotPartOfAReferralSet = func(party types.PartyID) error {
		return fmt.Errorf("party %q is not part of a referral set", party)
	}

	ErrUnknownSetID = errors.New("unknown set ID")
)

type Engine struct {
	broker                Broker
	marketActivityTracker MarketActivityTracker
	timeSvc               TimeService

	currentEpoch uint64
	staking      StakingBalances

	// referralSetsNotionalVolumes tracks the notional volumes per teams. Each
	// element of the num.Uint array is an epoch.
	referralSetsNotionalVolumes *runningVolumes
	factorsByReferee            map[types.PartyID]*types.RefereeStats

	// referralProgramMinStakedVegaTokens is the minimum number of token a party
	// must possess to become and stay a referrer.
	referralProgramMinStakedVegaTokens *num.Uint

	rewardProportionUpdate num.Decimal

	// latestProgramVersion tracks the latest version of the program. It used to
	// value any new program that comes in. It starts at 1.
	// It's incremented every time an update is received. Therefore, if, during
	// the same epoch, we have 2 successive updates, this field will be incremented
	// twice.
	latestProgramVersion uint64

	// currentProgram is the program currently in used against which the reward
	// are computed.
	// It's `nil` is there is none.
	currentProgram *types.ReferralProgram

	// programHasEnded tells if the current program has reached it's
	// end. It's flipped at the end of the epoch.
	programHasEnded bool
	// newProgram is the program born from the last enacted UpdateReferralProgram
	// proposal to apply at the start of the next epoch.
	// It's `nil` is there is none.
	newProgram *types.ReferralProgram

	sets      map[types.ReferralSetID]*types.ReferralSet
	referrers map[types.PartyID]types.ReferralSetID
	referees  map[types.PartyID]types.ReferralSetID

	minBalanceForApplyCode *num.Uint
}

func (e *Engine) CheckSufficientBalanceForApplyReferralCode(party types.PartyID, balance *num.Uint) error {
	if balance.LT(e.minBalanceForApplyCode) {
		return fmt.Errorf("party %q does not have sufficient balance to apply referral code, required balance %s available balance %s", party, e.minBalanceForApplyCode.String(), balance.String())
	}
	return nil
}

func (e *Engine) OnMinBalanceForApplyReferralCodeUpdated(_ context.Context, min *num.Uint) error {
	e.minBalanceForApplyCode = min
	return nil
}

func (e *Engine) GetReferrer(referee types.PartyID) (types.PartyID, error) {
	setID, ok := e.referees[referee]
	if !ok {
		return "", ErrNotPartOfAReferralSet(referee)
	}

	return e.sets[setID].Referrer.PartyID, nil
}

func (e *Engine) SetExists(setID types.ReferralSetID) bool {
	_, ok := e.sets[setID]
	return ok
}

func (e *Engine) CreateReferralSet(ctx context.Context, party types.PartyID, deterministicSetID types.ReferralSetID) error {
	if _, ok := e.referrers[party]; ok {
		return ErrIsAlreadyAReferrer(party)
	}
	if _, ok := e.referees[party]; ok {
		return ErrIsAlreadyAReferee(party)
	}

	now := e.timeSvc.GetTimeNow()

	newSet := types.ReferralSet{
		ID:        deterministicSetID,
		CreatedAt: now,
		UpdatedAt: now,
		Referrer: &types.Membership{
			PartyID:        party,
			JoinedAt:       now,
			StartedAtEpoch: e.currentEpoch,
		},
		CurrentRewardFactor:            num.DecimalZero(),
		CurrentRewardsMultiplier:       num.DecimalZero(),
		CurrentRewardsFactorMultiplier: num.DecimalZero(),
	}

	e.sets[deterministicSetID] = &newSet
	e.referrers[party] = deterministicSetID

	e.broker.Send(events.NewReferralSetCreatedEvent(ctx, &newSet))

	return nil
}

func (e *Engine) ApplyReferralCode(ctx context.Context, party types.PartyID, setID types.ReferralSetID) error {
	if _, ok := e.referrers[party]; ok {
		return ErrIsAlreadyAReferrer(party)
	}

	var (
		isSwitching bool
		prevSet     types.ReferralSetID
		ok          bool
	)
	if prevSet, ok = e.referees[party]; ok {
		isSwitching = e.canSwitchReferralSet(party, setID)
		if !isSwitching {
			return ErrIsAlreadyAReferee(party)
		}
	}

	set, ok := e.sets[setID]
	if !ok {
		return ErrUnknownReferralCode(setID)
	}

	now := e.timeSvc.GetTimeNow()

	set.UpdatedAt = now

	membership := &types.Membership{
		PartyID:        party,
		JoinedAt:       now,
		StartedAtEpoch: e.currentEpoch,
	}
	set.Referees = append(set.Referees, membership)

	e.referees[party] = set.ID

	e.broker.Send(events.NewRefereeJoinedReferralSetEvent(ctx, setID, membership))

	if isSwitching {
		e.removeFromSet(party, prevSet)
	}

	return nil
}

func (e *Engine) removeFromSet(party types.PartyID, prevSet types.ReferralSetID) {
	set := e.sets[prevSet]

	var idx int
	for i, r := range set.Referees {
		if r.PartyID == party {
			idx = i
			break
		}
	}

	set.Referees = append(set.Referees[:idx], set.Referees[idx+1:]...)
}

func (e *Engine) UpdateProgram(newProgram *types.ReferralProgram) {
	e.latestProgramVersion += 1
	e.newProgram = newProgram

	sort.Slice(e.newProgram.BenefitTiers, func(i, j int) bool {
		return e.newProgram.BenefitTiers[i].MinimumRunningNotionalTakerVolume.LT(e.newProgram.BenefitTiers[j].MinimumRunningNotionalTakerVolume)
	})

	sort.Slice(e.newProgram.StakingTiers, func(i, j int) bool {
		return e.newProgram.StakingTiers[i].MinimumStakedTokens.LT(e.newProgram.StakingTiers[j].MinimumStakedTokens)
	})

	e.newProgram.Version = e.latestProgramVersion
}

func (e *Engine) HasProgramEnded() bool {
	return e.programHasEnded
}

func (e *Engine) ReferralDiscountFactorForParty(party types.PartyID) num.Decimal {
	if e.programHasEnded {
		return num.DecimalZero()
	}

	factors, ok := e.factorsByReferee[party]
	if !ok {
		return num.DecimalZero()
	}

	return factors.DiscountFactor
}

func (e *Engine) RewardsFactorForParty(party types.PartyID) num.Decimal {
	if e.programHasEnded {
		return num.DecimalZero()
	}

	setID, ok := e.referees[party]
	if !ok {
		return num.DecimalZero()
	}

	return e.sets[setID].CurrentRewardFactor
}

func (e *Engine) RewardsFactorMultiplierAppliedForParty(party types.PartyID) num.Decimal {
	setID, ok := e.referees[party]
	if !ok {
		return num.DecimalZero()
	}

	return e.sets[setID].CurrentRewardsFactorMultiplier
}

func (e *Engine) RewardsMultiplierForParty(party types.PartyID) num.Decimal {
	setID, ok := e.referees[party]
	if !ok {
		return num.DecimalZero()
	}

	return e.sets[setID].CurrentRewardsMultiplier
}

func (e *Engine) OnReferralProgramMaxReferralRewardProportionUpdate(_ context.Context, value num.Decimal) error {
	e.rewardProportionUpdate = value
	return nil
}

func (e *Engine) OnReferralProgramMinStakedVegaTokensUpdate(_ context.Context, value *num.Uint) error {
	e.referralProgramMinStakedVegaTokens = value
	return nil
}

func (e *Engine) OnReferralProgramMaxPartyNotionalVolumeByQuantumPerEpochUpdate(_ context.Context, value *num.Uint) error {
	e.referralSetsNotionalVolumes.maxPartyNotionalVolumeByQuantumPerEpoch = value
	return nil
}

func (e *Engine) OnEpoch(ctx context.Context, ep types.Epoch) {
	switch ep.Action {
	case vegapb.EpochAction_EPOCH_ACTION_START:
		e.currentEpoch = ep.Seq
		e.applyProgramUpdate(ctx, ep.StartTime, ep.Seq)
	case vegapb.EpochAction_EPOCH_ACTION_END:
		e.computeReferralSetsStats(ctx, ep)
	}
}

func (e *Engine) OnEpochRestore(_ context.Context, ep types.Epoch) {
	if ep.Action == vegapb.EpochAction_EPOCH_ACTION_START {
		e.currentEpoch = ep.Seq
	}
}

func (e *Engine) calculateRewardsFactorMultiplierForParty(multiplierForParty, rewardsFactorForParty num.Decimal) num.Decimal {
	return num.MinD(
		rewardsFactorForParty.Mul(multiplierForParty),
		e.rewardProportionUpdate,
	)
}

func (e *Engine) calculateMultiplierForParty(setID types.ReferralSetID) num.Decimal {
	if e.isSetEligible(setID) != nil {
		return num.DecimalZero()
	}

	balance, _ := e.staking.GetAvailableBalance(
		string(e.sets[setID].Referrer.PartyID),
	)

	multiplier := num.DecimalOne()
	for _, v := range e.currentProgram.StakingTiers {
		if balance.LTE(v.MinimumStakedTokens) {
			break
		}
		multiplier = v.ReferralRewardMultiplier
	}

	return multiplier
}

func (e *Engine) applyProgramUpdate(ctx context.Context, startEpochTime time.Time, epoch uint64) {
	if e.newProgram != nil {
		if e.currentProgram != nil {
			e.endCurrentProgram()
			e.startNewProgram()
			e.notifyReferralProgramUpdated(ctx, startEpochTime, epoch)
		} else {
			e.startNewProgram()
			e.notifyReferralProgramStarted(ctx, startEpochTime, epoch)
		}
	}

	// This handles a edge case where the new program ends before the next
	// epoch starts. It can happen when the proposal updating the referral
	// program specifies an end date that is within the same epoch as the enactment
	// time.
	if e.currentProgram != nil && !e.currentProgram.EndOfProgramTimestamp.IsZero() && !e.currentProgram.EndOfProgramTimestamp.After(startEpochTime) {
		e.notifyReferralProgramEnded(ctx, startEpochTime, epoch)
		e.endCurrentProgram()
	}
}

func (e *Engine) endCurrentProgram() {
	e.programHasEnded = true
	e.currentProgram = nil
}

func (e *Engine) startNewProgram() {
	e.programHasEnded = false
	e.currentProgram = e.newProgram
	e.newProgram = nil
}

func (e *Engine) notifyReferralProgramStarted(ctx context.Context, epochTime time.Time, epoch uint64) {
	e.broker.Send(events.NewReferralProgramStartedEvent(ctx, e.currentProgram, epochTime, epoch))
}

func (e *Engine) notifyReferralProgramUpdated(ctx context.Context, epochTime time.Time, epoch uint64) {
	e.broker.Send(events.NewReferralProgramUpdatedEvent(ctx, e.currentProgram, epochTime, epoch))
}

func (e *Engine) notifyReferralProgramEnded(ctx context.Context, epochTime time.Time, epoch uint64) {
	e.broker.Send(events.NewReferralProgramEndedEvent(ctx, e.currentProgram.Version, e.currentProgram.ID, epochTime, epoch))
}

func (e *Engine) notifyReferralSetStatsUpdated(ctx context.Context, stats *types.ReferralSetStats) {
	e.broker.Send(events.NewReferralSetStatsUpdatedEvent(ctx, stats))
}

func (e *Engine) load(referralProgramState *types.PayloadReferralProgramState) {
	if referralProgramState.CurrentProgram != nil {
		e.currentProgram = types.NewReferralProgramFromProto(referralProgramState.CurrentProgram)
	}
	if referralProgramState.NewProgram != nil {
		e.newProgram = types.NewReferralProgramFromProto(referralProgramState.NewProgram)
	}
	e.latestProgramVersion = referralProgramState.LastProgramVersion
	e.programHasEnded = referralProgramState.ProgramHasEnded
	e.loadReferralSetsFromSnapshot(referralProgramState.Sets)
	e.loadFactorsByReferee(referralProgramState.FactorByReferee)
}

func (e *Engine) loadFactorsByReferee(factors []*snapshotpb.FactorByReferee) {
	e.factorsByReferee = make(map[types.PartyID]*types.RefereeStats, len(factors))
	for _, fbr := range factors {
		party := types.PartyID(fbr.Party)
		discountFactor, _ := num.UnmarshalBinaryDecimal(fbr.DiscountFactor)
		takerVolume := num.UintFromBytes(fbr.TakerVolume)
		e.factorsByReferee[party] = &types.RefereeStats{
			DiscountFactor: discountFactor,
			TakerVolume:    takerVolume,
		}
	}
}

func (e *Engine) loadReferralSetsFromSnapshot(setsProto []*snapshotpb.ReferralSet) {
	for _, setProto := range setsProto {
		setID := types.ReferralSetID(setProto.Id)

		newSet := &types.ReferralSet{
			ID:        setID,
			CreatedAt: time.Unix(0, setProto.CreatedAt),
			UpdatedAt: time.Unix(0, setProto.UpdatedAt),
			Referrer: &types.Membership{
				PartyID:        types.PartyID(setProto.Referrer.PartyId),
				JoinedAt:       time.Unix(0, setProto.Referrer.JoinedAt),
				StartedAtEpoch: setProto.Referrer.StartedAtEpoch,
			},
			CurrentRewardFactor:            num.MustDecimalFromString(setProto.CurrentRewardFactor),
			CurrentRewardsMultiplier:       num.MustDecimalFromString(setProto.CurrentRewardsMultiplier),
			CurrentRewardsFactorMultiplier: num.MustDecimalFromString(setProto.CurrentRewardsFactorMultiplier),
		}

		e.referrers[types.PartyID(setProto.Referrer.PartyId)] = setID

		for _, r := range setProto.Referees {
			partyID := types.PartyID(r.PartyId)
			e.referees[partyID] = setID
			newSet.Referees = append(newSet.Referees,
				&types.Membership{
					PartyID:        partyID,
					JoinedAt:       time.Unix(0, r.JoinedAt),
					StartedAtEpoch: r.StartedAtEpoch,
				},
			)
		}

		runningVolumes := make([]*notionalVolume, 0, len(setProto.RunningVolumes))
		for _, volume := range setProto.RunningVolumes {
			var volumeNum *num.Uint
			if len(volume.Volume) > 0 {
				volumeNum = num.UintFromBytes(volume.Volume)
			}
			runningVolumes = append(runningVolumes, &notionalVolume{
				epoch: volume.Epoch,
				value: volumeNum,
			})
		}

		// set only if the running volume is not empty, or it will panic
		// down the line when trying to add new ones.
		// the creation of runningVolumeBySet is done in the Add method of the
		// runningVolumes type.
		if len(runningVolumes) > 0 {
			e.referralSetsNotionalVolumes.runningVolumesBySet[setID] = runningVolumes
		}

		e.sets[setID] = newSet
	}
}

func (e *Engine) computeReferralSetsStats(ctx context.Context, epoch types.Epoch) {
	priorEpoch := uint64(0)
	if epoch.Seq > MaximumWindowLength {
		priorEpoch = epoch.Seq - MaximumWindowLength
	}
	e.referralSetsNotionalVolumes.RemovePriorEpoch(priorEpoch)

	for partyID, setID := range e.referrers {
		volumeForEpoch := e.marketActivityTracker.NotionalTakerVolumeForParty(string(partyID))
		e.referralSetsNotionalVolumes.Add(epoch.Seq, setID, volumeForEpoch)
	}

	partiesTakerVolume := map[types.PartyID]*num.Uint{}

	for partyID, setID := range e.referees {
		volumeForEpoch := e.marketActivityTracker.NotionalTakerVolumeForParty(string(partyID))
		e.referralSetsNotionalVolumes.Add(epoch.Seq, setID, volumeForEpoch)
		partiesTakerVolume[partyID] = volumeForEpoch
	}

	if e.programHasEnded {
		return
	}

	e.computeFactorsByReferee(ctx, epoch.Seq, partiesTakerVolume)
}

func (e *Engine) computeFactorsByReferee(ctx context.Context, epoch uint64, takerVolumePerParty map[types.PartyID]*num.Uint) {
	e.factorsByReferee = map[types.PartyID]*types.RefereeStats{}

	allStats := map[types.ReferralSetID]*types.ReferralSetStats{}
	tiersLen := len(e.currentProgram.BenefitTiers)

	for setID, set := range e.sets {
		setStats := &types.ReferralSetStats{
			AtEpoch:                  epoch,
			SetID:                    setID,
			RefereesStats:            map[types.PartyID]*types.RefereeStats{},
			ReferralSetRunningVolume: e.referralSetsNotionalVolumes.RunningSetVolumeForWindow(setID, e.currentProgram.WindowLength),
			RewardFactor:             num.DecimalZero(),
		}

		for i := tiersLen - 1; i >= 0; i-- {
			tier := e.currentProgram.BenefitTiers[i]
			if setStats.RewardFactor.IsZero() && setStats.ReferralSetRunningVolume.GTE(tier.MinimumRunningNotionalTakerVolume) {
				setStats.RewardFactor = tier.ReferralRewardFactor
				break
			}
		}

		balance, _ := e.staking.GetAvailableBalance(set.Referrer.PartyID.String())

		if balance.GTE(e.referralProgramMinStakedVegaTokens) {
			setStats.WasEligible = true
		}

		setStats.RewardsMultiplier = e.calculateMultiplierForParty(setID)
		setStats.RewardsFactorMultiplier = e.calculateRewardsFactorMultiplierForParty(setStats.RewardsMultiplier, setStats.RewardFactor)

		e.sets[setID].CurrentRewardFactor = setStats.RewardFactor
		e.sets[setID].CurrentRewardsMultiplier = setStats.RewardsMultiplier
		e.sets[setID].CurrentRewardsFactorMultiplier = setStats.RewardsFactorMultiplier

		allStats[setID] = setStats
	}

	for party, setID := range e.referees {
		set := e.sets[setID]
		epochCount := uint64(0)

		for _, referee := range set.Referees {
			if referee.PartyID == party {
				epochCount = e.currentEpoch - referee.StartedAtEpoch + 1
				break
			}
		}

		setStats := allStats[setID]
		runningVolumeForSet := setStats.ReferralSetRunningVolume

		partyTakerVolume := num.UintZero()
		if takerVolume := takerVolumePerParty[party]; takerVolume != nil {
			partyTakerVolume = takerVolume
		}
		refereeStats := &types.RefereeStats{
			TakerVolume:    partyTakerVolume,
			DiscountFactor: num.DecimalZero(),
		}
		e.factorsByReferee[party] = refereeStats
		setStats.RefereesStats[party] = refereeStats

		if !setStats.WasEligible {
			continue
		}

		for i := tiersLen - 1; i >= 0; i-- {
			tier := e.currentProgram.BenefitTiers[i]
			if refereeStats.DiscountFactor.IsZero() && epochCount >= tier.MinimumEpochs.Uint64() && runningVolumeForSet.GTE(tier.MinimumRunningNotionalTakerVolume) {
				refereeStats.DiscountFactor = tier.ReferralDiscountFactor
				break
			}
		}
	}

	setIDs := maps.Keys(allStats)
	slices.Sort(setIDs)
	for _, setID := range setIDs {
		e.notifyReferralSetStatsUpdated(ctx, allStats[setID])
	}
}

func (e *Engine) isSetEligible(setID types.ReferralSetID) error {
	set, ok := e.sets[setID]
	if !ok {
		return ErrUnknownSetID
	}

	return e.isPartyEligible(string(set.Referrer.PartyID))
}

func (e *Engine) canSwitchReferralSet(party types.PartyID, newSet types.ReferralSetID) bool {
	currentSet := e.referees[party]
	if currentSet == newSet {
		return false
	}

	// if the current set is not eligible for rewards,
	// then we can switch
	if e.isSetEligible(currentSet) != nil {
		return true
	}

	return false
}

func (e *Engine) isPartyEligible(party string) error {
	// Ignore error, function returns zero balance anyway.
	balance, _ := e.staking.GetAvailableBalance(party)

	if balance.GTE(e.referralProgramMinStakedVegaTokens) {
		return nil
	}

	return ErrNotEligibleForReferralRewards(party, balance, e.referralProgramMinStakedVegaTokens)
}

func NewEngine(broker Broker, timeSvc TimeService, mat MarketActivityTracker, staking StakingBalances) *Engine {
	engine := &Engine{
		broker:                broker,
		timeSvc:               timeSvc,
		marketActivityTracker: mat,

		// There is no program yet, so we mark it has ended so consumer of this
		// engine can know there is no reward computation to be done.
		programHasEnded: true,

		referralSetsNotionalVolumes: newRunningVolumes(),

		referralProgramMinStakedVegaTokens: num.UintZero(),

		sets:      map[types.ReferralSetID]*types.ReferralSet{},
		referrers: map[types.PartyID]types.ReferralSetID{},
		referees:  map[types.PartyID]types.ReferralSetID{},
		staking:   staking,
	}

	return engine
}
