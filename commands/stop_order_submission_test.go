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

package commands_test

import (
	"errors"
	"testing"

	"code.vegaprotocol.io/vega/commands"
	"code.vegaprotocol.io/vega/libs/ptr"
	"code.vegaprotocol.io/vega/protos/vega"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"

	"github.com/stretchr/testify/assert"
)

func TestCheckStopOrdersStubmission(t *testing.T) {
	cases := []struct {
		submission commandspb.StopOrdersSubmission
		errStr     string
	}{
		{
			submission: commandspb.StopOrdersSubmission{},
			errStr:     "must have at least one of rises above or falls below",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger (must have a stop order trigger)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_Price{
						Price: "",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.price (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_Price{
						Price: "-1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.price (must be positive)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_Price{
						Price: "asdsad",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.price (not a valid integer)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_Price{
						Price: "100",
					},
				},
			},
			errStr: "order_submission (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.trailing_percent_offset (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.trailing_percent_offset (must be between 0 and 1)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.trailing_percent_offset (must be between 0 and 1)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.89",
					},
				},
			},
			errStr: "order_submission (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "132213ds",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.trigger.trailing_percent_offset (not a valid float)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt: ptr.From(int64(1000)),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.expiry_strategy (expiry strategy required when expires_at set)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt:      ptr.From(int64(-1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.expires_at (must be positive)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_ExpiryStrategy(-1)),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.expiry_strategy (is not a valid value)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_ExpiryStrategy(-1)),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.expiry_strategy (is not a valid value)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_UNSPECIFIED),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required), stop_orders_submission.rises_below.expiry_strategy (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "order_submission (is required)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "stop_orders_submission.rises_below.order_submission.reduce_only (must be reduce only)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
				FallsBelow: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "e9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "* (market ID for falls below and rises above must be the same)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
				},
			},
			errStr: "",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "0.5"},
				},
			},
			errStr: "",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "BrokenString"},
				},
			},
			errStr: "stop_orders_submission.rises_above.size_override_value (is not a valid number)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "0.0"},
				},
			},
			errStr: "stop_orders_submission.rises_above.size_override_value (must be between 0 (excluded) and 1 (included))",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				RisesAbove: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "1.1"},
				},
			},
			errStr: "stop_orders_submission.rises_above.size_override_value (must be between 0 (excluded) and 1 (included))",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				FallsBelow: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "0.5"},
				},
			},
			errStr: "",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				FallsBelow: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "BrokenString"},
				},
			},
			errStr: "stop_orders_submission.falls_below.size_override_value (is not a valid number)",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				FallsBelow: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "0.0"},
				},
			},
			errStr: "stop_orders_submission.falls_below.size_override_value (must be between 0 (excluded) and 1 (included))",
		},
		{
			submission: commandspb.StopOrdersSubmission{
				FallsBelow: &commandspb.StopOrderSetup{
					OrderSubmission: &commandspb.OrderSubmission{
						MarketId:    "f9982447fb4128f9968f9981612c5ea85d19b62058ec2636efc812dcbbc745ca",
						Side:        vega.Side_SIDE_BUY,
						Size:        100,
						TimeInForce: vega.Order_TIME_IN_FORCE_IOC,
						Type:        vega.Order_TYPE_MARKET,
						ReduceOnly:  true,
					},
					ExpiresAt:      ptr.From(int64(1000)),
					ExpiryStrategy: ptr.From(vega.StopOrder_EXPIRY_STRATEGY_CANCELS),
					Trigger: &commandspb.StopOrderSetup_TrailingPercentOffset{
						TrailingPercentOffset: "0.1",
					},
					SizeOverrideSetting: ptr.From(vega.StopOrder_SIZE_OVERRIDE_SETTING_POSITION),
					SizeOverrideValue:   &vega.StopOrder_SizeOverrideValue{Percentage: "1.1"},
				},
			},
			errStr: "stop_orders_submission.falls_below.size_override_value (must be between 0 (excluded) and 1 (included))",
		},
	}

	for n, c := range cases {
		if len(c.errStr) <= 0 {
			assert.NoError(t, commands.CheckStopOrdersSubmission(&c.submission), n)
			continue
		}

		assert.Contains(t, checkStopOrdersSubmission(&c.submission).Error(), c.errStr, n)
	}
}

func checkStopOrdersSubmission(cmd *commandspb.StopOrdersSubmission) commands.Errors {
	err := commands.CheckStopOrdersSubmission(cmd)

	var e commands.Errors
	if ok := errors.As(err, &e); !ok {
		return commands.NewErrors()
	}

	return e
}
