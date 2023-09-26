// Copyright (c) 2022 Gobalsky Labs Limited
//
// Use of this software is governed by the Business Source License included
// in the LICENSE.VEGA file and at https://www.mariadb.com/bsl11.
//
// Change Date: 18 months from the later of the date of the first publicly
// available Distribution of this version of the repository, and 25 June 2022.
//
// On the date above, in accordance with the Business Source License, use
// of this software will be governed by version 3 or later of the GNU General
// Public License.

package products_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/core/datasource"
	dscommon "code.vegaprotocol.io/vega/core/datasource/common"
	dstypes "code.vegaprotocol.io/vega/core/datasource/common"
	"code.vegaprotocol.io/vega/core/datasource/external/signedoracle"
	"code.vegaprotocol.io/vega/core/datasource/spec"
	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/products"
	"code.vegaprotocol.io/vega/core/products/mocks"
	"code.vegaprotocol.io/vega/core/types"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/libs/ptr"
	"code.vegaprotocol.io/vega/logging"
	datapb "code.vegaprotocol.io/vega/protos/vega/data/v1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeriodicSettlement(t *testing.T) {
	t.Run("incoming data ignored before leaving opening auction", testIncomingDataIgnoredBeforeLeavingOpeningAuction)
	t.Run("period end with no data point", testPeriodEndWithNoDataPoints)
	t.Run("equal internal and external prices", testEqualInternalAndExternalPrices)
	t.Run("constant difference long pays short", TestConstantDifferenceLongPaysShort)
	t.Run("data points outside of period", testDataPointsOutsidePeriod)
	t.Run("data points not on boundary", testDataPointsNotOnBoundary)
	t.Run("matching data points outside of period through callbacks", testRegisteredCallbacks)
	t.Run("non-matching data points outside of period through callbacks", testRegisteredCallbacksWithDifferentData)
	t.Run("funding payments with interest rate", testFundingPaymentsWithInterestRate)
	t.Run("funding payments with interest rate clamped", testFundingPaymentsWithInterestRateClamped)
	t.Run("terminate perps market test", testTerminateTrading)
	t.Run("margin increase", testGetMarginIncrease)
	t.Run("margin increase, negative payment", TestGetMarginIncreaseNegativePayment)
	t.Run("test pathological case with out of order points", testOutOfOrderPointsBeforePeriodStart)
	t.Run("test update perpetual", testUpdatePerpetual)
}

func TestExternalDataPointTWAPInSequence(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()

	ctx := context.Background()
	tstData, err := getGQLData()
	require.NoError(t, err)
	data := tstData.GetDataPoints()
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)

	// want to start the period from before the point with the smallest time
	seq := math.MaxInt
	st := data[0].t
	for i := 0; i < len(data); i++ {
		if data[i].t < st {
			st = data[i].t
		}
		seq = num.MinV(seq, data[i].seq)
	}
	// leave opening auction
	perp.perpetual.OnLeaveOpeningAuction(ctx, st-1)

	// set the first internal data-point
	// perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	// perp.perpetual.SubmitDataPoint(ctx, data[0].price.Clone(), data[0].t)
	for i, dp := range data {
		if dp.seq > seq {
			perp.broker.EXPECT().Send(gomock.Any()).Times(2)
			if dp.seq == 2 {
				perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
			}
			perp.perpetual.PromptSettlementCue(ctx, dp.t)
			seq = dp.seq
		}
		check := func(e events.Event) {
			de, ok := e.(*events.FundingPeriodDataPoint)
			require.True(t, ok)
			dep := de.Proto()
			if dep.Twap == "0" {
				return
			}
			require.Equal(t, dp.twap.String(), dep.Twap, fmt.Sprintf("IDX: %d\n%#v\n", i, dep))
		}
		perp.broker.EXPECT().Send(gomock.Any()).Times(1).Do(check)
		perp.perpetual.AddTestExternalPoint(ctx, dp.price, dp.t)
	}
}

func TestExternalDataPointTWAPOutSequence(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()

	ctx := context.Background()
	tstData, err := getGQLData()
	require.NoError(t, err)
	data := tstData.GetDataPoints()
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	// leave opening auction
	perp.perpetual.OnLeaveOpeningAuction(ctx, data[0].t-1)

	seq := data[0].seq
	last := 0
	for i := 0; i < len(data); i++ {
		if data[i].seq != seq {
			break
		}
		last = i
	}
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	// add the first (earliest) data-point first
	perp.perpetual.AddTestExternalPoint(ctx, data[0].price, data[0].t)
	// submit external data points in non-sequential order
	for j := last; j < 0; j-- {
		dp := data[j]
		if dp.seq > seq {
			// break
			perp.broker.EXPECT().Send(gomock.Any()).Times(2)
			perp.perpetual.PromptSettlementCue(ctx, dp.t)
		}
		check := func(e events.Event) {
			de, ok := e.(*events.FundingPeriodDataPoint)
			require.True(t, ok)
			dep := de.Proto()
			if dep.Twap == "0" {
				return
			}
			require.Equal(t, dp.twap.String(), dep.Twap, fmt.Sprintf("IDX: %d\n%#v\n", j, dep))
		}
		perp.broker.EXPECT().Send(gomock.Any()).Times(1).Do(check)
		perp.perpetual.AddTestExternalPoint(ctx, dp.price, dp.t)
	}
}

func testIncomingDataIgnoredBeforeLeavingOpeningAuction(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()

	ctx := context.Background()

	// no error because its really a callback from the oracle engine, but we expect no events
	perp.perpetual.AddTestExternalPoint(ctx, num.UintOne(), 2000)

	err := perp.perpetual.SubmitDataPoint(ctx, num.UintOne(), 2000)
	assert.ErrorIs(t, err, products.ErrInitialPeriodNotStarted)

	// check that settlement cues are ignored, we expect no events when it is
	perp.perpetual.PromptSettlementCue(ctx, 4000)
}

func testPeriodEndWithNoDataPoints(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()

	ctx := context.Background()

	// funding payment will be zero because there are no data points
	var called bool
	fn := func(context.Context, *num.Numeric) {
		called = true
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1000)

	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.PromptSettlementCue(ctx, 1040)

	// we had no points to check we didn't call into listener
	assert.False(t, called)
}

func testEqualInternalAndExternalPrices(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// set of the data points such that difference in averages is 0
	points := getTestDataPoints(t)

	// tell the perpetual that we are ready to accept settlement stuff
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// send in some data points
	perp.broker.EXPECT().Send(gomock.Any()).Times(len(points) * 2)
	for _, p := range points {
		// send in an external and a matching internal
		require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, p.price, p.t))
		perp.perpetual.AddTestExternalPoint(ctx, p.price, p.t)
	}

	// ask for the funding payment
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
	perp.perpetual.PromptSettlementCue(ctx, points[len(points)-1].t)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "0", fundingPayment.String())
}

