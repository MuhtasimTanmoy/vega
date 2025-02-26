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

package models

import (
	"errors"
	"math"

	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"

	"code.vegaprotocol.io/quant/interfaces"
	pd "code.vegaprotocol.io/quant/pricedistribution"
	"code.vegaprotocol.io/quant/riskmodelbs"
)

var ErrMissingLogNormalParameter = errors.New("missing log normal parameters")

// LogNormal represent a future risk model.
type LogNormal struct {
	riskAversionParameter, tau num.Decimal
	params                     riskmodelbs.ModelParamsBS
	asset                      string

	distCache    interfaces.AnalyticalDistribution
	cachePrice   num.Decimal
	cacheHorizon num.Decimal
}

// NewBuiltinFutures instantiate a new builtin future.
func NewBuiltinFutures(pf *types.LogNormalRiskModel, asset string) (*LogNormal, error) {
	if pf.Params == nil {
		return nil, ErrMissingLogNormalParameter
	}
	// the quant stuff really needs to be updated to use the same num types...
	mu, _ := pf.Params.Mu.Float64()
	r, _ := pf.Params.R.Float64()
	sigma, _ := pf.Params.Sigma.Float64()
	return &LogNormal{
		riskAversionParameter: pf.RiskAversionParameter,
		tau:                   pf.Tau,
		cachePrice:            num.DecimalZero(),
		params: riskmodelbs.ModelParamsBS{
			Mu:    mu,
			R:     r,
			Sigma: sigma,
		},
		asset: asset,
	}, nil
}

// CalculateRiskFactors calls the risk model in order to get
// the new risk models.
func (f *LogNormal) CalculateRiskFactors() *types.RiskFactor {
	rav, _ := f.riskAversionParameter.Float64()
	tau, _ := f.tau.Float64()
	rawrf := riskmodelbs.RiskFactorsForward(rav, tau, f.params)
	return &types.RiskFactor{
		Long:  num.DecimalFromFloat(rawrf.Long),
		Short: num.DecimalFromFloat(rawrf.Short),
	}
}

// PriceRange returns the minimum and maximum price as implied by the model's probability distribution with horizon given by yearFraction (e.g. 0.5 for half a year) and probability level (e.g. 0.95 for 95%).
func (f *LogNormal) PriceRange(currentP, yFrac, probabilityLevel num.Decimal) (num.Decimal, num.Decimal) {
	dist := f.getDistribution(currentP, yFrac)
	// damn you quant!
	pl, _ := probabilityLevel.Float64()
	min, max := pd.PriceRange(dist, pl)
	return num.DecimalFromFloat(min), num.DecimalFromFloat(max)
}

// ProbabilityOfTrading of trading returns the probability of trading given current mark price, projection horizon expressed as year fraction, order price and side (isBid).
// Additional arguments control optional truncation of probability density outside the [minPrice,maxPrice] range.
func (f *LogNormal) ProbabilityOfTrading(currentP, orderP num.Decimal, minP, maxP num.Decimal, yFrac num.Decimal, isBid, applyMinMax bool) num.Decimal {
	dist := f.getDistribution(currentP, yFrac)
	min := math.Max(minP.InexactFloat64(), 0)
	// still, quant uses floats
	prob := pd.ProbabilityOfTrading(dist, orderP.InexactFloat64(), isBid, applyMinMax, min, maxP.InexactFloat64())
	if math.IsNaN(prob) {
		return num.DecimalZero()
	}

	return num.DecimalFromFloat(prob)
}

func (f *LogNormal) getDistribution(currentP num.Decimal, yFrac num.Decimal) interfaces.AnalyticalDistribution {
	if f.distCache == nil || !f.cachePrice.Equal(currentP) || !f.cacheHorizon.Equal(yFrac) {
		// quant still uses floats... sad
		yf, _ := yFrac.Float64()
		f.distCache = f.params.GetProbabilityDistribution(currentP.InexactFloat64(), yf)
	}
	return f.distCache
}

// GetProjectionHorizon returns the projection horizon used by the model for margin calculation pruposes.
func (f *LogNormal) GetProjectionHorizon() num.Decimal {
	return f.tau
}

func (f *LogNormal) DefaultRiskFactors() *types.RiskFactor {
	return &types.RiskFactor{
		Short: num.DecimalFromFloat(1),
		Long:  num.DecimalFromFloat(1),
	}
}
