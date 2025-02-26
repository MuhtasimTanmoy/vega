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

package sqlstore_test

import (
	"context"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/sqlstore"
	"code.vegaprotocol.io/vega/protos/vega"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAssetID = "deadbeef"

func TestMarginLevels(t *testing.T) {
	t.Run("Add should insert margin levels that don't exist in the current block", testInsertMarginLevels)
	t.Run("Add should insert margin levels that already exist in the same block", testDuplicateMarginLevelInSameBlock)
	t.Run("GetMarginLevelsByID should return the latest state of margin levels for all markets if only party ID is provided", testGetMarginLevelsByPartyID)
	t.Run("GetMarginLevelsByID should return the latest state of margin levels for all parties if only market ID is provided", testGetMarginLevelsByMarketID)
	t.Run("GetMarginLevelsByID should return the latest state of margin levels for the given party/market ID", testGetMarginLevelsByID)
	t.Run("GetByTxHash", testGetMarginByTxHash)

	t.Run("GetMarginLevelsByIDWithCursorPagination should return all margin levels for a given party if no pagination is provided", testGetMarginLevelsByIDPaginationWithPartyNoCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return all margin levels for a given market if no pagination is provided", testGetMarginLevelsByIDPaginationWithMarketNoCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the first page of margin levels for a given party if first is set with no after cursor", testGetMarginLevelsByIDPaginationWithPartyFirstNoAfterCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the first page of margin levels for a given market if first is set with no after cursor", testGetMarginLevelsByIDPaginationWithMarketFirstNoAfterCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the last page of margin levels for a given party if last is set with no before cursor", testGetMarginLevelsByIDPaginationWithPartyLastNoBeforeCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the last page of margin levels for a given market if last is set with no before cursor", testGetMarginLevelsByIDPaginationWithMarketLastNoBeforeCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given party if first is set with after cursor", testGetMarginLevelsByIDPaginationWithPartyFirstAndAfterCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given market if first is set with after cursor", testGetMarginLevelsByIDPaginationWithMarketFirstAndAfterCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given party if last is set with before cursor", testGetMarginLevelsByIDPaginationWithPartyLastAndBeforeCursor)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given market if last is set with before cursor", testGetMarginLevelsByIDPaginationWithMarketLastAndBeforeCursor)

	t.Run("GetMarginLevelsByIDWithCursorPagination should return all margin levels for a given party if no pagination is provided - Newest First", testGetMarginLevelsByIDPaginationWithPartyNoCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return all margin levels for a given market if no pagination is provided - Newest First", testGetMarginLevelsByIDPaginationWithMarketNoCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the first page of margin levels for a given party if first is set with no after cursor - Newest First", testGetMarginLevelsByIDPaginationWithPartyFirstNoAfterCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the first page of margin levels for a given market if first is set with no after cursor - Newest First", testGetMarginLevelsByIDPaginationWithMarketFirstNoAfterCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the last page of margin levels for a given party if last is set with no before cursor - Newest First", testGetMarginLevelsByIDPaginationWithPartyLastNoBeforeCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the last page of margin levels for a given market if last is set with no before cursor - Newest First", testGetMarginLevelsByIDPaginationWithMarketLastNoBeforeCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given party if first is set with after cursor - Newest First", testGetMarginLevelsByIDPaginationWithPartyFirstAndAfterCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given market if first is set with after cursor - Newest First", testGetMarginLevelsByIDPaginationWithMarketFirstAndAfterCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given party if last is set with before cursor - Newest First", testGetMarginLevelsByIDPaginationWithPartyLastAndBeforeCursorNewestFirst)
	t.Run("GetMarginLevelsByIDWithCursorPagination should return the requested page of margin levels for a given market if last is set with before cursor - Newest First", testGetMarginLevelsByIDPaginationWithMarketLastAndBeforeCursorNewestFirst)
}

func setupMarginLevelTests(t *testing.T, ctx context.Context) (*testBlockSource, *sqlstore.MarginLevels, *sqlstore.Accounts, sqlstore.Connection) {
	t.Helper()

	bs := sqlstore.NewBlocks(connectionSource)
	testBlockSource := &testBlockSource{bs, time.Now()}

	block := testBlockSource.getNextBlock(t, ctx)

	assets := sqlstore.NewAssets(connectionSource)

	testAsset := entities.Asset{
		ID:            testAssetID,
		Name:          "testAssetName",
		Symbol:        "tan",
		Decimals:      1,
		Quantum:       decimal.NewFromInt(1),
		Source:        "TS",
		ERC20Contract: "ET",
		VegaTime:      block.VegaTime,
	}

	err := assets.Add(ctx, testAsset)
	if err != nil {
		t.Fatalf("failed to add test asset:%s", err)
	}

	accountStore := sqlstore.NewAccounts(connectionSource)
	ml := sqlstore.NewMarginLevels(connectionSource)

	return testBlockSource, ml, accountStore, connectionSource.Connection
}

func testInsertMarginLevels(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)

	marginLevelProto := getMarginLevelProto()
	marginLevel, err := entities.MarginLevelsFromProto(ctx, marginLevelProto, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel)
	require.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func testDuplicateMarginLevelInSameBlock(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)

	marginLevelProto := getMarginLevelProto()
	marginLevel, err := entities.MarginLevelsFromProto(ctx, marginLevelProto, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel)
	require.NoError(t, err)

	err = ml.Add(marginLevel)
	require.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func getMarginLevelProto() *vega.MarginLevels {
	return getMarginLevelWithMaintenanceProto("1000", "deadbeef", "deadbeef", time.Now().UnixNano())
}

func getMarginLevelWithMaintenanceProto(maintenanceMargin, partyID, marketID string, timestamp int64) *vega.MarginLevels {
	return &vega.MarginLevels{
		MaintenanceMargin:      maintenanceMargin,
		SearchLevel:            "1000",
		InitialMargin:          "1000",
		CollateralReleaseLevel: "1000",
		OrderMargin:            "0",
		PartyId:                partyID,
		MarketId:               marketID,
		Asset:                  testAssetID,
		Timestamp:              timestamp,
		MarginMode:             vega.MarginMode_MARGIN_MODE_CROSS_MARGIN,
		MarginFactor:           "0",
	}
}

func testGetMarginLevelsByPartyID(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	ml1 := getMarginLevelProto()
	ml2 := getMarginLevelProto()
	ml3 := getMarginLevelProto()
	ml4 := getMarginLevelProto()

	ml2.MarketId = "deadbaad"

	ml3.Timestamp = ml2.Timestamp + 1000000000
	ml3.MaintenanceMargin = "2000"
	ml3.SearchLevel = "2000"

	ml4.Timestamp = ml2.Timestamp + 1000000000
	ml4.MaintenanceMargin = "2000"
	ml4.SearchLevel = "2000"
	ml4.MarketId = "deadbaad"

	marginLevel1, err := entities.MarginLevelsFromProto(ctx, ml1, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel2, err := entities.MarginLevelsFromProto(ctx, ml2, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel1)
	require.NoError(t, err)
	err = ml.Add(marginLevel2)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	require.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, rowCount)

	block = blockSource.getNextBlock(t, ctx)
	marginLevel3, err := entities.MarginLevelsFromProto(ctx, ml3, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel4, err := entities.MarginLevelsFromProto(ctx, ml4, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel3)
	require.NoError(t, err)
	err = ml.Add(marginLevel4)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	require.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 4, rowCount)

	got, _, err := ml.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", entities.CursorPagination{})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(got))

	// We have to truncate the time because Postgres only supports time to microsecond granularity.
	want1 := marginLevel3
	want1.Timestamp = want1.Timestamp.Truncate(time.Microsecond)
	want1.VegaTime = want1.VegaTime.Truncate(time.Microsecond)

	want2 := marginLevel4
	want2.Timestamp = want2.Timestamp.Truncate(time.Microsecond)
	want2.VegaTime = want2.VegaTime.Truncate(time.Microsecond)

	want := []entities.MarginLevels{want1, want2}

	assert.ElementsMatch(t, want, got)
}

func testGetMarginLevelsByMarketID(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	ml1 := getMarginLevelProto()
	ml2 := getMarginLevelProto()
	ml3 := getMarginLevelProto()
	ml4 := getMarginLevelProto()

	ml2.PartyId = "deadbaad"

	ml3.Timestamp = ml2.Timestamp + 1000000000
	ml3.MaintenanceMargin = "2000"
	ml3.SearchLevel = "2000"

	ml4.Timestamp = ml2.Timestamp + 1000000000
	ml4.MaintenanceMargin = "2000"
	ml4.SearchLevel = "2000"
	ml4.PartyId = "deadbaad"

	marginLevel1, err := entities.MarginLevelsFromProto(ctx, ml1, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel2, err := entities.MarginLevelsFromProto(ctx, ml2, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel1)
	require.NoError(t, err)
	err = ml.Add(marginLevel2)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, rowCount)

	block = blockSource.getNextBlock(t, ctx)
	marginLevel3, err := entities.MarginLevelsFromProto(ctx, ml3, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel4, err := entities.MarginLevelsFromProto(ctx, ml4, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel3)
	require.NoError(t, err)
	err = ml.Add(marginLevel4)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 4, rowCount)

	got, _, err := ml.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", entities.CursorPagination{})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(got))

	// We have to truncate the time because Postgres only supports time to microsecond granularity.
	want1 := marginLevel3
	want1.Timestamp = want1.Timestamp.Truncate(time.Microsecond)
	want1.VegaTime = want1.VegaTime.Truncate(time.Microsecond)

	want2 := marginLevel4
	want2.Timestamp = want2.Timestamp.Truncate(time.Microsecond)
	want2.VegaTime = want2.VegaTime.Truncate(time.Microsecond)

	want := []entities.MarginLevels{want1, want2}

	assert.ElementsMatch(t, want, got)
}

func testGetMarginLevelsByID(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	ml1 := getMarginLevelProto()
	ml2 := getMarginLevelProto()
	ml3 := getMarginLevelProto()
	ml4 := getMarginLevelProto()

	ml2.PartyId = "DEADBAAD"

	ml3.Timestamp = ml2.Timestamp + 1000000000
	ml3.MaintenanceMargin = "2000"
	ml3.SearchLevel = "2000"

	ml4.Timestamp = ml2.Timestamp + 1000000000
	ml4.MaintenanceMargin = "2000"
	ml4.SearchLevel = "2000"
	ml4.PartyId = "DEADBAAD"

	marginLevel1, err := entities.MarginLevelsFromProto(ctx, ml1, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel2, err := entities.MarginLevelsFromProto(ctx, ml2, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel1)
	require.NoError(t, err)
	err = ml.Add(marginLevel2)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, rowCount)

	block = blockSource.getNextBlock(t, ctx)
	marginLevel3, err := entities.MarginLevelsFromProto(ctx, ml3, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel4, err := entities.MarginLevelsFromProto(ctx, ml4, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel3)
	require.NoError(t, err)
	err = ml.Add(marginLevel4)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 4, rowCount)

	got, _, err := ml.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "DEADBEEF", entities.CursorPagination{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(got))

	// We have to truncate the time because Postgres only supports time to microsecond granularity.
	want1 := marginLevel3
	want1.Timestamp = want1.Timestamp.Truncate(time.Microsecond)
	want1.VegaTime = want1.VegaTime.Truncate(time.Microsecond)

	want := []entities.MarginLevels{want1}

	assert.ElementsMatch(t, want, got)
}

func testGetMarginByTxHash(t *testing.T) {
	ctx := tempTransaction(t)

	blockSource, ml, accountStore, conn := setupMarginLevelTests(t, ctx)
	block := blockSource.getNextBlock(t, ctx)

	var rowCount int
	err := conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, rowCount)

	ml1 := getMarginLevelProto()
	ml2 := getMarginLevelProto()
	ml2.PartyId = "DEADBAAD"

	marginLevel1, err := entities.MarginLevelsFromProto(ctx, ml1, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	marginLevel2, err := entities.MarginLevelsFromProto(ctx, ml2, accountStore, generateTxHash(), block.VegaTime)
	require.NoError(t, err, "Converting margin levels proto to database entity")

	err = ml.Add(marginLevel1)
	require.NoError(t, err)
	err = ml.Add(marginLevel2)
	require.NoError(t, err)

	_, err = ml.Flush(ctx)
	assert.NoError(t, err)

	err = conn.QueryRow(ctx, `select count(*) from margin_levels`).Scan(&rowCount)
	assert.NoError(t, err)
	assert.Equal(t, 2, rowCount)

	got, err := ml.GetByTxHash(ctx, marginLevel1.TxHash)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(got))

	// We have to truncate the time because Postgres only supports time to microsecond granularity.
	want1 := marginLevel1
	want1.Timestamp = want1.Timestamp.Truncate(time.Microsecond)
	want1.VegaTime = want1.VegaTime.Truncate(time.Microsecond)

	want := []entities.MarginLevels{want1}
	assert.ElementsMatch(t, want, got)

	got2, err := ml.GetByTxHash(ctx, marginLevel2.TxHash)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(got2))

	// We have to truncate the time because Postgres only supports time to microsecond granularity.
	want2 := marginLevel2
	want2.Timestamp = want2.Timestamp.Truncate(time.Microsecond)
	want2.VegaTime = want2.VegaTime.Truncate(time.Microsecond)

	want = []entities.MarginLevels{want2}
	assert.ElementsMatch(t, want, got2)
}