func TestConstantDifferenceLongPaysShort(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)

	// when: the funding period starts at 1000
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1000)

	// and: the difference in external/internal prices are a constant -10
	submitDataWithDifference(t, perp, points, -10)

	// funding payment will be zero so no transfers
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)

	productData := perp.perpetual.GetData(points[len(points)-1].t)
	perpData, ok := productData.Data.(*types.PerpetualData)
	assert.True(t, ok)

	perp.perpetual.PromptSettlementCue(ctx, points[len(points)-1].t)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "-10", fundingPayment.String())
	assert.Equal(t, "-10", perpData.FundingPayment)
	assert.Equal(t, "116", perpData.ExternalTWAP)
	assert.Equal(t, "106", perpData.InternalTWAP)
	assert.Equal(t, "-0.0862068965517241", perpData.FundingRate)
}

func testDataPointsOutsidePeriod(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// set of the data points such that difference in averages is 0
	points := getTestDataPoints(t)

	// tell the perpetual that we are ready to accept settlement stuff
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// add data-points from the past, they will just be ignored
	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, num.UintOne(), points[0].t-int64(time.Hour)))
	perp.perpetual.AddTestExternalPoint(ctx, num.UintZero(), points[0].t-int64(time.Hour))

	// send in some data points
	perp.broker.EXPECT().Send(gomock.Any()).Times(len(points) * 2)
	for _, p := range points {
		// send in an external and a matching internal
		require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, p.price, p.t))
		perp.perpetual.AddTestExternalPoint(ctx, p.price, p.t)
	}

	// add some data-points in the future from when we will cue the end of the funding period
	// they should not affect the funding payment of this period
	lastPoint := points[len(points)-1]
	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, num.UintOne(), lastPoint.t+int64(time.Hour)))
	perp.perpetual.AddTestExternalPoint(ctx, num.UintZero(), lastPoint.t+int64(time.Hour))

	// ask for the funding payment
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	// 6 times because: end + start of the period, plus 2 carry over points for external + internal (total 4)
	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1).Do(func(evts []events.Event) {
		require.Equal(t, 4, len(evts)) // 4 carry over points
	})
	perp.perpetual.PromptSettlementCue(ctx, lastPoint.t)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "0", fundingPayment.String())
}

func testDataPointsNotOnBoundary(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// set of the data points such that difference in averages is 0
	points := getTestDataPoints(t)

	// start time is *after* our first data points
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1005)

	// send in some data points
	submitDataWithDifference(t, perp, points, 10)

	// ask for the funding payment
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	// period end is *after* our last point
	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
	perp.perpetual.PromptSettlementCue(ctx, points[len(points)-1].t+int64(time.Hour))
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "10", fundingPayment.String())
}

func testOutOfOrderPointsBeforePeriodStart(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// start time will be after the *second* data point
	perp.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	perp.broker.EXPECT().SendBatch(gomock.Any()).AnyTimes()
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1693398617000000000)

	price := num.NewUint(100000000)
	timestamps := []int64{
		1693398614000000000,
		1693398615000000000,
		1693398616000000000,
		1693398618000000000,
		1693398617000000000,
	}

	for _, tt := range timestamps {
		perp.perpetual.AddTestExternalPoint(ctx, price, tt)
		perp.perpetual.SubmitDataPoint(ctx, num.UintZero().Add(price, num.NewUint(100000000)), tt)
	}

	// ask for the funding payment
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	// period end is *after* our last point
	perp.perpetual.PromptSettlementCue(ctx, 1693398617000000000+int64(time.Hour))
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "100000000", fundingPayment.String())
}

