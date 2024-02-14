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
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/sqlstore"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/libs/ptr"

	"github.com/georgysavva/scany/pgxscan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMMPool_Upsert(t *testing.T) {
	ctx := tempTransaction(t)

	bs := sqlstore.NewBlocks(connectionSource)
	ps := sqlstore.NewAMMPools(connectionSource)
	block := addTestBlock(t, ctx, bs)

	partyID := entities.PartyID(GenerateID())
	marketID := entities.MarketID(GenerateID())
	poolID := entities.AMMPoolID(GenerateID())
	subAccount := "subAccount"

	t.Run("Upsert statuses", func(t *testing.T) {
		upsertTests := []struct {
			Status entities.AMMPoolStatus
			Reason entities.AMMPoolStatusReason
		}{
			{entities.AMMPoolStatusActive, entities.AMMPoolStatusReasonUnspecified},
			{entities.AMMPoolStatusStopped, entities.AMMPoolStatusReasonUnspecified},
			{entities.AMMPoolStatusCancelled, entities.AMMPoolStatusReasonUnspecified},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonCancelledByParty},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonCannotRebase},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonMarketClosed},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonCannotFillCommitment},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonCommitmentTooLow},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonPartyAlreadyOwnsAPool},
			{entities.AMMPoolStatusRejected, entities.AMMPoolStatusReasonPartyClosedOut},
		}

		upsertTime := block.VegaTime
		for i, test := range upsertTests {
			upsertTime = upsertTime.Add(time.Duration(i) * time.Minute)
			pool := entities.AMMPool{
				PartyID:                           partyID,
				MarketID:                          marketID,
				PoolID:                            poolID,
				SubAccount:                        subAccount,
				Commitment:                        num.DecimalFromInt64(100),
				Status:                            test.Status,
				StatusReason:                      test.Reason,
				ParametersBase:                    num.DecimalFromInt64(100),
				ParametersLowerBound:              num.DecimalFromInt64(100),
				ParametersUpperBound:              num.DecimalFromInt64(100),
				ParametersMarginRatioAtLowerBound: num.DecimalFromInt64(100),
				ParametersMarginRatioAtUpperBound: num.DecimalFromInt64(100),
				CreatedAt:                         block.VegaTime,
				LastUpdated:                       upsertTime,
			}
			require.NoError(t, ps.Upsert(ctx, pool))
			var upserted entities.AMMPool
			require.NoError(t, pgxscan.Get(
				ctx,
				connectionSource.Connection,
				&upserted,
				`SELECT * FROM amm_pool WHERE party_id = $1 AND market_id = $2 AND pool_id = $3 AND sub_account = $4`,
				partyID, marketID, poolID, subAccount))
			assert.Equal(t, pool, upserted)
		}
	})

	t.Run("Upsert with different commitments and bounds", func(t *testing.T) {
		amounts := []num.Decimal{
			num.DecimalFromInt64(100),
			num.DecimalFromInt64(200),
			num.DecimalFromInt64(300),
		}
		upsertTime := block.VegaTime
		for i, amount := range amounts {
			upsertTime = upsertTime.Add(time.Duration(i) * time.Minute)
			pool := entities.AMMPool{
				PartyID:                           partyID,
				MarketID:                          marketID,
				PoolID:                            poolID,
				SubAccount:                        subAccount,
				Commitment:                        amount,
				Status:                            entities.AMMPoolStatusActive,
				StatusReason:                      entities.AMMPoolStatusReasonUnspecified,
				ParametersBase:                    amount,
				ParametersLowerBound:              amount,
				ParametersUpperBound:              amount,
				ParametersMarginRatioAtLowerBound: amount,
				ParametersMarginRatioAtUpperBound: amount,
				CreatedAt:                         block.VegaTime,
				LastUpdated:                       upsertTime,
			}
			require.NoError(t, ps.Upsert(ctx, pool))
			var upserted entities.AMMPool
			require.NoError(t, pgxscan.Get(
				ctx,
				connectionSource.Connection,
				&upserted,
				`SELECT * FROM amm_pool WHERE party_id = $1 AND market_id = $2 AND pool_id = $3 AND sub_account = $4`,
				partyID, marketID, poolID, subAccount))
			assert.Equal(t, pool, upserted)
		}
	})
}

type poolSubAccounts struct {
	PoolID     entities.AMMPoolID
	SubAccount string
}