func populateMarginLevelPaginationTestData(t *testing.T, ctx context.Context) (*sqlstore.MarginLevels, map[int]entities.Block, map[int]entities.MarginLevels) {
	t.Helper()

	blockSource, mlStore, accountStore, _ := setupMarginLevelTests(t, ctx)

	margins := []struct {
		maintenanceMargin string
		partyID           string
		marketID          string
	}{
		{
			// 0
			maintenanceMargin: "1000",
			partyID:           "DEADBEEF",
			marketID:          "DEADBAAD",
		},
		{
			// 1
			maintenanceMargin: "1001",
			partyID:           "DEADBEEF",
			marketID:          "0FF1CE",
		},
		{
			// 2
			maintenanceMargin: "1002",
			partyID:           "DEADBAAD",
			marketID:          "DEADBEEF",
		},
		{
			// 3
			maintenanceMargin: "1003",
			partyID:           "DEADBAAD",
			marketID:          "DEADBAAD",
		},
		{
			// 4
			maintenanceMargin: "1004",
			partyID:           "DEADBEEF",
			marketID:          "DEADC0DE",
		},
		{
			// 5
			maintenanceMargin: "1005",
			partyID:           "0FF1CE",
			marketID:          "DEADBEEF",
		},
		{
			// 6
			maintenanceMargin: "1006",
			partyID:           "DEADC0DE",
			marketID:          "DEADBEEF",
		},
		{
			// 7
			maintenanceMargin: "1007",
			partyID:           "DEADBEEF",
			marketID:          "CAFED00D",
		},
		{
			// 8
			maintenanceMargin: "1008",
			partyID:           "CAFED00D",
			marketID:          "DEADBEEF",
		},
		{
			// 9
			maintenanceMargin: "1009",
			partyID:           "DEADBAAD",
			marketID:          "DEADBAAD",
		},
		{
			// 10
			maintenanceMargin: "1010",
			partyID:           "DEADBEEF",
			marketID:          "CAFEB0BA",
		},
		{
			// 11
			maintenanceMargin: "1011",
			partyID:           "CAFEB0BA",
			marketID:          "DEADBEEF",
		},
		{
			// 12
			maintenanceMargin: "1012",
			partyID:           "DEADBAAD",
			marketID:          "DEADBAAD",
		},
		{
			// 13
			maintenanceMargin: "1013",
			partyID:           "0D15EA5E",
			marketID:          "DEADBEEF",
		},
		{
			// 14
			maintenanceMargin: "1014",
			partyID:           "DEADBEEF",
			marketID:          "0D15EA5E",
		},
	}

	blocks := make(map[int]entities.Block)
	marginLevels := make(map[int]entities.MarginLevels)

	for i, ml := range margins {
		block := blockSource.getNextBlock(t, ctx)
		mlProto := getMarginLevelWithMaintenanceProto(ml.maintenanceMargin, ml.partyID, ml.marketID, block.VegaTime.UnixNano())
		mlEntity, err := entities.MarginLevelsFromProto(ctx, mlProto, accountStore, generateTxHash(), block.VegaTime)
		require.NoError(t, err, "Converting margin levels proto to database entity")
		err = mlStore.Add(mlEntity)
		require.NoError(t, err)

		blocks[i] = block
		marginLevels[i] = mlEntity
	}

	_, err := mlStore.Flush(ctx)
	require.NoError(t, err)

	return mlStore, blocks, marginLevels
}