func testRegisteredCallbacks(t *testing.T) {
	log := logging.NewTestLogger()
	ctrl := gomock.NewController(t)
	oe := mocks.NewMockOracleEngine(ctrl)
	broker := mocks.NewMockBroker(ctrl)
	exp := &num.Numeric{}
	exp.SetUint(num.UintZero())
	ctx := context.Background()
	received := false
	points := getTestDataPoints(t)
	marketSettle := func(_ context.Context, data *num.Numeric) {
		received = true
		require.Equal(t, exp.String(), data.String())
	}
	var settle, period spec.OnMatchedData
	oe.EXPECT().Subscribe(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(func(_ context.Context, s spec.Spec, cb spec.OnMatchedData) (spec.SubscriptionID, spec.Unsubscriber, error) {
		filters := s.OriginalSpec.GetDefinition().DataSourceType.GetFilters()
		for _, f := range filters {
			if f.Key.Type == datapb.PropertyKey_TYPE_INTEGER || f.Key.Type == datapb.PropertyKey_TYPE_DECIMAL {
				settle = cb
				return spec.SubscriptionID(1), func(_ context.Context, _ spec.SubscriptionID) {}, nil
			}
		}
		period = cb
		return spec.SubscriptionID(1), func(_ context.Context, _ spec.SubscriptionID) {}, nil
	})
	broker.EXPECT().Send(gomock.Any()).AnyTimes()
	broker.EXPECT().SendBatch(gomock.Any()).AnyTimes()
	perp := getTestPerpProd(t)
	perpetual, err := products.NewPerpetual(context.Background(), log, perp, "", oe, broker, 1)
	require.NoError(t, err)
	require.NotNil(t, settle)
	require.NotNil(t, period)
	// register the callback
	perpetual.NotifyOnSettlementData(marketSettle)

	perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	for _, p := range points {
		// send in an external and a matching internal
		require.NoError(t, perpetual.SubmitDataPoint(ctx, p.price, p.t))
		settle(ctx, dscommon.Data{
			Data: map[string]string{
				perp.DataSourceSpecBinding.SettlementDataProperty: p.price.String(),
			},
			MetaData: map[string]string{
				"eth-block-time": fmt.Sprintf("%d", time.Unix(0, p.t).Unix()),
			},
		})
	}
	// add some data-points in the future from when we will cue the end of the funding period
	// they should not affect the funding payment of this period
	lastPoint := points[len(points)-1]
	require.NoError(t, perpetual.SubmitDataPoint(ctx, num.UintOne(), lastPoint.t+int64(time.Hour)))
	settle(ctx, dscommon.Data{
		Data: map[string]string{
			perp.DataSourceSpecBinding.SettlementDataProperty: "1",
		},
		MetaData: map[string]string{
			"eth-block-time": fmt.Sprintf("%d", time.Unix(0, lastPoint.t+int64(time.Hour)).Unix()),
		},
	})
	// make sure the data-point outside of the period doesn't trigger the schedule callback
	// that has to come from the oracle, too
	assert.False(t, received)

	// end period
	period(ctx, dscommon.Data{
		Data: map[string]string{
			perp.DataSourceSpecBinding.SettlementScheduleProperty: fmt.Sprintf("%d", time.Unix(0, lastPoint.t).Unix()),
		},
	})

	assert.True(t, received)
}

func testRegisteredCallbacksWithDifferentData(t *testing.T) {
	log := logging.NewTestLogger()
	ctrl := gomock.NewController(t)
	oe := mocks.NewMockOracleEngine(ctrl)
	broker := mocks.NewMockBroker(ctrl)
	exp := &num.Numeric{}
	// should be 2
	res, _ := num.IntFromString("-4", 10)
	exp.SetInt(res)
	ctx := context.Background()
	received := false
	points := getTestDataPoints(t)
	marketSettle := func(_ context.Context, data *num.Numeric) {
		received = true
		require.Equal(t, exp.String(), data.String())
	}
	var settle, period spec.OnMatchedData
	oe.EXPECT().Subscribe(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(func(_ context.Context, s spec.Spec, cb spec.OnMatchedData) (spec.SubscriptionID, spec.Unsubscriber, error) {
		filters := s.OriginalSpec.GetDefinition().DataSourceType.GetFilters()
		for _, f := range filters {
			if f.Key.Type == datapb.PropertyKey_TYPE_INTEGER || f.Key.Type == datapb.PropertyKey_TYPE_DECIMAL {
				settle = cb
				return spec.SubscriptionID(1), func(_ context.Context, _ spec.SubscriptionID) {}, nil
			}
		}
		period = cb
		return spec.SubscriptionID(1), func(_ context.Context, _ spec.SubscriptionID) {}, nil
	})
	broker.EXPECT().Send(gomock.Any()).AnyTimes()
	broker.EXPECT().SendBatch(gomock.Any()).AnyTimes()
	perp := getTestPerpProd(t)
	perpetual, err := products.NewPerpetual(context.Background(), log, perp, "", oe, broker, 1)
	require.NoError(t, err)
	require.NotNil(t, settle)
	require.NotNil(t, period)
	// register the callback
	perpetual.NotifyOnSettlementData(marketSettle)

	// start the funding period
	perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// send data in from before the start of the period, it should not affect the result
	require.NoError(t, perpetual.SubmitDataPoint(ctx, num.UintOne(), points[0].t-int64(time.Hour)))
	// callback to receive settlement data
	settle(ctx, dscommon.Data{
		Data: map[string]string{
			perp.DataSourceSpecBinding.SettlementDataProperty: "1",
		},
		MetaData: map[string]string{
			"eth-block-time": fmt.Sprintf("%d", time.Unix(0, points[0].t-int64(time.Hour)).Unix()),
		},
	})

	// send all external points, but not all internal ones and have their price
	// be one less. This means external twap > internal tswap so we expect a negative funding rate
	for i, p := range points {
		if i%2 == 0 {
			ip := num.UintZero().Sub(p.price, num.UintOne())
			require.NoError(t, perpetual.SubmitDataPoint(ctx, ip, p.t))
		}
		settle(ctx, dscommon.Data{
			Data: map[string]string{
				perp.DataSourceSpecBinding.SettlementDataProperty: p.price.String(),
			},
			MetaData: map[string]string{
				"eth-block-time": fmt.Sprintf("%d", time.Unix(0, p.t).Unix()),
			},
		})
	}

	// add some data-points in the future from when we will cue the end of the funding period
	// they should not affect the funding payment of this period
	lastPoint := points[len(points)-1]
	require.NoError(t, perpetual.SubmitDataPoint(ctx, num.UintOne(), lastPoint.t+int64(time.Hour)))
	settle(ctx, dscommon.Data{
		Data: map[string]string{
			perp.DataSourceSpecBinding.SettlementDataProperty: "1",
		},
		MetaData: map[string]string{
			"eth-block-time": fmt.Sprintf("%d", time.Unix(0, lastPoint.t+int64(time.Hour)).Unix()),
		},
	})

	// end period
	period(ctx, dscommon.Data{
		Data: map[string]string{
			perp.DataSourceSpecBinding.SettlementScheduleProperty: fmt.Sprintf("%d", time.Unix(0, lastPoint.t).Unix()),
		},
	})

	assert.True(t, received)
}

func testFundingPaymentsWithInterestRate(t *testing.T) {
	perp := testPerpetualWithOpts(t, "0.01", "-1", "1", "0")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)
	lastPoint := points[len(points)-1]

	// when: the funding period starts
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// scale the price so that we have more precision to work with
	scale := num.UintFromUint64(100000000000)
	for _, p := range points {
		p.price = num.UintZero().Mul(p.price, scale)
	}

	// and: the difference in external/internal prices are a constant -10
	submitDataWithDifference(t, perp, points, -1000000000000)

	// Whats happening:
	// the fundingPayment without the interest terms will be -10000000000000
	//
	// interest will be (1 + r * t) * swap - fswap
	// where r = 0.01, t = 0.25, stwap = 11666666666666, ftwap = 10666666666656,
	// interest = (1 + 0.0025) * 11666666666666 - 10666666666656 = 1029166666666

	// since lower clamp <    interest   < upper clamp
	//   -11666666666666 < 1029166666666 < 11666666666666
	// there is no adjustment and so
	// funding payment = -10000000000000 + 1029166666666 = 29166666666

	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
	perp.perpetual.PromptSettlementCue(ctx, lastPoint.t)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "29166666666", fundingPayment.String())
}

func testFundingPaymentsWithInterestRateClamped(t *testing.T) {
	perp := testPerpetualWithOpts(t, "0.5", "0.001", "0.002", "0")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)

	// when: the funding period starts
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// scale the price so that we have more precision to work with
	scale := num.UintFromUint64(100000000000)
	for _, p := range points {
		p.price = num.UintZero().Mul(p.price, scale)
	}

	// and: the difference in external/internal prices are a constant -10
	submitDataWithDifference(t, perp, points, -10)

	// Whats happening:
	// the fundingPayment will be -10 without the interest terms
	//
	// interest will be (1 + r * t) * swap - fswap
	// where stwap=116, ftwap=106, r=0.5 t=0.25
	// interest = (1 + 0.125) * 11666666666666 - 11666666666656 = 1458333333343

	// if we consider the clamps:
	// lower clamp:   11666666666
	// interest:    1458333333343
	// upper clamp:   23333333333

	// so we have exceeded the upper clamp the the interest term is snapped to it and so:
	// funding payment = -10 + 23333333333 = 23333333323

	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
	perp.perpetual.PromptSettlementCue(ctx, points[3].t)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "23333333323", fundingPayment.String())
}

