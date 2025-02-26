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

package price

import (
	"context"
	"errors"

	"code.vegaprotocol.io/vega/core/types/statevar"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/logging"
)

type boundFactorsConverter struct{}

func (boundFactorsConverter) BundleToInterface(kvb *statevar.KeyValueBundle) statevar.StateVariableResult {
	return &boundFactors{
		up:   kvb.KVT[0].Val.(*statevar.DecimalVector).Val,
		down: kvb.KVT[1].Val.(*statevar.DecimalVector).Val,
	}
}

func (boundFactorsConverter) InterfaceToBundle(res statevar.StateVariableResult) *statevar.KeyValueBundle {
	value := res.(*boundFactors)
	return &statevar.KeyValueBundle{
		KVT: []statevar.KeyValueTol{
			{Key: "up", Val: &statevar.DecimalVector{Val: value.up}, Tolerance: tolerance},
			{Key: "down", Val: &statevar.DecimalVector{Val: value.down}, Tolerance: tolerance},
		},
	}
}

func (e *Engine) IsBoundFactorsInitialised() bool {
	return e.boundFactorsInitialised
}

// startCalcPriceRanges kicks off the bounds factors calculation, done asynchronously for illustration.
func (e *Engine) startCalcPriceRanges(eventID string, endOfCalcCallback statevar.FinaliseCalculation) {
	if e.log.GetLevel() <= logging.DebugLevel {
		e.log.Debug("price range factors calculation started", logging.String("event-id", eventID))
	}

	down := make([]num.Decimal, 0, len(e.bounds))
	up := make([]num.Decimal, 0, len(e.bounds))

	// if we have no reference price, just abort and wait for the next round
	if len(e.pricesPast) < 1 && len(e.pricesNow) < 1 {
		e.log.Info("no reference price available for market - cannot calculate price ranges", logging.String("event-id", eventID))
		endOfCalcCallback.CalculationFinished(eventID, nil, errors.New("no reference price available for market - cannot calculate price ranges"))
		return
	}

	for _, b := range e.bounds {
		ref := e.getRefPriceNoUpdate(b.Trigger.Horizon)
		minPrice, maxPrice := e.riskModel.PriceRange(ref, e.fpHorizons[b.Trigger.Horizon], b.Trigger.Probability)
		down = append(down, minPrice.Div(ref))
		up = append(up, maxPrice.Div(ref))
	}
	res := &boundFactors{
		down: down,
		up:   up,
	}

	if e.log.GetLevel() <= logging.DebugLevel {
		e.log.Debug("price range factors calculation completed", logging.String("event-id", eventID), logging.String("asset", e.asset), logging.String("market", e.market))
	}
	endOfCalcCallback.CalculationFinished(eventID, res, nil)
}

// updatePriceBounds is called back from the state variable consensus engine when consensus is reached for the down/up factors and updates the price bounds.
func (e *Engine) updatePriceBounds(ctx context.Context, res statevar.StateVariableResult) error {
	bRes := res.(*boundFactors)
	e.updateFactors(bRes.down, bRes.up)
	if e.log.GetLevel() <= logging.DebugLevel {
		e.log.Debug("consensus reached for price ranges", logging.String("asset", e.asset), logging.String("market", e.market))
	}
	return nil
}

func (e *Engine) updateFactors(down, up []num.Decimal) {
	for i, b := range e.bounds {
		if !b.Active {
			continue
		}

		b.DownFactor = down[i]
		b.UpFactor = up[i]
	}
	e.boundFactorsInitialised = true
	// force invalidation of the price range cache
	if len(e.pricesNow) > 0 {
		e.getCurrentPriceRanges(true)
	}

	e.clearStalePrices()
	e.stateChanged = true
}
