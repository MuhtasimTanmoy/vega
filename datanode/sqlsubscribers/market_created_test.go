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

package sqlsubscribers_test

import (
	"context"
	"testing"

	"code.vegaprotocol.io/vega/core/datasource"
	dstypes "code.vegaprotocol.io/vega/core/datasource/common"
	"code.vegaprotocol.io/vega/core/datasource/external/signedoracle"
	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/datanode/sqlsubscribers"
	"code.vegaprotocol.io/vega/datanode/sqlsubscribers/mocks"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/protos/vega"
	datapb "code.vegaprotocol.io/vega/protos/vega/data/v1"

	"github.com/golang/mock/gomock"
)

func Test_MarketCreated_Push(t *testing.T) {
	t.Run("MarketCreatedEvent should call market SQL store Add", shouldCallMarketSQLStoreAdd)
}

func shouldCallMarketSQLStoreAdd(t *testing.T) {
	ctrl := gomock.NewController(t)

	store := mocks.NewMockMarketsStore(ctrl)

	store.EXPECT().Upsert(context.Background(), gomock.Any()).Times(1)
	subscriber := sqlsubscribers.NewMarketCreated(store)
	subscriber.Flush(context.Background())
	subscriber.Push(context.Background(), events.NewMarketCreatedEvent(context.Background(), getTestMarket(false)))
}

func getTestMarket(termInt bool) types.Market {
	term := &datasource.Spec{
		ID:        "",
		CreatedAt: 0,
		UpdatedAt: 0,
		Data: datasource.NewDefinition(
			datasource.ContentTypeOracle,
		).SetOracleConfig(
			&signedoracle.SpecConfiguration{
				Signers: nil,
				Filters: nil,
			},
		),
		Status: 0,
	}

	if termInt {
		term = &datasource.Spec{
			ID:        "",
			CreatedAt: 0,
			UpdatedAt: 0,
			Data: datasource.NewDefinition(
				datasource.ContentTypeInternalTimeTermination,
			).SetTimeTriggerConditionConfig(
				[]*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_EQUALS,
						Value:    "test-value",
					},
				},
			),
			Status: 0,
		}
	}

	return types.Market{
		ID: "DEADBEEF",
		TradableInstrument: &types.TradableInstrument{
			Instrument: &types.Instrument{
				ID:   "TEST_INSTRUMENT",
				Code: "TEST",
				Name: "Test Instrument",
				Metadata: &types.InstrumentMetadata{
					Tags: []string{"AAA", "BBB"},
				},
				Product: &types.InstrumentFuture{
					Future: &types.Future{
						SettlementAsset: "",
						QuoteName:       "",
						DataSourceSpecForSettlementData: &datasource.Spec{
							ID:        "",
							CreatedAt: 0,
							UpdatedAt: 0,
							Data: datasource.NewDefinition(
								datasource.ContentTypeOracle,
							).SetOracleConfig(
								&signedoracle.SpecConfiguration{
									Signers: nil,
									Filters: nil,
								},
							),
							Status: 0,
						},
						DataSourceSpecForTradingTermination: term,
						DataSourceSpecBinding: &datasource.SpecBindingForFuture{
							SettlementDataProperty:     "",
							TradingTerminationProperty: "",
						},
					},
				},
			},
			MarginCalculator: &types.MarginCalculator{
				ScalingFactors: &types.ScalingFactors{
					SearchLevel:       num.DecimalZero(),
					InitialMargin:     num.DecimalZero(),
					CollateralRelease: num.DecimalZero(),
				},
			},
			RiskModel: &types.TradableInstrumentSimpleRiskModel{
				SimpleRiskModel: &types.SimpleRiskModel{
					Params: &types.SimpleModelParams{
						FactorLong:           num.DecimalZero(),
						FactorShort:          num.DecimalZero(),
						MaxMoveUp:            num.DecimalZero(),
						MinMoveDown:          num.DecimalZero(),
						ProbabilityOfTrading: num.DecimalZero(),
					},
				},
			},
		},
		DecimalPlaces:         16,
		PositionDecimalPlaces: 8,
		Fees: &types.Fees{
			Factors: &types.FeeFactors{
				MakerFee:          num.DecimalZero(),
				InfrastructureFee: num.DecimalZero(),
				LiquidityFee:      num.DecimalZero(),
			},
			LiquidityFeeSettings: &types.LiquidityFeeSettings{
				Method: vega.LiquidityFeeSettings_METHOD_MARGINAL_COST,
			},
		},
		OpeningAuction: nil,
		PriceMonitoringSettings: &types.PriceMonitoringSettings{
			Parameters: &types.PriceMonitoringParameters{
				Triggers: []*types.PriceMonitoringTrigger{
					{
						Horizon:          0,
						HorizonDec:       num.DecimalZero(),
						Probability:      num.NewDecimalFromFloat(0.99),
						AuctionExtension: 0,
					},
				},
			},
		},
		LiquidityMonitoringParameters: &types.LiquidityMonitoringParameters{
			TargetStakeParameters: &types.TargetStakeParameters{
				TimeWindow:    0,
				ScalingFactor: num.DecimalZero(),
			},
		},
		TradingMode: 0,
		State:       0,
		MarketTimestamps: &types.MarketTimestamps{
			Proposed: 0,
			Pending:  0,
			Open:     0,
			Close:    0,
		},
		LiquiditySLAParams: &types.LiquiditySLAParams{
			PriceRange: num.DecimalFromFloat(0.95),
		},
	}
}