func setupAMMPoolsTest(ctx context.Context, t *testing.T) (
	*sqlstore.AMMPools, []entities.AMMPool, []entities.PartyID, []entities.MarketID, []poolSubAccounts,
) {
	t.Helper()
	const (
		partyCount      = 5
		marketCount     = 5
		poolCount       = 5
		subAccountCount = 3
	)

	bs := sqlstore.NewBlocks(connectionSource)
	ps := sqlstore.NewAMMPools(connectionSource)

	block := addTestBlock(t, tempTransaction(t), bs)

	pools := make([]entities.AMMPool, 0, partyCount*marketCount*poolCount*subAccountCount)
	parties := make([]entities.PartyID, 0, partyCount)
	markets := make([]entities.MarketID, 0, marketCount)
	accounts := make([]poolSubAccounts, 0, poolCount*subAccountCount)

	for i := 0; i < partyCount; i++ {
		partyID := entities.PartyID(GenerateID())
		parties = append(parties, partyID)
		for j := 0; j < marketCount; j++ {
			marketID := entities.MarketID(GenerateID())
			markets = append(markets, marketID)
			for k := 0; k < poolCount; k++ {
				poolID := entities.AMMPoolID(GenerateID())
				for l := 0; l < subAccountCount; l++ {
					subAccount := fmt.Sprintf("subAccount%d", l+1)
					accounts = append(accounts, poolSubAccounts{PoolID: poolID, SubAccount: subAccount})

					pool := entities.AMMPool{
						PartyID:                           partyID,
						MarketID:                          marketID,
						PoolID:                            poolID,
						SubAccount:                        subAccount,
						Commitment:                        num.DecimalFromInt64(100),
						Status:                            entities.AMMPoolStatusActive,
						StatusReason:                      entities.AMMPoolStatusReasonUnspecified,
						ParametersBase:                    num.DecimalFromInt64(100),
						ParametersLowerBound:              num.DecimalFromInt64(100),
						ParametersUpperBound:              num.DecimalFromInt64(100),
						ParametersMarginRatioAtLowerBound: num.DecimalFromInt64(100),
						ParametersMarginRatioAtUpperBound: num.DecimalFromInt64(100),
						CreatedAt:                         block.VegaTime,
						LastUpdated:                       block.VegaTime,
					}
					require.NoError(t, ps.Upsert(ctx, pool))
					pools = append(pools, pool)
				}
			}
		}
	}

	sort.Slice(pools, func(i, j int) bool {
		return pools[i].CreatedAt.After(pools[j].CreatedAt) ||
			(pools[i].CreatedAt == pools[j].CreatedAt && pools[i].PoolID < pools[j].PoolID) ||
			(pools[i].CreatedAt == pools[j].CreatedAt && pools[i].PoolID == pools[j].PoolID && pools[i].SubAccount < pools[j].SubAccount)
	})

	return ps, pools, parties, markets, accounts
}

func TestAMMPools_ListAll(t *testing.T) {
	ctx := tempTransaction(t)

	ps, pools, _, _, _ := setupAMMPoolsTest(ctx, t)

	t.Run("Should return all pools if no pagination is provided", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListAll(ctx, pagination)
		require.NoError(t, err)
		assert.Equal(t, len(pools), len(listedPools))
		assert.Equal(t, pools, listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: false,
			StartCursor:     pools[0].Cursor().Encode(),
			EndCursor:       pools[len(pools)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the first page of pools", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListAll(ctx, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, pools[:5], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: false,
			StartCursor:     pools[0].Cursor().Encode(),
			EndCursor:       pools[4].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the last page of pools", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListAll(ctx, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, pools[len(pools)-5:], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: true,
			StartCursor:     pools[len(pools)-5].Cursor().Encode(),
			EndCursor:       pools[len(pools)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the requested page when paging forward", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), ptr.From(pools[100].Cursor().Encode()), nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListAll(ctx, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, pools[101:106], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     pools[101].Cursor().Encode(),
			EndCursor:       pools[105].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the request page when paging backward", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), ptr.From(pools[100].Cursor().Encode()), true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListAll(ctx, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, pools[95:100], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     pools[95].Cursor().Encode(),
			EndCursor:       pools[99].Cursor().Encode(),
		}, pageInfo)
	})
}

func filterPools(pools []entities.AMMPool, filter func(entities.AMMPool) bool) []entities.AMMPool {
	filtered := make([]entities.AMMPool, 0, len(pools))
	for _, pool := range pools {
		if filter(pool) {
			filtered = append(filtered, pool)
		}
	}
	return filtered
}

