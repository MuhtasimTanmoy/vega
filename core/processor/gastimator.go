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

package processor

import (
	"context"
	"math"

	"code.vegaprotocol.io/vega/core/blockchain/abci"
	"code.vegaprotocol.io/vega/core/txn"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
)

const (
	batchFactor    = 0.5
	pegCostFactor  = uint64(50)
	stopCostFactor = 0.2
	positionFactor = uint64(1)
	levelFactor    = 0.1
	high           = 10000
	medium         = 100
	low            = 1
)

type ExecEngine interface {
	GetMarketCounters() map[string]*types.MarketCounters
}

type Gastimator struct {
	minBlockCapacity uint64
	maxGas           uint64
	defaultGas       uint64
	exec             ExecEngine
	marketCounters   map[string]*types.MarketCounters
}

func NewGastimator(exec ExecEngine) *Gastimator {
	return &Gastimator{
		exec:           exec,
		marketCounters: map[string]*types.MarketCounters{},
	}
}

// OnBlockEnd is called at the end of the block to update the per market counters and return the max gas as defined by the network parameter.
func (g *Gastimator) OnBlockEnd() uint64 {
	g.marketCounters = g.exec.GetMarketCounters()
	return g.maxGas
}

// OnMaxGasUpdate updates the max gas from the network parameter.
func (g *Gastimator) OnMinBlockCapacityUpdate(ctx context.Context, minBlockCapacity *num.Uint) error {
	g.minBlockCapacity = minBlockCapacity.Uint64()
	return nil
}

// OnMaxGasUpdate updates the max gas from the network parameter.
func (g *Gastimator) OnMaxGasUpdate(ctx context.Context, max *num.Uint) error {
	g.maxGas = max.Uint64()
	return nil
}

// OnDefaultGasUpdate updates the default gas wanted per transaction.
func (g *Gastimator) OnDefaultGasUpdate(ctx context.Context, def *num.Uint) error {
	g.defaultGas = def.Uint64()
	return nil
}

// GetMaxGas returns the current value of max gas.
func (g *Gastimator) GetMaxGas() uint64 {
	return g.maxGas
}

func (g *Gastimator) GetPriority(tx abci.Tx) uint64 {
	switch tx.Command() {
	case txn.ProposeCommand, txn.BatchProposeCommand, txn.VoteCommand:
		return medium
	default:
		if tx.Command().IsValidatorCommand() {
			return high
		}
		return low
	}
}