func testGetMarginLevelsByIDPaginationWithPartyNoCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 6)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[0],
		marginLevels[1],
		marginLevels[4],
		marginLevels[7],
		marginLevels[10],
		marginLevels[14],
	}
	assert.Equal(t, wantMarginLevels, got)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[0].VegaTime,
		AccountID: marginLevels[0].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[14].VegaTime,
		AccountID: marginLevels[14].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyNoCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 6)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[14],
		marginLevels[10],
		marginLevels[7],
		marginLevels[4],
		marginLevels[1],
		marginLevels[0],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[14].VegaTime,
		AccountID: marginLevels[14].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[0].VegaTime,
		AccountID: marginLevels[0].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketNoCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 6)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[2],
		marginLevels[5],
		marginLevels[6],
		marginLevels[8],
		marginLevels[11],
		marginLevels[13],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[2].VegaTime,
		AccountID: marginLevels[2].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[13].VegaTime,
		AccountID: marginLevels[13].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketNoCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 6)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[13],
		marginLevels[11],
		marginLevels[8],
		marginLevels[6],
		marginLevels[5],
		marginLevels[2],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[13].VegaTime,
		AccountID: marginLevels[13].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[2].VegaTime,
		AccountID: marginLevels[2].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyFirstNoAfterCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	pagination, err := entities.NewCursorPagination(&first, nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[0],
		marginLevels[1],
		marginLevels[4],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[0].VegaTime,
		AccountID: marginLevels[0].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[4].VegaTime,
		AccountID: marginLevels[4].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyFirstNoAfterCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	pagination, err := entities.NewCursorPagination(&first, nil, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[14],
		marginLevels[10],
		marginLevels[7],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[14].VegaTime,
		AccountID: marginLevels[14].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[7].VegaTime,
		AccountID: marginLevels[7].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketFirstNoAfterCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	pagination, err := entities.NewCursorPagination(&first, nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[2],
		marginLevels[5],
		marginLevels[6],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[2].VegaTime,
		AccountID: marginLevels[2].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[6].VegaTime,
		AccountID: marginLevels[6].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketFirstNoAfterCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	pagination, err := entities.NewCursorPagination(&first, nil, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[13],
		marginLevels[11],
		marginLevels[8],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[13].VegaTime,
		AccountID: marginLevels[13].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[8].VegaTime,
		AccountID: marginLevels[8].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyLastNoBeforeCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	pagination, err := entities.NewCursorPagination(nil, nil, &last, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[7],
		marginLevels[10],
		marginLevels[14],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[7].VegaTime,
		AccountID: marginLevels[7].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[14].VegaTime,
		AccountID: marginLevels[14].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyLastNoBeforeCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	pagination, err := entities.NewCursorPagination(nil, nil, &last, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[4],
		marginLevels[1],
		marginLevels[0],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[4].VegaTime,
		AccountID: marginLevels[4].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[0].VegaTime,
		AccountID: marginLevels[0].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketLastNoBeforeCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	pagination, err := entities.NewCursorPagination(nil, nil, &last, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[8],
		marginLevels[11],
		marginLevels[13],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[8].VegaTime,
		AccountID: marginLevels[8].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[13].VegaTime,
		AccountID: marginLevels[13].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketLastNoBeforeCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	pagination, err := entities.NewCursorPagination(nil, nil, &last, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[6],
		marginLevels[5],
		marginLevels[2],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[6].VegaTime,
		AccountID: marginLevels[6].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[2].VegaTime,
		AccountID: marginLevels[2].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyFirstAndAfterCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	after := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[1].VegaTime,
		AccountID: marginLevels[1].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(&first, &after, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[4],
		marginLevels[7],
		marginLevels[10],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[4].VegaTime,
		AccountID: marginLevels[4].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[10].VegaTime,
		AccountID: marginLevels[10].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyFirstAndAfterCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	after := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[10].VegaTime,
		AccountID: marginLevels[10].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(&first, &after, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[7],
		marginLevels[4],
		marginLevels[1],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[7].VegaTime,
		AccountID: marginLevels[7].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[1].VegaTime,
		AccountID: marginLevels[1].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketFirstAndAfterCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	after := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[5].VegaTime,
		AccountID: marginLevels[5].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(&first, &after, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[6],
		marginLevels[8],
		marginLevels[11],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[6].VegaTime,
		AccountID: marginLevels[6].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[11].VegaTime,
		AccountID: marginLevels[11].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketFirstAndAfterCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	first := int32(3)
	after := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[11].VegaTime,
		AccountID: marginLevels[11].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(&first, &after, nil, nil, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[8],
		marginLevels[6],
		marginLevels[5],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[8].VegaTime,
		AccountID: marginLevels[8].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[5].VegaTime,
		AccountID: marginLevels[5].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyLastAndBeforeCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	before := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[10].VegaTime,
		AccountID: marginLevels[10].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(nil, nil, &last, &before, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[1],
		marginLevels[4],
		marginLevels[7],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[1].VegaTime,
		AccountID: marginLevels[1].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[7].VegaTime,
		AccountID: marginLevels[7].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithPartyLastAndBeforeCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	before := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[1].VegaTime,
		AccountID: marginLevels[1].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(nil, nil, &last, &before, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "DEADBEEF", "", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[10],
		marginLevels[7],
		marginLevels[4],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[10].VegaTime,
		AccountID: marginLevels[10].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[4].VegaTime,
		AccountID: marginLevels[4].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketLastAndBeforeCursor(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	before := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[11].VegaTime,
		AccountID: marginLevels[11].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(nil, nil, &last, &before, false)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[5],
		marginLevels[6],
		marginLevels[8],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[5].VegaTime,
		AccountID: marginLevels[5].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[8].VegaTime,
		AccountID: marginLevels[8].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}

func testGetMarginLevelsByIDPaginationWithMarketLastAndBeforeCursorNewestFirst(t *testing.T) {
	ctx := tempTransaction(t)

	t.Logf("DB Port: %d", testDBPort)
	mls, blocks, marginLevels := populateMarginLevelPaginationTestData(t, ctx)
	last := int32(3)
	before := entities.NewCursor(entities.MarginCursor{
		VegaTime:  blocks[5].VegaTime,
		AccountID: marginLevels[5].AccountID,
	}.String()).Encode()
	pagination, err := entities.NewCursorPagination(nil, nil, &last, &before, true)
	require.NoError(t, err)
	got, pageInfo, err := mls.GetMarginLevelsByIDWithCursorPagination(ctx, "", "DEADBEEF", pagination)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	wantMarginLevels := []entities.MarginLevels{
		marginLevels[11],
		marginLevels[8],
		marginLevels[6],
	}
	assert.Equal(t, wantMarginLevels, got)
	wantStartCursor := entities.MarginCursor{
		VegaTime:  blocks[11].VegaTime,
		AccountID: marginLevels[11].AccountID,
	}
	wantEndCursor := entities.MarginCursor{
		VegaTime:  blocks[6].VegaTime,
		AccountID: marginLevels[6].AccountID,
	}
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: true,
		StartCursor:     entities.NewCursor(wantStartCursor.String()).Encode(),
		EndCursor:       entities.NewCursor(wantEndCursor.String()).Encode(),
	}, pageInfo)
}