func testTerminateTrading(t *testing.T) {
	perp := testPerpetual(t)
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// set of the data points such that difference in averages is 0
	points := getTestDataPoints(t)

	// tell the perpetual that we are ready to accept settlement stuff
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, points[0].t)

	// send in some data points
	perp.broker.EXPECT().Send(gomock.Any()).Times(len(points) * 2)
	for _, p := range points {
		// send in an external and a matching internal
		require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, p.price, p.t))
		perp.perpetual.AddTestExternalPoint(ctx, p.price, p.t)
	}

	// ask for the funding payment
	var fundingPayment *num.Numeric
	fn := func(_ context.Context, fp *num.Numeric) {
		fundingPayment = fp
	}
	perp.perpetual.SetSettlementListener(fn)

	perp.broker.EXPECT().Send(gomock.Any()).Times(2)
	perp.broker.EXPECT().SendBatch(gomock.Any()).Times(1)
	perp.perpetual.UnsubscribeTradingTerminated(ctx)
	assert.NotNil(t, fundingPayment)
	assert.True(t, fundingPayment.IsInt())
	assert.Equal(t, "0", fundingPayment.String())
}

func testGetMarginIncrease(t *testing.T) {
	// margin factor is 0.5
	perp := testPerpetualWithOpts(t, "0", "0", "0", "0.5")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)

	// before we've started the first funding interval margin increase is 0
	inc := perp.perpetual.GetMarginIncrease(points[0].t)
	assert.Equal(t, "0", inc.String())

	// start funding period
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1000)

	// started interval, but not points, margin increase is 0
	inc = perp.perpetual.GetMarginIncrease(points[0].t)
	assert.Equal(t, "0", inc.String())

	// and: the difference in external/internal prices are is 10
	submitDataWithDifference(t, perp, points, 10)

	lastPoint := points[len(points)-1]
	inc = perp.perpetual.GetMarginIncrease(lastPoint.t)
	// margin increase is margin_factor * funding-payment = 0.5 * 10
	assert.Equal(t, "5", inc.String())
}

func TestGetMarginIncreaseNegativePayment(t *testing.T) {
	// margin factor is 0.5
	perp := testPerpetualWithOpts(t, "0", "0", "0", "0.5")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)

	// start funding period
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1000)

	// and: the difference in external/internal prices are is 10
	submitDataWithDifference(t, perp, points, -10)

	lastPoint := points[len(points)-1]
	inc := perp.perpetual.GetMarginIncrease(lastPoint.t)
	// margin increase is margin_factor * funding-payment = 0.5 * 10
	assert.Equal(t, "-5", inc.String())
}

func testUpdatePerpetual(t *testing.T) {
	// margin factor is 0.5
	perp := testPerpetualWithOpts(t, "0", "0", "0", "0.5")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	// test data
	points := getTestDataPoints(t)
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1000)
	submitDataWithDifference(t, perp, points, 10)

	// query margin factor before update
	lastPoint := points[len(points)-1]
	inc := perp.perpetual.GetMarginIncrease(lastPoint.t)
	assert.Equal(t, "5", inc.String())

	// do the perps update with a new margin factor
	update := getTestPerpProd(t)
	update.MarginFundingFactor = num.DecimalFromFloat(1)
	err := perp.perpetual.Update(ctx, &types.InstrumentPerps{Perps: update}, perp.oe)
	require.NoError(t, err)

	// expect two unsubscriptions
	assert.Equal(t, perp.unsub, 2)

	// margin increase should now be double, which means the data-points were preserved
	inc = perp.perpetual.GetMarginIncrease(lastPoint.t)
	assert.Equal(t, "10", inc.String())

	// now submit a data point and check it is expected i.e the funding period is still active
	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	assert.NoError(t, perp.perpetual.SubmitDataPoint(ctx, num.NewUint(123), lastPoint.t+int64(time.Hour)))
}

func TestRealLifeData(t *testing.T) {
	// margin factor is 0.5
	perp := testPerpetualWithOpts(t, "0", "0", "0", "0.5")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1695710684000000000)

	// now submit a data point and check it is expected i.e the funding period is still active
	// perp.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// perp.broker.EXPECT().SendBatch(gomock.Any()).AnyTimes()

	// regardless of the order the points are submitted we should get the same TWAP, so we randomly shuffle the order
	points := fairgroundTestData[:]
	for i := range points {
		j := rand.Intn(i + 1)
		points[i], points[j] = points[j], points[i]
	}

	// we need external/internal data so we submit the same set with a difference
	submitDataWithDifference(t, perp, points, 1000)
	productData := perp.perpetual.GetData(1695712484000000000)
	perpData := productData.Data.(*types.PerpetualData)
	assert.Equal(t, "26274800006", perpData.InternalTWAP)
	assert.Equal(t, "26274799006", perpData.ExternalTWAP)
}

// submits the given data points as both external and interval but with the given different added to the internal price.
func submitDataWithDifference(t *testing.T, perp *tstPerp, points []*testDataPoint, diff int) {
	t.Helper()
	ctx := context.Background()

	var internalPrice *num.Uint
	perp.broker.EXPECT().Send(gomock.Any()).Times(len(points) * 2)
	for _, p := range points {
		perp.perpetual.AddTestExternalPoint(ctx, p.price, p.t)

		if diff > 0 {
			internalPrice = num.UintZero().Add(p.price, num.NewUint(uint64(diff)))
		}
		if diff < 0 {
			internalPrice = num.UintZero().Sub(p.price, num.NewUint(uint64(-diff)))
		}
		require.NoError(t, perp.perpetual.SubmitDataPoint(ctx, internalPrice, p.t))
	}
}

type testDataPoint struct {
	price *num.Uint
	t     int64
}

func getTestDataPoints(t *testing.T) []*testDataPoint {
	t.Helper()

	// interest-rates are daily so we want the time of the data-points to be of that scale
	// so we make them over 6 hours, a quarter of a day.

	year := 31536000000000000
	month := int64(year / 12)
	st := int64(time.Hour)

	return []*testDataPoint{
		{
			price: num.NewUint(110),
			t:     st,
		},
		{
			price: num.NewUint(120),
			t:     st + month,
		},
		{
			price: num.NewUint(120),
			t:     st + (month * 2),
		},
		{
			price: num.NewUint(100),
			t:     st + (month * 3),
		},
	}
}

type tstPerp struct {
	oe        *mocks.MockOracleEngine
	broker    *mocks.MockBroker
	perpetual *products.Perpetual
	ctrl      *gomock.Controller
	perp      *types.Perps

	unsub int
}

