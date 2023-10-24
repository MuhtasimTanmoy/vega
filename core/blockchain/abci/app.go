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

package abci

import (
	"context"

	"code.vegaprotocol.io/vega/core/blockchain"
	"code.vegaprotocol.io/vega/core/txn"
	"code.vegaprotocol.io/vega/core/types"

	abci "github.com/cometbft/cometbft/abci/types"
	lru "github.com/hashicorp/golang-lru"
)

type (
	Command   byte
	TxHandler func(ctx context.Context, tx Tx) error
)

type SnapshotEngine interface {
	AddProviders(provs ...types.StateProvider)
}

type App struct {
	abci.BaseApplication
	codec Codec

	// handlers
	OnPrepareProposal PrepareProposalHandler
	OnProcessProposal ProcessProposalHandler
	OnInitChain       OnInitChainHandler
	OnCheckTx         OnCheckTxHandler
	OnCommit          OnCommitHandler
	OnBeginBlock      OnBeginBlockHandler
	OnEndBlock        OnEndBlockHandler
	OnFinalize        FinalizeHandler

	// spam check
	OnCheckTxSpam OnCheckTxSpamHandler

	// snapshot stuff

	OnListSnapshots      ListSnapshotsHandler
	OnOfferSnapshot      OfferSnapshotHandler
	OnLoadSnapshotChunk  LoadSnapshotChunkHandler
	OnApplySnapshotChunk ApplySnapshotChunkHandler
	OnInfo               InfoHandler

	// These are Tx handlers
	checkTxs   map[txn.Command]TxHandler
	deliverTxs map[txn.Command]TxHandler

	// checkedTxs holds a map of valid transactions (validated by CheckTx)
	// They are consumed by DeliverTx to avoid double validation.
	checkedTxs *lru.Cache // map[string]Transaction

	// the current block context
	ctx context.Context

	chainID string
}

func New(codec Codec) *App {
	lruCache, _ := lru.New(1024)
	return &App{
		codec:      codec,
		checkTxs:   map[txn.Command]TxHandler{},
		deliverTxs: map[txn.Command]TxHandler{},
		checkedTxs: lruCache,
		ctx:        context.Background(),
	}
}

func (app *App) SetChainID(chainID string) {
	app.chainID = chainID
}

func (app *App) GetChainID() string {
	return app.chainID
}

func (app *App) HandleCheckTx(cmd txn.Command, fn TxHandler) *App {
	app.checkTxs[cmd] = fn
	return app
}

func (app *App) HandleDeliverTx(cmd txn.Command, fn TxHandler) *App {
	app.deliverTxs[cmd] = fn
	return app
}

func (app *App) decodeTx(bytes []byte) (Tx, uint32, error) {
	tx, err := app.codec.Decode(bytes, app.chainID)
	if err != nil {
		return nil, blockchain.AbciTxnDecodingFailure, err
	}
	return tx, 0, nil
}

// cacheTx adds a Tx to the cache.
func (app *App) cacheTx(in []byte, tx Tx) {
	app.checkedTxs.Add(string(in), tx)
}

// txFromCache retrieves (and remove if found) a Tx from the cache,
// it returns the Tx or nil if not found.
func (app *App) txFromCache(in []byte) Tx {
	tx, ok := app.checkedTxs.Get(string(in))
	if !ok {
		return nil
	}

	return tx.(Tx)
}

func (app *App) removeTxFromCache(in []byte) {
	app.checkedTxs.Remove(string(in))
}

// getTx returns an internal Tx given a []byte.
// An error code different from 0 is returned if decoding  fails with its the corresponding error.
func (app *App) getTx(bytes []byte) (Tx, uint32, error) {
	if tx := app.txFromCache(bytes); tx != nil {
		return tx, 0, nil
	}

	tx, code, err := app.decodeTx(bytes)
	if err != nil {
		return nil, code, err
	}

	return tx, 0, nil
}
