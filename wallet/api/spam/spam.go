package spam

import (
	"errors"
	"fmt"

	vgcrypto "code.vegaprotocol.io/vega/libs/crypto"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	walletpb "code.vegaprotocol.io/vega/protos/vega/wallet/v1"
	nodetypes "code.vegaprotocol.io/vega/wallet/api/node/types"
)

var (
	ErrPartyBanned       = errors.New("the pubkey is banned from submitting transaction due to network spam rules")
	ErrPartyBannedPoW    = errors.New("the pubkey is banned from submitting transaction due to network spam rules PoW")
	ErrPartyWillBeBanned = errors.New("submitting the transaction will cause the pubkey to be banned due to network spam rules")
)

// CheckSubmission return an error if we are banned from making this type of transaction or if submitting
// the transaction will result in a banning.
func CheckSubmission(req *walletpb.SubmitTransactionRequest, statistics nodetypes.SpamStatistics) error {
	fmt.Println(statistics)
	if statistics.PoW.BannedUntil != nil {
		return ErrPartyBannedPoW
	}

	var st nodetypes.SpamStatistic
	switch cmd := req.Command.(type) {
	case *walletpb.SubmitTransactionRequest_ProposalSubmission:
		st = statistics.Proposals
	case *walletpb.SubmitTransactionRequest_AnnounceNode:
		st = statistics.NodeAnnouncements
	case *walletpb.SubmitTransactionRequest_UndelegateSubmission, *walletpb.SubmitTransactionRequest_DelegateSubmission:
		st = statistics.Delegations
	case *walletpb.SubmitTransactionRequest_Transfer:
		st = statistics.Transfers
	case *walletpb.SubmitTransactionRequest_VoteSubmission:
		pID := cmd.VoteSubmission.ProposalId
		st = nodetypes.SpamStatistic{
			CountForEpoch: statistics.Votes.Proposals[pID],
			MaxForEpoch:   statistics.Votes.MaxForEpoch,
			BannedUntil:   statistics.Votes.BannedUntil,
		}
	default:
		return nil
	}

	if st.BannedUntil != nil {
		return ErrPartyBanned
	}

	if st.CountForEpoch == st.MaxForEpoch {
		return ErrPartyWillBeBanned
	}

	return nil
}

func GenerateProofOfWork(st nodetypes.PoWStatistics) (*commandspb.ProofOfWork, error) {
	// find the first block that we can use for proof-of-work, should already be ordered with
	// lastest block first
	var state nodetypes.PoWBlockState
	for _, s := range st.PowBlockStates {
		if s.ExpectedDifficulty != nil {
			state = s
			break
		}
	}

	if state.ExpectedDifficulty == nil {
		return nil, fmt.Errorf("couldn't find block we can use")
	}

	tid := vgcrypto.RandomHash()
	powNonce, _, err := vgcrypto.PoW(state.BlockHash, tid, uint(*state.ExpectedDifficulty), state.HashFunction)
	if err != nil {
		return nil, err
	}

	return &commandspb.ProofOfWork{
		Tid:   tid,
		Nonce: powNonce,
	}, nil
}