func (tp *tstPerp) unsubscribe(_ context.Context, _ spec.SubscriptionID) {
	tp.unsub++
}

func testPerpetual(t *testing.T) *tstPerp {
	t.Helper()
	return testPerpetualWithOpts(t, "0", "0", "0", "0")
}

func testPerpetualWithOpts(t *testing.T, interestRate, clampLowerBound, clampUpperBound, marginFactor string) *tstPerp {
	t.Helper()

	log := logging.NewTestLogger()
	ctrl := gomock.NewController(t)
	oe := mocks.NewMockOracleEngine(ctrl)
	broker := mocks.NewMockBroker(ctrl)
	perp := getTestPerpProd(t)

	tp := &tstPerp{
		oe:     oe,
		broker: broker,
		ctrl:   ctrl,
		perp:   perp,
	}
	tp.oe.EXPECT().Subscribe(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(spec.SubscriptionID(1), tp.unsubscribe, nil)

	perpetual, err := products.NewPerpetual(context.Background(), log, perp, "", oe, broker, 1)
	perp.InterestRate = num.MustDecimalFromString(interestRate)
	perp.ClampLowerBound = num.MustDecimalFromString(clampLowerBound)
	perp.ClampUpperBound = num.MustDecimalFromString(clampUpperBound)
	perp.MarginFundingFactor = num.MustDecimalFromString(marginFactor)
	if err != nil {
		t.Fatalf("couldn't create a perp for testing: %v", err)
	}

	tp.perpetual = perpetual
	return tp
}

func getTestPerpProd(t *testing.T) *types.Perps {
	t.Helper()
	dp := uint32(1)
	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}

	factor, _ := num.DecimalFromString("0.5")
	settlementSrc := &datasource.Spec{
		Data: datasource.NewDefinition(
			datasource.ContentTypeOracle,
		).SetOracleConfig(
			&signedoracle.SpecConfiguration{
				Signers: pubKeys,
				Filters: []*dstypes.SpecFilter{
					{
						Key: &dstypes.SpecPropertyKey{
							Name:                "foo",
							Type:                datapb.PropertyKey_TYPE_INTEGER,
							NumberDecimalPlaces: ptr.From(uint64(dp)),
						},
						Conditions: nil,
					},
				},
			},
		),
	}

	scheduleSrc := &datasource.Spec{
		Data: datasource.NewDefinition(
			datasource.ContentTypeOracle,
		).SetOracleConfig(&signedoracle.SpecConfiguration{
			Signers: pubKeys,
			Filters: []*dstypes.SpecFilter{
				{
					Key: &dstypes.SpecPropertyKey{
						Name: "bar",
						Type: datapb.PropertyKey_TYPE_TIMESTAMP,
					},
					Conditions: nil,
				},
			},
		}),
	}

	return &types.Perps{
		MarginFundingFactor:                 factor,
		DataSourceSpecForSettlementData:     settlementSrc,
		DataSourceSpecForSettlementSchedule: scheduleSrc,
		DataSourceSpecBinding: &datasource.SpecBindingForPerps{
			SettlementDataProperty:     "foo",
			SettlementScheduleProperty: "bar",
		},
	}
}

type DataPoint struct {
	price *num.Uint
	t     int64
	seq   int
	twap  *num.Uint
}

type FundingNode struct {
	Timestamp time.Time `json:"timestamp"`
	Seq       int       `json:"seq"`
	Price     string    `json:"price"`
	TWAP      string    `json:"twap"`
	Source    string    `json:"dataPointSource"`
}

type Edge struct {
	Node FundingNode `json:"node"`
}

type FundingDataPoints struct {
	Edges []Edge `json:"edges"`
}

type GQLData struct {
	FundingDataPoints FundingDataPoints `json:"fundingPeriodDataPoints"`
}

type GQL struct {
	Data GQLData `json:"data"`
}