func TestAMMPools_ListByMarket(t *testing.T) {
	ctx := tempTransaction(t)

	ps, pools, _, markets, _ := setupAMMPoolsTest(ctx, t)
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	n := len(markets)

	t.Run("Should return all pools if no pagination is provided", func(t *testing.T) {
		// Randomly pick a market
		market := markets[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.MarketID == market
		})
		pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByMarket(ctx, market, pagination)
		require.NoError(t, err)
		assert.Equal(t, len(want), len(listedPools))
		assert.Equal(t, want, listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the first page of pools", func(t *testing.T) {
		// Randomly pick a market
		market := markets[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.MarketID == market
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByMarket(ctx, market, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[:5], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[4].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the last page of pools", func(t *testing.T) {
		// Randomly pick a market
		market := markets[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.MarketID == market
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByMarket(ctx, market, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[len(want)-5:], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: true,
			StartCursor:     want[len(want)-5].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the requested page when paging forward", func(t *testing.T) {
		// Randomly pick a market
		market := markets[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.MarketID == market
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), ptr.From(want[5].Cursor().Encode()), nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByMarket(ctx, market, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[6:11], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[6].Cursor().Encode(),
			EndCursor:       want[10].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the request page when paging backward", func(t *testing.T) {
		// Randomly pick a market
		market := markets[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.MarketID == market
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), ptr.From(want[10].Cursor().Encode()), true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByMarket(ctx, market, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[5:10], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[5].Cursor().Encode(),
			EndCursor:       want[9].Cursor().Encode(),
		}, pageInfo)
	})
}

func TestAMMPools_ListByParty(t *testing.T) {
	ctx := tempTransaction(t)

	ps, pools, parties, _, _ := setupAMMPoolsTest(ctx, t)
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	n := len(parties)

	t.Run("Should return all pools if no pagination is provided", func(t *testing.T) {
		// Randomly pick a party
		party := parties[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PartyID == party
		})
		pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByParty(ctx, party, pagination)
		require.NoError(t, err)
		assert.Equal(t, len(want), len(listedPools))
		assert.Equal(t, want, listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the first page of pools", func(t *testing.T) {
		// Randomly pick a party
		party := parties[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PartyID == party
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByParty(ctx, party, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[:5], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[4].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the last page of pools", func(t *testing.T) {
		// Randomly pick a party
		party := parties[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PartyID == party
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByParty(ctx, party, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[len(want)-5:], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: true,
			StartCursor:     want[len(want)-5].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the requested page when paging forward", func(t *testing.T) {
		// Randomly pick a party
		party := parties[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PartyID == party
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(5)), ptr.From(want[10].Cursor().Encode()), nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByParty(ctx, party, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[11:16], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[11].Cursor().Encode(),
			EndCursor:       want[15].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the request page when paging backward", func(t *testing.T) {
		// Randomly pick a party
		party := parties[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PartyID == party
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(5)), ptr.From(want[10].Cursor().Encode()), true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByParty(ctx, party, pagination)
		require.NoError(t, err)
		assert.Equal(t, 5, len(listedPools))
		assert.Equal(t, want[5:10], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[5].Cursor().Encode(),
			EndCursor:       want[9].Cursor().Encode(),
		}, pageInfo)
	})
}

func TestAMMPools_ListByPool(t *testing.T) {
	ctx := tempTransaction(t)

	ps, pools, _, _, poolAccounts := setupAMMPoolsTest(ctx, t)
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	n := len(poolAccounts)

	t.Run("Should return all pools if no pagination is provided", func(t *testing.T) {
		pa := poolAccounts[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PoolID == pa.PoolID && pool.SubAccount == pa.SubAccount
		})
		pagination, err := entities.NewCursorPagination(nil, nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByPool(ctx, pa.PoolID, &pa.SubAccount, pagination)
		require.NoError(t, err)
		assert.Equal(t, len(want), len(listedPools))
		assert.Equal(t, want, listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the first page of pools", func(t *testing.T) {
		pa := poolAccounts[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PoolID == pa.PoolID
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(2)), nil, nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByPool(ctx, pa.PoolID, nil, pagination)
		require.NoError(t, err)
		assert.Equal(t, 2, len(listedPools))
		assert.Equal(t, want[:2], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the last page of pools", func(t *testing.T) {
		pa := poolAccounts[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PoolID == pa.PoolID
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(2)), nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByPool(ctx, pa.PoolID, nil, pagination)
		require.NoError(t, err)
		assert.Equal(t, 2, len(listedPools))
		assert.Equal(t, want[len(want)-2:], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: true,
			StartCursor:     want[len(want)-2].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the requested page when paging forward", func(t *testing.T) {
		pa := poolAccounts[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PoolID == pa.PoolID
		})
		pagination, err := entities.NewCursorPagination(ptr.From(int32(2)), ptr.From(want[0].Cursor().Encode()), nil, nil, true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByPool(ctx, pa.PoolID, nil, pagination)
		require.NoError(t, err)
		assert.Equal(t, 2, len(listedPools))
		assert.Equal(t, want[1:3], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     false, // because there's only 3 records so we're actually at the end of the results
			HasPreviousPage: true,
			StartCursor:     want[1].Cursor().Encode(),
			EndCursor:       want[2].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("Should return the request page when paging backward", func(t *testing.T) {
		pa := poolAccounts[r.Intn(n)]
		want := filterPools(pools, func(pool entities.AMMPool) bool {
			return pool.PoolID == pa.PoolID
		})
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(2)), ptr.From(want[2].Cursor().Encode()), true)
		require.NoError(t, err)
		listedPools, pageInfo, err := ps.ListByPool(ctx, pa.PoolID, nil, pagination)
		require.NoError(t, err)
		assert.Equal(t, 2, len(listedPools))
		assert.Equal(t, want[:2], listedPools)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: false,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[1].Cursor().Encode(),
		}, pageInfo)
	})
}
