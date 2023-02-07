// Copyright (c) 2022 Gobalsky Labs Limited
//
// Use of this software is governed by the Business Source License included
// in the LICENSE.DATANODE file and at https://www.mariadb.com/bsl11.
//
// Change Date: 18 months from the later of the date of the first publicly
// available Distribution of this version of the repository, and 25 June 2022.
//
// On the date above, in accordance with the Business Source License, use
// of this software will be governed by version 3 or later of the GNU General
// Public License.

package service

import (
	"context"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/utils"
	"code.vegaprotocol.io/vega/logging"

	lru "github.com/hashicorp/golang-lru/v2"
)

type PositionStore interface {
	Flush(ctx context.Context) ([]entities.Position, error)
	Add(ctx context.Context, p entities.Position) error
	GetByMarketAndParty(ctx context.Context, marketID string, partyID string) (entities.Position, error)
	GetByMarket(ctx context.Context, marketID string) ([]entities.Position, error)
	GetByParty(ctx context.Context, partyID string) ([]entities.Position, error)
	GetByPartyConnection(ctx context.Context, partyID []string, marketID []string, pagination entities.CursorPagination) ([]entities.Position, entities.PageInfo, error)
	GetAll(ctx context.Context) ([]entities.Position, error)
}

type positionCacheKey struct {
	MarketID entities.MarketID
	PartyID  entities.PartyID
}

type positionCacheValue struct {
	pos entities.Position
	err error
}
type Position struct {
	log      *logging.Logger
	store    PositionStore
	observer utils.Observer[entities.Position]
	cache    *lru.Cache[positionCacheKey, positionCacheValue]
}

func NewPosition(store PositionStore, log *logging.Logger) *Position {
	cache, err := lru.New[positionCacheKey, positionCacheValue](10000)
	if err != nil {
		panic(err)
	}
	return &Position{
		store:    store,
		log:      log,
		observer: utils.NewObserver[entities.Position]("positions", log, 0, 0),
		cache:    cache,
	}
}

func (p *Position) Flush(ctx context.Context) error {
	flushed, err := p.store.Flush(ctx)
	if err != nil {
		return err
	}
	p.observer.Notify(flushed)
	return nil
}

func (p *Position) Add(ctx context.Context, pos entities.Position) error {
	// It is a bit unorthodox to add values into the cache here, before flushing; but the current
	// design of the positions subscriber relies on this cache to be able to fetch up-to-date
	// positions in the middle of a block/transaction.
	// TODO: There is potentially an issue here if a position that is evicted from the LRU
	//       that has been updated in a block and then subsequently needed again; we would
	//       end up going to the database and getting an out of date version!
	key := positionCacheKey{pos.MarketID, pos.PartyID}
	value := positionCacheValue{pos, nil}
	p.cache.Add(key, value)
	return p.store.Add(ctx, pos)
}

func (p *Position) GetByMarketAndParty(ctx context.Context, marketID string, partyID string) (entities.Position, error) {
	key := positionCacheKey{entities.MarketID(marketID), entities.PartyID(partyID)}
	value, ok := p.cache.Get(key)
	if !ok {
		pos, err := p.store.GetByMarketAndParty(
			ctx, marketID, partyID)
		p.cache.Add(key, positionCacheValue{pos, err})
		return pos, err
	}

	return value.pos, value.err
}

func (p *Position) GetByMarket(ctx context.Context, marketID string) ([]entities.Position, error) {
	return p.store.GetByMarket(ctx, marketID)
}

func (p *Position) GetByParty(ctx context.Context, partyID entities.PartyID) ([]entities.Position, error) {
	return p.store.GetByParty(ctx, partyID.String())
}

func (p *Position) GetByPartyConnection(ctx context.Context, partyIDs []entities.PartyID, marketIDs []entities.MarketID, pagination entities.CursorPagination) ([]entities.Position, entities.PageInfo, error) {
	ps := make([]string, len(partyIDs))
	for i, p := range partyIDs {
		ps[i] = p.String()
	}

	ms := make([]string, len(marketIDs))
	for i, m := range marketIDs {
		ms[i] = m.String()
	}
	return p.store.GetByPartyConnection(ctx, ps, ms, pagination)
}

func (p *Position) GetAll(ctx context.Context) ([]entities.Position, error) {
	return p.store.GetAll(ctx)
}

func (p *Position) Observe(ctx context.Context, retries int, partyID, marketID string) (<-chan []entities.Position, uint64) {
	ch, ref := p.observer.Observe(ctx,
		retries,
		func(pos entities.Position) bool {
			return (len(marketID) == 0 || marketID == pos.MarketID.String()) &&
				(len(partyID) == 0 || partyID == pos.PartyID.String())
		})
	return ch, ref
}
