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
	t.Run("test with real life data", TestWithRealLifeData)
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

func TestWithRealLifeData(t *testing.T) {
	// margin factor is 0.5
	perp := testPerpetualWithOpts(t, "0", "0", "0", "0.5")
	defer perp.ctrl.Finish()
	ctx := context.Background()

	perp.broker.EXPECT().Send(gomock.Any()).Times(1)
	perp.perpetual.OnLeaveOpeningAuction(ctx, 1695732284000000000)

	// regardless of the order the points are submitted we should get the same TWAP, so we randomly shuffle the order

	points := fairgroundTestData[:]

	// we need external/internal data so we submit the same set with a difference
	submitDataWithDifference(t, perp, points, 1000)
	productData := perp.perpetual.GetData(1695734084000000000)
	perpData := productData.Data.(*types.PerpetualData)
	// assert.Equal(t, "26208680677", perpData.InternalTWAP)
	assert.Equal(t, "26208680677", perpData.ExternalTWAP)
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
	{price: num.NewUint(26218200000), t: 1695732280465388987},
	{price: num.NewUint(26215200000), t: 1695732285778953712},
	{price: num.NewUint(26215200000), t: 1695732291519890890},
	{price: num.NewUint(26224900000), t: 1695732302063584555},
	{price: num.NewUint(26225900000), t: 1695732307520224492},
	{price: num.NewUint(26226900000), t: 1695732312722296473},
	{price: num.NewUint(26216200000), t: 1695732317942436729},
	{price: num.NewUint(26225900000), t: 1695732323091533669},
	{price: num.NewUint(26217200000), t: 1695732328355256911},
	{price: num.NewUint(26224900000), t: 1695732333572070817},
	{price: num.NewUint(26226900000), t: 1695732338763872718},
	{price: num.NewUint(26215200000), t: 1695732344120449458},
	{price: num.NewUint(26224900000), t: 1695732349644254838},
	{price: num.NewUint(26226900000), t: 1695732355285206008},
	{price: num.NewUint(26226900000), t: 1695732360591668658},
	{price: num.NewUint(26215200000), t: 1695732365695942476},
	{price: num.NewUint(26215200000), t: 1695732370975703149},
	{price: num.NewUint(26225700000), t: 1695732376297967254},
	{price: num.NewUint(26225900000), t: 1695732381436167961},
	{price: num.NewUint(26225900000), t: 1695732386668316333},
	{price: num.NewUint(26224700000), t: 1695732391726800987},
	{price: num.NewUint(26216100000), t: 1695732397164876366},
	{price: num.NewUint(26216100000), t: 1695732402314828282},
	{price: num.NewUint(26216900000), t: 1695732407398568029},
	{price: num.NewUint(26215900000), t: 1695732412860529952},
	{price: num.NewUint(26215200000), t: 1695732418007643223},
	{price: num.NewUint(26227600000), t: 1695732423328037396},
	{price: num.NewUint(26224600000), t: 1695732428742962876},
	{price: num.NewUint(26226600000), t: 1695732434069615676},
	{price: num.NewUint(26226600000), t: 1695732439101439880},
	{price: num.NewUint(26215900000), t: 1695732444463730305},
	{price: num.NewUint(26225300000), t: 1695732449583302907},
	{price: num.NewUint(26214600000), t: 1695732455246417601},
	{price: num.NewUint(26227300000), t: 1695732460345117118},
	{price: num.NewUint(26214000000), t: 1695732465514249211},
	{price: num.NewUint(26216600000), t: 1695732471010084126},
	{price: num.NewUint(26214600000), t: 1695732476097220257},
	{price: num.NewUint(26223300000), t: 1695732481287880044},
	{price: num.NewUint(26224300000), t: 1695732486566571353},
	{price: num.NewUint(26214600000), t: 1695732491643168193},
	{price: num.NewUint(26223300000), t: 1695732496732266314},
	{price: num.NewUint(26224300000), t: 1695732502035045153},
	{price: num.NewUint(26215600000), t: 1695732507350275767},
	{price: num.NewUint(26224300000), t: 1695732512756947746},
	{price: num.NewUint(26225300000), t: 1695732517902439299},
	{price: num.NewUint(26214600000), t: 1695732523160079421},
	{price: num.NewUint(26216600000), t: 1695732528385513247},
	{price: num.NewUint(26215600000), t: 1695732534024923733},
	{price: num.NewUint(26214600000), t: 1695732539280980320},
	{price: num.NewUint(26214600000), t: 1695732544556249481},
	{price: num.NewUint(26214000000), t: 1695732549778628761},
	{price: num.NewUint(26214000000), t: 1695732555112906208},
	{price: num.NewUint(26214000000), t: 1695732560145908912},
	{price: num.NewUint(26214000000), t: 1695732565454560393},
	{price: num.NewUint(26214000000), t: 1695732570931999623},
	{price: num.NewUint(26206400000), t: 1695732576248494477},
	{price: num.NewUint(26214000000), t: 1695732586755628259},
	{price: num.NewUint(26215000000), t: 1695732592348904966},
	{price: num.NewUint(26204400000), t: 1695732597474317620},
	{price: num.NewUint(26213000000), t: 1695732602716331797},
	{price: num.NewUint(26205400000), t: 1695732608256408828},
	{price: num.NewUint(26215000000), t: 1695732613598330663},
	{price: num.NewUint(26215000000), t: 1695732618736574629},
	{price: num.NewUint(26203400000), t: 1695732623781242401},
	{price: num.NewUint(26206400000), t: 1695732629221791073},
	{price: num.NewUint(26214000000), t: 1695732634695301213},
	{price: num.NewUint(26214000000), t: 1695732639801786255},
	{price: num.NewUint(26204400000), t: 1695732645512670572},
	{price: num.NewUint(26204400000), t: 1695732650776258196},
	{price: num.NewUint(26206400000), t: 1695732656035691550},
	{price: num.NewUint(26214000000), t: 1695732661227924134},
	{price: num.NewUint(26213000000), t: 1695732666355758186},
	{price: num.NewUint(26206400000), t: 1695732671470227493},
	{price: num.NewUint(26215300000), t: 1695732676809602411},
	{price: num.NewUint(26203400000), t: 1695732682071254579},
	{price: num.NewUint(26205400000), t: 1695732687342216991},
	{price: num.NewUint(26215000000), t: 1695732692572006886},
	{price: num.NewUint(26203400000), t: 1695732698269212851},
	{price: num.NewUint(26216000000), t: 1695732703471326554},
	{price: num.NewUint(26203400000), t: 1695732708572392529},
	{price: num.NewUint(26213000000), t: 1695732714127646879},
	{price: num.NewUint(26216000000), t: 1695732719458552278},
	{price: num.NewUint(26213000000), t: 1695732724932722980},
	{price: num.NewUint(26204400000), t: 1695732730122833565},
	{price: num.NewUint(26203400000), t: 1695732735404866640},
	{price: num.NewUint(26216000000), t: 1695732740687393647},
	{price: num.NewUint(26205400000), t: 1695732745988232573},
	{price: num.NewUint(26205400000), t: 1695732750997481272},
	{price: num.NewUint(26215000000), t: 1695732756137399927},
	{price: num.NewUint(26203400000), t: 1695732761306421926},
	{price: num.NewUint(26214000000), t: 1695732766693791253},
	{price: num.NewUint(26204400000), t: 1695732772339920631},
	{price: num.NewUint(26216000000), t: 1695732777743298518},
	{price: num.NewUint(26206400000), t: 1695732783006324809},
	{price: num.NewUint(26214000000), t: 1695732788178423488},
	{price: num.NewUint(26214000000), t: 1695732793549870995},
	{price: num.NewUint(26205400000), t: 1695732798889933606},
	{price: num.NewUint(26204400000), t: 1695732804096670611},
	{price: num.NewUint(26213000000), t: 1695732809197101954},
	{price: num.NewUint(26213000000), t: 1695732814665717317},
	{price: num.NewUint(26206400000), t: 1695732819805994968},
	{price: num.NewUint(26213000000), t: 1695732825522911311},
	{price: num.NewUint(26213000000), t: 1695732830769869771},
	{price: num.NewUint(26213000000), t: 1695732835790789846},
	{price: num.NewUint(26214000000), t: 1695732841078407208},
	{price: num.NewUint(26206400000), t: 1695732846395607405},
	{price: num.NewUint(26214000000), t: 1695732851768951551},
	{price: num.NewUint(26204400000), t: 1695732856790974187},
	{price: num.NewUint(26204400000), t: 1695732861804427273},
	{price: num.NewUint(26217000000), t: 1695732867220343923},
	{price: num.NewUint(26206400000), t: 1695732872430842361},
	{price: num.NewUint(26205400000), t: 1695732878060493842},
	{price: num.NewUint(26205400000), t: 1695732883532392873},
	{price: num.NewUint(26205400000), t: 1695732888947405966},
	{price: num.NewUint(26215000000), t: 1695732894410972585},
	{price: num.NewUint(26216000000), t: 1695732899906750267},
	{price: num.NewUint(26204400000), t: 1695732905558315626},
	{price: num.NewUint(26213000000), t: 1695732910940118590},
	{price: num.NewUint(26214000000), t: 1695732916217255335},
	{price: num.NewUint(26206400000), t: 1695732921496582612},
	{price: num.NewUint(26214000000), t: 1695732926997834284},
	{price: num.NewUint(26204400000), t: 1695732932420419575},
	{price: num.NewUint(26201400000), t: 1695732937584754204},
	{price: num.NewUint(26201400000), t: 1695732942960222614},
	{price: num.NewUint(26200400000), t: 1695732948192632513},
	{price: num.NewUint(26214000000), t: 1695732953929627089},
	{price: num.NewUint(26205400000), t: 1695732959276777009},
	{price: num.NewUint(26204400000), t: 1695732964429479883},
	{price: num.NewUint(26203400000), t: 1695732969952950915},
	{price: num.NewUint(26217000000), t: 1695732975421363094},
	{price: num.NewUint(26202400000), t: 1695732980684095108},
	{price: num.NewUint(26206400000), t: 1695732986097580936},
	{price: num.NewUint(26205400000), t: 1695732991225127667},
	{price: num.NewUint(26214000000), t: 1695732996815866080},
	{price: num.NewUint(26215000000), t: 1695733001925818825},
	{price: num.NewUint(26204400000), t: 1695733007309379221},
	{price: num.NewUint(26215000000), t: 1695733012438065706},
	{price: num.NewUint(26203400000), t: 1695733018221255925},
	{price: num.NewUint(26213000000), t: 1695733023446103535},
	{price: num.NewUint(26214000000), t: 1695733028800840046},
	{price: num.NewUint(26216000000), t: 1695733034244861221},
	{price: num.NewUint(26213000000), t: 1695733039635508456},
	{price: num.NewUint(26205400000), t: 1695733044992828226},
	{price: num.NewUint(26205400000), t: 1695733050177846108},
	{price: num.NewUint(26204400000), t: 1695733055355309288},
	{price: num.NewUint(26205400000), t: 1695733060636029948},
	{price: num.NewUint(26214000000), t: 1695733065894204714},
	{price: num.NewUint(26215000000), t: 1695733071113237421},
	{price: num.NewUint(26203400000), t: 1695733076614617306},
	{price: num.NewUint(26205400000), t: 1695733087025839227},
	{price: num.NewUint(26216000000), t: 1695733092679503714},
	{price: num.NewUint(26216000000), t: 1695733097961110701},
	{price: num.NewUint(26213000000), t: 1695733103138898267},
	{price: num.NewUint(26213000000), t: 1695733108375977072},
	{price: num.NewUint(26217000000), t: 1695733113758604458},
	{price: num.NewUint(26217000000), t: 1695733119080136208},
	{price: num.NewUint(26206400000), t: 1695733124807532342},
	{price: num.NewUint(26206400000), t: 1695733129969723715},
	{price: num.NewUint(26206400000), t: 1695733135018083944},
	{price: num.NewUint(26204400000), t: 1695733140402347490},
	{price: num.NewUint(26204400000), t: 1695733145731086069},
	{price: num.NewUint(26206400000), t: 1695733150849420279},
	{price: num.NewUint(26215000000), t: 1695733156268191194},
	{price: num.NewUint(26204400000), t: 1695733161308793593},
	{price: num.NewUint(26204400000), t: 1695733166604831024},
	{price: num.NewUint(26204400000), t: 1695733172321127769},
	{price: num.NewUint(26213000000), t: 1695733177649637288},
	{price: num.NewUint(26214000000), t: 1695733182918032623},
	{price: num.NewUint(26215000000), t: 1695733188091937533},
	{price: num.NewUint(26204400000), t: 1695733193309629977},
	{price: num.NewUint(26204400000), t: 1695733198722969698},
	{price: num.NewUint(26215000000), t: 1695733204007566000},
	{price: num.NewUint(26213000000), t: 1695733209551796618},
	{price: num.NewUint(26205400000), t: 1695733215145904039},
	{price: num.NewUint(26215000000), t: 1695733220346812208},
	{price: num.NewUint(26203400000), t: 1695733225847723103},
	{price: num.NewUint(26213000000), t: 1695733231064256041},
	{price: num.NewUint(26206400000), t: 1695733236232707636},
	{price: num.NewUint(26205400000), t: 1695733241533207100},
	{price: num.NewUint(26204400000), t: 1695733246736564040},
	{price: num.NewUint(26204400000), t: 1695733252153197119},
	{price: num.NewUint(26215000000), t: 1695733257640216417},
	{price: num.NewUint(26214000000), t: 1695733263123800878},
	{price: num.NewUint(26215000000), t: 1695733268580813000},
	{price: num.NewUint(26215900000), t: 1695733274224981257},
	{price: num.NewUint(26203400000), t: 1695733279446063404},
	{price: num.NewUint(26215900000), t: 1695733284823314656},
	{price: num.NewUint(26206400000), t: 1695733290569628828},
	{price: num.NewUint(26214000000), t: 1695733295850655745},
	{price: num.NewUint(26204400000), t: 1695733300956452265},
	{price: num.NewUint(26206400000), t: 1695733306643674730},
	{price: num.NewUint(26214000000), t: 1695733311876138089},
	{price: num.NewUint(26204400000), t: 1695733317586603218},
	{price: num.NewUint(26213000000), t: 1695733323186072392},
	{price: num.NewUint(26205400000), t: 1695733328390152187},
	{price: num.NewUint(26206400000), t: 1695733333480069546},
	{price: num.NewUint(26214000000), t: 1695733339314075563},
	{price: num.NewUint(26214000000), t: 1695733344606835981},
	{price: num.NewUint(26204400000), t: 1695733349661165422},
	{price: num.NewUint(26203400000), t: 1695733354807797443},
	{price: num.NewUint(26203400000), t: 1695733360046532023},
	{price: num.NewUint(26213000000), t: 1695733365684978963},
	{price: num.NewUint(26206400000), t: 1695733371024182593},
	{price: num.NewUint(26206400000), t: 1695733376737190655},
	{price: num.NewUint(26204400000), t: 1695733382278895421},
	{price: num.NewUint(26205400000), t: 1695733387578481548},
	{price: num.NewUint(26213000000), t: 1695733392876372381},
	{price: num.NewUint(26214000000), t: 1695733398215008863},
	{price: num.NewUint(26205400000), t: 1695733403576230493},
	{price: num.NewUint(26204400000), t: 1695733408774544777},
	{price: num.NewUint(26204400000), t: 1695733414046226273},
	{price: num.NewUint(26205400000), t: 1695733419621888461},
	{price: num.NewUint(26205400000), t: 1695733424801784381},
	{price: num.NewUint(26206400000), t: 1695733429919916592},
	{price: num.NewUint(26204400000), t: 1695733435744006326},
	{price: num.NewUint(26214000000), t: 1695733440913595590},
	{price: num.NewUint(26204400000), t: 1695733446258960251},
	{price: num.NewUint(26214000000), t: 1695733451808030531},
	{price: num.NewUint(26213000000), t: 1695733457268410915},
	{price: num.NewUint(26214000000), t: 1695733462612935550},
	{price: num.NewUint(26205400000), t: 1695733467680359109},
	{price: num.NewUint(26214000000), t: 1695733473200906399},
	{price: num.NewUint(26204400000), t: 1695733478720049642},
	{price: num.NewUint(26213000000), t: 1695733484056162710},
	{price: num.NewUint(26214000000), t: 1695733489385844967},
	{price: num.NewUint(26214000000), t: 1695733494695466884},
	{price: num.NewUint(26203400000), t: 1695733499865915748},
	{price: num.NewUint(26214000000), t: 1695733505118997056},
	{price: num.NewUint(26214000000), t: 1695733510265315593},
	{price: num.NewUint(26213000000), t: 1695733516033269184},
	{price: num.NewUint(26205400000), t: 1695733521394641547},
	{price: num.NewUint(26214000000), t: 1695733526997540991},
	{price: num.NewUint(26204400000), t: 1695733532560051244},
	{price: num.NewUint(26205400000), t: 1695733537688326275},
	{price: num.NewUint(26206400000), t: 1695733543069581959},
	{price: num.NewUint(26205400000), t: 1695733548381730028},
	{price: num.NewUint(26204400000), t: 1695733553626560721},
	{price: num.NewUint(26213000000), t: 1695733558762222765},
	{price: num.NewUint(26214000000), t: 1695733564214031305},
	{price: num.NewUint(26214000000), t: 1695733569390857525},
	{price: num.NewUint(26214000000), t: 1695733574695304272},
	{price: num.NewUint(26206400000), t: 1695733579766317307},
	{price: num.NewUint(26217000000), t: 1695733584864524180},
	{price: num.NewUint(26203400000), t: 1695733590386042414},
	{price: num.NewUint(26203400000), t: 1695733595541440663},
	{price: num.NewUint(26206400000), t: 1695733600545271182},
	{price: num.NewUint(26206400000), t: 1695733605651257828},
	{price: num.NewUint(26214000000), t: 1695733610915965745},
	{price: num.NewUint(26204400000), t: 1695733616634441609},
	{price: num.NewUint(26206400000), t: 1695733621746289231},
	{price: num.NewUint(26213000000), t: 1695733626970582343},
	{price: num.NewUint(26214000000), t: 1695733633479413798},
	{price: num.NewUint(26204400000), t: 1695733638845827284},
	{price: num.NewUint(26216000000), t: 1695733644684532375},
	{price: num.NewUint(26217000000), t: 1695733650285202173},
	{price: num.NewUint(26202400000), t: 1695733655965495333},
	{price: num.NewUint(26214000000), t: 1695733661676980379},
	{price: num.NewUint(26205400000), t: 1695733666960169830},
	{price: num.NewUint(26204400000), t: 1695733672348623013},
	{price: num.NewUint(26205400000), t: 1695733677795663995},
	{price: num.NewUint(26204400000), t: 1695733683207430253},
	{price: num.NewUint(26206400000), t: 1695733688454980444},
	{price: num.NewUint(26202400000), t: 1695733693883817572},
	{price: num.NewUint(26201400000), t: 1695733699440495537},
	{price: num.NewUint(26213000000), t: 1695733704723321263},
	{price: num.NewUint(26213000000), t: 1695733710178462290},
	{price: num.NewUint(26205400000), t: 1695733715656635024},
	{price: num.NewUint(26206400000), t: 1695733720939201367},
	{price: num.NewUint(26214000000), t: 1695733726051211495},
	{price: num.NewUint(26205400000), t: 1695733731385172975},
	{price: num.NewUint(26214000000), t: 1695733737087814828},
	{price: num.NewUint(26206400000), t: 1695733742279524408},
	{price: num.NewUint(26204400000), t: 1695733747545611721},
	{price: num.NewUint(26213000000), t: 1695733753058069307},
	{price: num.NewUint(26215000000), t: 1695733758375688000},
	{price: num.NewUint(26203400000), t: 1695733763655794160},
	{price: num.NewUint(26216000000), t: 1695733768845334575},
	{price: num.NewUint(26217000000), t: 1695733774303421918},
	{price: num.NewUint(26206400000), t: 1695733779733984901},
	{price: num.NewUint(26206400000), t: 1695733785159025950},
	{price: num.NewUint(26206400000), t: 1695733790650902524},
	{price: num.NewUint(26205400000), t: 1695733795797252773},
	{price: num.NewUint(26216000000), t: 1695733801259178711},
	{price: num.NewUint(26221000000), t: 1695733806619971509},
	{price: num.NewUint(26221000000), t: 1695733811734619158},
	{price: num.NewUint(26213000000), t: 1695733817149243562},
	{price: num.NewUint(26206400000), t: 1695733822290706953},
	{price: num.NewUint(26215000000), t: 1695733827531976753},
	{price: num.NewUint(26205400000), t: 1695733833204838443},
	{price: num.NewUint(26213000000), t: 1695733843926128155},
	{price: num.NewUint(26204400000), t: 1695733849101127333},
	{price: num.NewUint(26203400000), t: 1695733854350641675},
	{price: num.NewUint(26187900000), t: 1695733859795908328},
	{price: num.NewUint(26203500000), t: 1695733865035163228},
	{price: num.NewUint(26185900000), t: 1695733870739597026},
	{price: num.NewUint(26187900000), t: 1695733876058961235},
	{price: num.NewUint(26195500000), t: 1695733881352889209},
	{price: num.NewUint(26196500000), t: 1695733886835018996},
	{price: num.NewUint(26194500000), t: 1695733892099725918},
	{price: num.NewUint(26187900000), t: 1695733897537955894},
	{price: num.NewUint(26196500000), t: 1695733902594419134},
	{price: num.NewUint(26197500000), t: 1695733908062975235},
	{price: num.NewUint(26198500000), t: 1695733913305393779},
	{price: num.NewUint(26184900000), t: 1695733918501348411},
	{price: num.NewUint(26195500000), t: 1695733924077803648},
	{price: num.NewUint(26194500000), t: 1695733929506640376},
	{price: num.NewUint(26186900000), t: 1695733934810060948},
	{price: num.NewUint(26185900000), t: 1695733939859460887},
	{price: num.NewUint(26196500000), t: 1695733945346635635},
	{price: num.NewUint(26197500000), t: 1695733950392921816},
	{price: num.NewUint(26184900000), t: 1695733955637877685},
	{price: num.NewUint(26186900000), t: 1695733960742456358},
	{price: num.NewUint(26184900000), t: 1695733966489793419},
	{price: num.NewUint(26184900000), t: 1695733971765845078},
	{price: num.NewUint(26197500000), t: 1695733976945914915},
	{price: num.NewUint(26186900000), t: 1695733982003126158},
	{price: num.NewUint(26185900000), t: 1695733987255611292},
	{price: num.NewUint(26194500000), t: 1695733992522804482},
	{price: num.NewUint(26187900000), t: 1695733997824071870},
	{price: num.NewUint(26195000000), t: 1695734002899635979},
	{price: num.NewUint(26200500000), t: 1695734008261956110},
	{price: num.NewUint(26186900000), t: 1695734018709587678},
	{price: num.NewUint(26186900000), t: 1695734024027613220},
	{price: num.NewUint(26185900000), t: 1695734029387273015},
	{price: num.NewUint(26185900000), t: 1695734034929100746},
	{price: num.NewUint(26183900000), t: 1695734040167665312},
	{price: num.NewUint(26183900000), t: 1695734045556651215},
	{price: num.NewUint(26183900000), t: 1695734050952961413},
	{price: num.NewUint(26198500000), t: 1695734056182927227},
	{price: num.NewUint(26199500000), t: 1695734061521658202},
	{price: num.NewUint(26187900000), t: 1695734066830603407},
	{price: num.NewUint(26186900000), t: 1695734072309740531},
	{price: num.NewUint(26183900000), t: 1695734077360850161},
	{price: num.NewUint(26186900000), t: 1695734082633687642},
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