// data-points from a funding period during a fairground incentive for build v0.73.0.preview-7
// funding period start: 1695710684000000000 end: 1695712484000000000
var fairgroundTestData = []*testDataPoint{
	{price: num.NewUint(26274100000), t: 1695712483988556000},
	{price: num.NewUint(26280800000), t: 1695712478711589000},
	{price: num.NewUint(26274100000), t: 1695712473437357000},
	{price: num.NewUint(26274100000), t: 1695712468045834000},
	{price: num.NewUint(26280800000), t: 1695712462177957000},
	{price: num.NewUint(26280200000), t: 1695712412013435000},
	{price: num.NewUint(26270500000), t: 1695712406197952000},
	{price: num.NewUint(26280200000), t: 1695712400827544000},
	{price: num.NewUint(26270500000), t: 1695712394177142000},
	{price: num.NewUint(26279200000), t: 1695712388408480000},
	{price: num.NewUint(26270500000), t: 1695712377487025000},
	{price: num.NewUint(26279200000), t: 1695712371666697000},
	{price: num.NewUint(26270500000), t: 1695712366268181000},
	{price: num.NewUint(26279200000), t: 1695712354387618000},
	{price: num.NewUint(26270500000), t: 1695712349310601000},
	{price: num.NewUint(26279200000), t: 1695712343695950000},
	{price: num.NewUint(26279200000), t: 1695712333157641000},
	{price: num.NewUint(26279200000), t: 1695712321282868000},
	{price: num.NewUint(26270500000), t: 1695712303475880000},
	{price: num.NewUint(26279200000), t: 1695712297842002000},
	{price: num.NewUint(26271500000), t: 1695712292159528000},
	{price: num.NewUint(26279200000), t: 1695712286343633000},
	{price: num.NewUint(26278200000), t: 1695712280789352000},
	{price: num.NewUint(26278200000), t: 1695712275419698000},
	{price: num.NewUint(26271500000), t: 1695712269898875000},
	{price: num.NewUint(26278200000), t: 1695712263473403000},
	{price: num.NewUint(26270500000), t: 1695712245927072000},
	{price: num.NewUint(26270500000), t: 1695712240079604000},
	{price: num.NewUint(26271500000), t: 1695712234208778000},
	{price: num.NewUint(26271500000), t: 1695712223375650000},
	{price: num.NewUint(26270500000), t: 1695712188130632000},
	{price: num.NewUint(26278200000), t: 1695712175872871000},
	{price: num.NewUint(26278200000), t: 1695712164689346000},
	{price: num.NewUint(26270500000), t: 1695712159614377000},
	{price: num.NewUint(26270500000), t: 1695712148355352000},
	{price: num.NewUint(26279200000), t: 1695712142562210000},
	{price: num.NewUint(26271500000), t: 1695712135982323000},
	{price: num.NewUint(26279200000), t: 1695712095503371000},
	{price: num.NewUint(26278200000), t: 1695712073330657000},
	{price: num.NewUint(26271500000), t: 1695712056847928000},
	{price: num.NewUint(26279200000), t: 1695712050115063000},
	{price: num.NewUint(26271500000), t: 1695712043957382000},
	{price: num.NewUint(26279200000), t: 1695712033812045000},
	{price: num.NewUint(26271500000), t: 1695712022733947000},
	{price: num.NewUint(26271500000), t: 1695712017206005000},
	{price: num.NewUint(26278200000), t: 1695712006340503000},
	{price: num.NewUint(26271500000), t: 1695711989404972000},
	{price: num.NewUint(26278200000), t: 1695711984385553000},
	{price: num.NewUint(26271500000), t: 1695711968504402000},
	{price: num.NewUint(26279200000), t: 1695711961823866000},
	{price: num.NewUint(26271500000), t: 1695711956518579000},
	{price: num.NewUint(26278200000), t: 1695711951268333000},
	{price: num.NewUint(26271500000), t: 1695711946228906000},
	{price: num.NewUint(26278200000), t: 1695711935261745000},
	{price: num.NewUint(26271500000), t: 1695711929785170000},
	{price: num.NewUint(26271500000), t: 1695711924273156000},
	{price: num.NewUint(26278200000), t: 1695711919224365000},
	{price: num.NewUint(26271500000), t: 1695711913736125000},
	{price: num.NewUint(26278200000), t: 1695711901853197000},
	{price: num.NewUint(26271500000), t: 1695711896812022000},
	{price: num.NewUint(26271500000), t: 1695711891424377000},
	{price: num.NewUint(26271500000), t: 1695711886000070000},
	{price: num.NewUint(26278200000), t: 1695711880743359000},
	{price: num.NewUint(26271500000), t: 1695711875458003000},
	{price: num.NewUint(26278200000), t: 1695711869501814000},
	{price: num.NewUint(26271500000), t: 1695711864129401000},
	{price: num.NewUint(26278200000), t: 1695711858725056000},
	{price: num.NewUint(26271500000), t: 1695711853238422000},
	{price: num.NewUint(26279200000), t: 1695711847961126000},
	{price: num.NewUint(26283200000), t: 1695711842499723000},
	{price: num.NewUint(26268500000), t: 1695711837229288000},
	{price: num.NewUint(26282200000), t: 1695711832144228000},
	{price: num.NewUint(26269500000), t: 1695711827006009000},
	{price: num.NewUint(26270500000), t: 1695711821964355000},
	{price: num.NewUint(26279200000), t: 1695711816858753000},
	{price: num.NewUint(26271500000), t: 1695711811673148000},
	{price: num.NewUint(26270500000), t: 1695711806413596000},
	{price: num.NewUint(26278200000), t: 1695711801345441000},
	{price: num.NewUint(26281200000), t: 1695711795947409000},
	{price: num.NewUint(26270500000), t: 1695711790590730000},
	{price: num.NewUint(26271500000), t: 1695711785476724000},
	{price: num.NewUint(26268500000), t: 1695711779880852000},
	{price: num.NewUint(26269500000), t: 1695711774796680000},
	{price: num.NewUint(26278200000), t: 1695711769632513000},
	{price: num.NewUint(26268500000), t: 1695711759118263000},
	{price: num.NewUint(26282200000), t: 1695711753806517000},
	{price: num.NewUint(26268500000), t: 1695711748555825000},
	{price: num.NewUint(26280200000), t: 1695711743194300000},
	{price: num.NewUint(26279200000), t: 1695711737891687000},
	{price: num.NewUint(26270500000), t: 1695711732343518000},
	{price: num.NewUint(26270500000), t: 1695711727170987000},
	{price: num.NewUint(26279200000), t: 1695711721810047000},
	{price: num.NewUint(26278200000), t: 1695711716472074000},
	{price: num.NewUint(26278200000), t: 1695711711220295000},
	{price: num.NewUint(26269500000), t: 1695711706123260000},
	{price: num.NewUint(26271500000), t: 1695711700886504000},
	{price: num.NewUint(26269500000), t: 1695711695534426000},
	{price: num.NewUint(26270500000), t: 1695711690480095000},
	{price: num.NewUint(26279200000), t: 1695711685091433000},
	{price: num.NewUint(26278200000), t: 1695711679829765000},
	{price: num.NewUint(26281200000), t: 1695711674195010000},
	{price: num.NewUint(26268500000), t: 1695711669026613000},
	{price: num.NewUint(26268500000), t: 1695711663775700000},
	{price: num.NewUint(26270500000), t: 1695711658364642000},
	{price: num.NewUint(26270500000), t: 1695711653146260000},
	{price: num.NewUint(26278200000), t: 1695711647456440000},
	{price: num.NewUint(26269500000), t: 1695711642017158000},
	{price: num.NewUint(26280200000), t: 1695711636648031000},
	{price: num.NewUint(26278200000), t: 1695711631518158000},
	{price: num.NewUint(26271500000), t: 1695711625692800000},
	{price: num.NewUint(26280200000), t: 1695711620172878000},
	{price: num.NewUint(26280200000), t: 1695711614857950000},
	{price: num.NewUint(26270500000), t: 1695711609255838000},
	{price: num.NewUint(26271500000), t: 1695711603605091000},
	{price: num.NewUint(26271500000), t: 1695711597883464000},
	{price: num.NewUint(26280200000), t: 1695711592771353000},
	{price: num.NewUint(26270500000), t: 1695711587027686000},
	{price: num.NewUint(26268500000), t: 1695711581661385000},
	{price: num.NewUint(26282200000), t: 1695711576141119000},
	{price: num.NewUint(26281200000), t: 1695711570553562000},
	{price: num.NewUint(26269500000), t: 1695711565206909000},
	{price: num.NewUint(26279200000), t: 1695711559984629000},
	{price: num.NewUint(26271500000), t: 1695711554487792000},
	{price: num.NewUint(26268900000), t: 1695711549020727000},
	{price: num.NewUint(26268500000), t: 1695711543629306000},
	{price: num.NewUint(26280200000), t: 1695711538269819000},
	{price: num.NewUint(26270500000), t: 1695711533139625000},
	{price: num.NewUint(26268500000), t: 1695711527862792000},
	{price: num.NewUint(26282200000), t: 1695711522653865000},
	{price: num.NewUint(26281200000), t: 1695711517382999000},
	{price: num.NewUint(26279200000), t: 1695711511995711000},
	{price: num.NewUint(26278200000), t: 1695711506909359000},
	{price: num.NewUint(26279200000), t: 1695711501843214000},
	{price: num.NewUint(26278200000), t: 1695711496498798000},
	{price: num.NewUint(26280200000), t: 1695711491040968000},
	{price: num.NewUint(26270500000), t: 1695711485847186000},
	{price: num.NewUint(26271500000), t: 1695711480447131000},
	{price: num.NewUint(26278200000), t: 1695711475442605000},
	{price: num.NewUint(26283200000), t: 1695711470069625000},
	{price: num.NewUint(26268500000), t: 1695711464978794000},
	{price: num.NewUint(26269500000), t: 1695711459951565000},
	{price: num.NewUint(26269500000), t: 1695711454452735000},
	{price: num.NewUint(26270500000), t: 1695711448685194000},
	{price: num.NewUint(26279200000), t: 1695711443399557000},
	{price: num.NewUint(26271500000), t: 1695711438354414000},
	{price: num.NewUint(26269500000), t: 1695711432588909000},
	{price: num.NewUint(26270000000), t: 1695711427492188000},
	{price: num.NewUint(26279200000), t: 1695711422346311000},
	{price: num.NewUint(26278200000), t: 1695711417194422000},
	{price: num.NewUint(26270500000), t: 1695711411836401000},
	{price: num.NewUint(26282200000), t: 1695711406262612000},
	{price: num.NewUint(26282200000), t: 1695711401009882000},
	{price: num.NewUint(26280200000), t: 1695711395830417000},
	{price: num.NewUint(26280200000), t: 1695711390647109000},
	{price: num.NewUint(26270000000), t: 1695711385537051000},
	{price: num.NewUint(26271500000), t: 1695711380469202000},
	{price: num.NewUint(26280200000), t: 1695711374797746000},
	{price: num.NewUint(26279200000), t: 1695711369419438000},
	{price: num.NewUint(26278200000), t: 1695711363962475000},
	{price: num.NewUint(26281200000), t: 1695711358501685000},
	{price: num.NewUint(26270000000), t: 1695711353272262000},
	{price: num.NewUint(26280200000), t: 1695711348036576000},
	{price: num.NewUint(26271500000), t: 1695711342956549000},
	{price: num.NewUint(26279200000), t: 1695711337649101000},
	{price: num.NewUint(26271500000), t: 1695711332215005000},
	{price: num.NewUint(26270000000), t: 1695711327213541000},
	{price: num.NewUint(26279200000), t: 1695711321904173000},
	{price: num.NewUint(26271500000), t: 1695711316411230000},
	{price: num.NewUint(26270500000), t: 1695711310819957000},
	{price: num.NewUint(26280200000), t: 1695711305574014000},
	{price: num.NewUint(26280200000), t: 1695711300358683000},
	{price: num.NewUint(26279200000), t: 1695711295008246000},
	{price: num.NewUint(26270000000), t: 1695711289807801000},
	{price: num.NewUint(26270000000), t: 1695711284653516000},
	{price: num.NewUint(26271500000), t: 1695711279501202000},
	{price: num.NewUint(26268500000), t: 1695711269095938000},
	{price: num.NewUint(26280200000), t: 1695711264067687000},
	{price: num.NewUint(26271500000), t: 1695711258858129000},
	{price: num.NewUint(26269500000), t: 1695711253842229000},
	{price: num.NewUint(26269500000), t: 1695711248657779000},
	{price: num.NewUint(26269500000), t: 1695711242909668000},
	{price: num.NewUint(26270500000), t: 1695711237310804000},
	{price: num.NewUint(26279200000), t: 1695711231709865000},
	{price: num.NewUint(26279200000), t: 1695711226555922000},
	{price: num.NewUint(26279200000), t: 1695711221109244000},
	{price: num.NewUint(26269500000), t: 1695711215897580000},
	{price: num.NewUint(26269500000), t: 1695711210421524000},
	{price: num.NewUint(26280200000), t: 1695711205217273000},
	{price: num.NewUint(26271500000), t: 1695711199994668000},
	{price: num.NewUint(26264900000), t: 1695711194374408000},
	{price: num.NewUint(26264900000), t: 1695711189305886000},
	{price: num.NewUint(26280200000), t: 1695711183532722000},
	{price: num.NewUint(26269500000), t: 1695711178489283000},
	{price: num.NewUint(26278200000), t: 1695711172754490000},
	{price: num.NewUint(26271500000), t: 1695711167639593000},
	{price: num.NewUint(26268500000), t: 1695711162183684000},
	{price: num.NewUint(26281200000), t: 1695711157088087000},
	{price: num.NewUint(26270500000), t: 1695711152027342000},
	{price: num.NewUint(26271500000), t: 1695711146948276000},
	{price: num.NewUint(26268500000), t: 1695711141834146000},
	{price: num.NewUint(26268500000), t: 1695711136551167000},
	{price: num.NewUint(26279200000), t: 1695711131217461000},
	{price: num.NewUint(26278200000), t: 1695711126055500000},
	{price: num.NewUint(26271500000), t: 1695711120759312000},
	{price: num.NewUint(26279000000), t: 1695711115312693000},
	{price: num.NewUint(26269500000), t: 1695711109980732000},
	{price: num.NewUint(26281200000), t: 1695711104944890000},
	{price: num.NewUint(26280200000), t: 1695711099787364000},
	{price: num.NewUint(26279200000), t: 1695711094407785000},
	{price: num.NewUint(26270500000), t: 1695711089212303000},
	{price: num.NewUint(26265500000), t: 1695711084076397000},
	{price: num.NewUint(26279200000), t: 1695711078909404000},
	{price: num.NewUint(26271500000), t: 1695711073222635000},
	{price: num.NewUint(26271500000), t: 1695711067828052000},
	{price: num.NewUint(26271500000), t: 1695711062655760000},
	{price: num.NewUint(26267500000), t: 1695711057407225000},
	{price: num.NewUint(26281200000), t: 1695711052255836000},
	{price: num.NewUint(26269500000), t: 1695711047071627000},
	{price: num.NewUint(26270500000), t: 1695711041785910000},
	{price: num.NewUint(26279200000), t: 1695711036593393000},
	{price: num.NewUint(26278500000), t: 1695711031161880000},
	{price: num.NewUint(26278500000), t: 1695711025984720000},
	{price: num.NewUint(26279200000), t: 1695711020713280000},
	{price: num.NewUint(26278500000), t: 1695711015545532000},
	{price: num.NewUint(26279500000), t: 1695711009986472000},
	{price: num.NewUint(26279500000), t: 1695711004637326000},
	{price: num.NewUint(26280500000), t: 1695710999376525000},
	{price: num.NewUint(26278500000), t: 1695710994254356000},
	{price: num.NewUint(26289100000), t: 1695710988616405000},
	{price: num.NewUint(26280500000), t: 1695710983350593000},
	{price: num.NewUint(26281500000), t: 1695710978054918000},
	{price: num.NewUint(26280500000), t: 1695710972871553000},
	{price: num.NewUint(26289100000), t: 1695710967463711000},
	{price: num.NewUint(26281500000), t: 1695710961665622000},
	{price: num.NewUint(26279500000), t: 1695710956450322000},
	{price: num.NewUint(26290100000), t: 1695710951016352000},
	{price: num.NewUint(26276500000), t: 1695710945260157000},
	{price: num.NewUint(26278500000), t: 1695710939687792000},
	{price: num.NewUint(26290100000), t: 1695710934418659000},
	{price: num.NewUint(26279500000), t: 1695710929255532000},
	{price: num.NewUint(26288100000), t: 1695710923688315000},
	{price: num.NewUint(26266900000), t: 1695710918447960000},
	{price: num.NewUint(26275500000), t: 1695710913242389000},
	{price: num.NewUint(26275500000), t: 1695710907870997000},
	{price: num.NewUint(26264900000), t: 1695710902690206000},
	{price: num.NewUint(26279500000), t: 1695710897428120000},
	{price: num.NewUint(26267900000), t: 1695710891735156000},
	{price: num.NewUint(26267900000), t: 1695710886522175000},
	{price: num.NewUint(26276500000), t: 1695710881200321000},
	{price: num.NewUint(26267900000), t: 1695710876151199000},
	{price: num.NewUint(26266900000), t: 1695710871077299000},
	{price: num.NewUint(26276500000), t: 1695710865532192000},
	{price: num.NewUint(26275500000), t: 1695710859710597000},
	{price: num.NewUint(26268900000), t: 1695710854616313000},
	{price: num.NewUint(26275500000), t: 1695710848812067000},
	{price: num.NewUint(26268900000), t: 1695710843545420000},
	{price: num.NewUint(26264900000), t: 1695710838374483000},
	{price: num.NewUint(26266900000), t: 1695710832821801000},
	{price: num.NewUint(26266900000), t: 1695710827548482000},
	{price: num.NewUint(26276500000), t: 1695710822307620000},
	{price: num.NewUint(26275500000), t: 1695710816963022000},
	{price: num.NewUint(26276500000), t: 1695710811859900000},
	{price: num.NewUint(26276500000), t: 1695710800815513000},
	{price: num.NewUint(26265900000), t: 1695710795739907000},
	{price: num.NewUint(26267900000), t: 1695710790505187000},
	{price: num.NewUint(26275500000), t: 1695710785334602000},
	{price: num.NewUint(26275500000), t: 1695710779979031000},
	{price: num.NewUint(26275500000), t: 1695710774770529000},
	{price: num.NewUint(26275500000), t: 1695710769057359000},
	{price: num.NewUint(26275500000), t: 1695710763888678000},
	{price: num.NewUint(26275500000), t: 1695710758729327000},
	{price: num.NewUint(26265900000), t: 1695710753512164000},
	{price: num.NewUint(26275500000), t: 1695710748433756000},
	{price: num.NewUint(26268900000), t: 1695710743227908000},
	{price: num.NewUint(26275500000), t: 1695710738208568000},
	{price: num.NewUint(26266900000), t: 1695710733090273000},
	{price: num.NewUint(26267900000), t: 1695710727774367000},
	{price: num.NewUint(26268900000), t: 1695710722654310000},
	{price: num.NewUint(26268900000), t: 1695710717412332000},
	{price: num.NewUint(26268900000), t: 1695710711788331000},
	{price: num.NewUint(26268900000), t: 1695710706413224000},
	{price: num.NewUint(26275500000), t: 1695710701040221000},
	{price: num.NewUint(26268900000), t: 1695710695340581000},
	{price: num.NewUint(26275500000), t: 1695710690179011000},
	{price: num.NewUint(26268900000), t: 1695710684822992000},
	{price: num.NewUint(26276500000), t: 1695710679547858000},
}