func (g *Gastimator) CalcGasWantedForTx(tx abci.Tx) (uint64, error) {
	switch tx.Command() {
	case txn.SubmitOrderCommand:
		s := &commandspb.OrderSubmission{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		return g.orderGastimate(s.MarketId), nil
	case txn.AmendOrderCommand:
		s := &commandspb.OrderAmendment{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		return g.orderGastimate(s.MarketId), nil
	case txn.CancelOrderCommand:
		s := &commandspb.OrderCancellation{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		// if it is a cancel for one market
		if len(s.MarketId) > 0 && len(s.OrderId) > 0 {
			return g.cancelOrderGastimate(s.MarketId), nil
		}
		// if it is a cancel for all markets
		return g.defaultGas, nil
	case txn.BatchMarketInstructions:
		s := &commandspb.BatchMarketInstructions{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		return g.batchGastimate(s), nil
	case txn.StopOrdersSubmissionCommand:
		s := &commandspb.StopOrdersSubmission{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		var marketId string
		if s.FallsBelow != nil {
			marketId = s.FallsBelow.OrderSubmission.MarketId
		} else {
			marketId = s.RisesAbove.OrderSubmission.MarketId
		}

		return g.orderGastimate(marketId), nil
	case txn.StopOrdersCancellationCommand:
		s := &commandspb.StopOrdersCancellation{}
		if err := tx.Unmarshal(s); err != nil {
			return g.maxGas + 1, err
		}
		// if it is a cancel for one market
		if s.MarketId != nil && s.StopOrderId != nil {
			return g.cancelOrderGastimate(*s.MarketId), nil
		}
		// if it is a cancel for all markets
		return g.defaultGas, nil

	default:
		return g.defaultGas, nil
	}
}

// gasBatch =
// the full cost of the first cancellation (i.e. gasCancel)
// plus batchFactor times sum of all subsequent cancellations added together (each costing gasOrder)
// plus the full cost of the first amendment at gasOrder
// plus batchFactor sum of all subsequent amendments added together (each costing gasOrder)
// plus the full cost of the first limit order at gasOrder
// plus batchFactor sum of all subsequent limit orders added together (each costing gasOrder)
// gasBatch = min(maxGas-1,batchFactor).
func (g *Gastimator) batchGastimate(batch *commandspb.BatchMarketInstructions) uint64 {
	totalBatchGas := 0.0
	for i, os := range batch.Submissions {
		factor := batchFactor
		if i == 0 {
			factor = 1.0
		}
		orderGas := g.orderGastimate(os.MarketId)
		totalBatchGas += factor * float64(orderGas)
	}
	for i, os := range batch.Amendments {
		factor := batchFactor
		if i == 0 {
			factor = 1.0
		}
		orderGas := g.orderGastimate(os.MarketId)
		totalBatchGas += factor * float64(orderGas)
	}
	for i, os := range batch.Cancellations {
		factor := batchFactor
		if i == 0 {
			factor = 1.0
		}
		orderGas := g.cancelOrderGastimate(os.MarketId)
		totalBatchGas += factor * float64(orderGas)
	}
	for i, os := range batch.StopOrdersCancellation {
		factor := batchFactor
		if i == 0 {
			factor = 1.0
		}
		if os.MarketId == nil {
			totalBatchGas += factor * float64(g.defaultGas)
		}
		orderGas := g.cancelOrderGastimate(*os.MarketId)
		totalBatchGas += factor * float64(orderGas)
	}
	for i, os := range batch.StopOrdersSubmission {
		factor := batchFactor
		if i == 0 {
			factor = 1.0
		}
		var marketId string
		// if both are nil, marketId will be empty string, yielding default gas
		// the order is invalid, but validation is applied later.
		if os.FallsBelow != nil && os.FallsBelow.OrderSubmission != nil {
			marketId = os.FallsBelow.OrderSubmission.MarketId
		} else if os.RisesAbove != nil && os.RisesAbove.OrderSubmission != nil {
			marketId = os.RisesAbove.OrderSubmission.MarketId
		}
		orderGas := g.orderGastimate(marketId)
		totalBatchGas += factor * float64(orderGas)
	}
	return uint64(math.Min(float64(uint64(totalBatchGas)), float64(g.maxGas-1)))
}

// gasOrder = network.transaction.defaultgas + peg cost factor x pegs
// + position factor x positions
// + level factor x levels
// gasOrder = min(maxGas-1,gasOrder).
func (g *Gastimator) orderGastimate(marketID string) uint64 {
	if marketCounters, ok := g.marketCounters[marketID]; ok {
		return uint64(math.Min(float64(
			g.defaultGas+
				uint64(stopCostFactor*float64(marketCounters.StopOrderCounter))+
				pegCostFactor*marketCounters.PeggedOrderCounter+
				positionFactor*marketCounters.PositionCount+
				uint64(levelFactor*float64(marketCounters.OrderbookLevelCount))),
			math.Max(1.0, float64(g.maxGas/g.minBlockCapacity-1))))
	}
	return g.defaultGas
}

// gasCancel = network.transaction.defaultgas + peg cost factor x pegs
// + level factor x levels
// gasCancel = min(maxGas-1,gasCancel).
func (g *Gastimator) cancelOrderGastimate(marketID string) uint64 {
	if marketCounters, ok := g.marketCounters[marketID]; ok {
		return uint64(math.Min(float64(
			g.defaultGas+
				uint64(stopCostFactor*float64(marketCounters.StopOrderCounter))+
				pegCostFactor*marketCounters.PeggedOrderCounter+
				uint64(0.1*float64(marketCounters.OrderbookLevelCount))),
			math.Max(1.0, float64(g.maxGas/g.minBlockCapacity-1))))
	}
	return g.defaultGas
}
