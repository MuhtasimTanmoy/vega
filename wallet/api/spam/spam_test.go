package spam_test

import (
	"testing"

	vgcrypto "code.vegaprotocol.io/vega/libs/crypto"
	"code.vegaprotocol.io/vega/libs/ptr"
	v1 "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	nodetypes "code.vegaprotocol.io/vega/wallet/api/node/types"
	"code.vegaprotocol.io/vega/wallet/api/spam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProofOfWorkGeneration(t *testing.T) {
	st := nodetypes.PoWStatistics{
		PowBlockStates: []nodetypes.PoWBlockState{
			{
				BlockHeight:        10,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: ptr.From(uint64(10)),
				HashFunction:       vgcrypto.Sha3,
			},
		},
	}

	pow, err := spam.GenerateProofOfWork(st)
	require.NoError(t, err)
	require.NotEmpty(t, pow)

	ok, _ := vgcrypto.Verify(st.PowBlockStates[0].BlockHash, pow.Tid, pow.Nonce, vgcrypto.Sha3, 10)
	assert.True(t, ok)
}

func TestProofOfWorkGenerationMaxOnLatest(t *testing.T) {
	st := nodetypes.PoWStatistics{
		PowBlockStates: []nodetypes.PoWBlockState{
			{
				BlockHeight:        10,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: nil, // we cannot submit pow against this block
				HashFunction:       vgcrypto.Sha3,
			},
			{
				BlockHeight:        5,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: ptr.From(uint64(10)),
				HashFunction:       vgcrypto.Sha3,
			},
			{
				BlockHeight:        4,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: nil,
				HashFunction:       vgcrypto.Sha3,
			},
		},
	}

	pow, err := spam.GenerateProofOfWork(st)
	require.NoError(t, err)
	require.NotEmpty(t, pow)

	ok, _ := vgcrypto.Verify(st.PowBlockStates[1].BlockHash, pow.Tid, pow.Nonce, vgcrypto.Sha3, 10)
	assert.True(t, ok)
}

func TestProofOfWorkGenerationNoBlocksAvailable(t *testing.T) {
	st := nodetypes.PoWStatistics{
		PowBlockStates: []nodetypes.PoWBlockState{
			{
				BlockHeight:        10,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: nil,
				HashFunction:       vgcrypto.Sha3,
			},
			{
				BlockHeight:        5,
				BlockHash:          vgcrypto.RandomHash(),
				ExpectedDifficulty: nil,
				HashFunction:       vgcrypto.Sha3,
			},
		},
	}

	pow, err := spam.GenerateProofOfWork(st)
	require.Error(t, err)
	require.Empty(t, pow)
}

func TestSpamCheckSubmissionBanned(t *testing.T) {
	banned := nodetypes.SpamStatistic{
		BannedUntil: ptr.From("hard ban"),
	}

	tcs := []struct {
		name  string
		req   *walletpb.SubmitTransactionRequest
		stats nodetypes.SpamStatistics
	}{
		{
			name: "proposal ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_ProposalSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Proposals: banned,
			},
		},
		{
			name: "delegation ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_DelegateSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Delegations: banned,
			},
		},
		{
			name: "undelegation ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_UndelegateSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Delegations: banned,
			},
		},
		{
			name: "transfer ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_Transfer{},
			},
			stats: nodetypes.SpamStatistics{
				Transfers: banned,
			},
		},
		{
			name: "node announcement ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_AnnounceNode{},
			},
			stats: nodetypes.SpamStatistics{
				NodeAnnouncements: banned,
			},
		},
		{
			name: "votes ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_VoteSubmission{
					VoteSubmission: &v1.VoteSubmission{
						ProposalId: vgcrypto.RandomHash(),
					},
				},
			},
			stats: nodetypes.SpamStatistics{
				Votes: nodetypes.VoteSpamStatistics{
					BannedUntil: ptr.From("hard ban"),
				},
			},
		},
		{
			name: "pow ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_OracleDataSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				PoW: nodetypes.PoWStatistics{
					BannedUntil: ptr.From("hard ban"),
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(tt *testing.T) {
			assert.ErrorIs(t, spam.CheckSubmission(tc.req, tc.stats), spam.ErrPartyBanned)
		})
	}
}

func TestSpamCheckSubmissionMaxCount(t *testing.T) {
	maxCount := nodetypes.SpamStatistic{
		MaxForEpoch:   10,
		CountForEpoch: 10,
	}

	tcs := []struct {
		name  string
		req   *walletpb.SubmitTransactionRequest
		stats nodetypes.SpamStatistics
	}{
		{
			name: "proposal max count",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_ProposalSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Proposals: maxCount,
			},
		},
		{
			name: "delegation max count",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_DelegateSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Delegations: maxCount,
			},
		},
		{
			name: "undelegation max count",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_UndelegateSubmission{},
			},
			stats: nodetypes.SpamStatistics{
				Delegations: maxCount,
			},
		},
		{
			name: "transfer max count",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_Transfer{},
			},
			stats: nodetypes.SpamStatistics{
				Transfers: maxCount,
			},
		},
		{
			name: "node announcement max count",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_AnnounceNode{},
			},
			stats: nodetypes.SpamStatistics{
				NodeAnnouncements: maxCount,
			},
		},
		{
			name: "votes ban",
			req: &walletpb.SubmitTransactionRequest{
				Command: &walletpb.SubmitTransactionRequest_VoteSubmission{
					VoteSubmission: &v1.VoteSubmission{
						ProposalId: "some-proposal-id",
					},
				},
			},
			stats: nodetypes.SpamStatistics{
				Votes: nodetypes.VoteSpamStatistics{
					MaxForEpoch: 100,
					Proposals: map[string]uint64{
						"some-proposal-id":    100,
						"some-other-proposal": 1,
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(tt *testing.T) {
			assert.ErrorIs(t, spam.CheckSubmission(tc.req, tc.stats), spam.ErrPartyWillBeBanned)
		})
	}
}