const testData = `{
  "data": {
    "fundingPeriodDataPoints": {
      "edges": [
        {
          "node": {
            "timestamp": "2023-08-16T13:52:00Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:51:36Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:51:00Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:50:36Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:50:00Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:49:36Z",
            "seq": 6,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:49:00Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:48:36Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:48:12Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:47:36Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:47:00Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:46:36Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:46:00Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:45:36Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:45:00Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:44:36Z",
            "seq": 5,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:44:00Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:43:36Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:43:00Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:42:36Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:42:00Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:41:36Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:41:24Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:40:36Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:40:00Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:39:36Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:39:12Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:39:12Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:38:48Z",
            "seq": 4,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:38:12Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:37:36Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:37:00Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:36:36Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:36:00Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:35:36Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:35:00Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:34:36Z",
            "seq": 3,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:34:00Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:33:36Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:33:00Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:32:36Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:32:00Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:31:48Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:31:00Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:30:36Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:30:00Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:29:36Z",
            "seq": 2,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:29:00Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:28:36Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:28:00Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:27:36Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:27:12Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:26:36Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:26:00Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:25:36Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:25:12Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:24:48Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        },
        {
          "node": {
            "timestamp": "2023-08-16T13:24:00Z",
            "seq": 1,
            "price": "29124220000",
            "twap": "29124220000",
            "dataPointSource": "SOURCE_EXTERNAL"
          }
        }
      ]
    }
  }
}`

func getGQLData() (*GQL, error) {
	ret := GQL{}
	if err := json.Unmarshal([]byte(testData), &ret); err != nil {
		return nil, err
	}
	ret.Sort()
	return &ret, nil
}

func (g *GQL) Sort() {
	// group by sequence
	sort.SliceStable(g.Data.FundingDataPoints.Edges, func(i, j int) bool {
		return g.Data.FundingDataPoints.Edges[i].Node.Seq < g.Data.FundingDataPoints.Edges[j].Node.Seq
	})
	for i, j := 0, len(g.Data.FundingDataPoints.Edges)-1; i < j; i, j = i+1, j-1 {
		g.Data.FundingDataPoints.Edges[i], g.Data.FundingDataPoints.Edges[j] = g.Data.FundingDataPoints.Edges[j], g.Data.FundingDataPoints.Edges[i]
	}
}

func (g *GQL) GetDataPoints() []DataPoint {
	ret := make([]DataPoint, 0, len(g.Data.FundingDataPoints.Edges))
	for _, n := range g.Data.FundingDataPoints.Edges {
		p, _ := num.UintFromString(n.Node.Price, 10)
		twap, _ := num.UintFromString(n.Node.TWAP, 10)
		ret = append(ret, DataPoint{
			price: p,
			t:     n.Node.Timestamp.UnixNano(),
			seq:   n.Node.Seq,
			twap:  twap,
		})
	}
	return ret
}
