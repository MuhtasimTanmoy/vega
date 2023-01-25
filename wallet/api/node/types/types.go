package types

import "fmt"

type TransactionError struct {
	ABCICode uint32
	Message  string
}

func (e TransactionError) Error() string {
	return fmt.Sprintf("%s (ABCI code %d)", e.Message, e.ABCICode)
}

func (e TransactionError) Is(target error) bool {
	_, ok := target.(TransactionError)
	return ok
}

type Statistics struct {
	BlockHash   string
	BlockHeight uint64
	ChainID     string
	VegaTime    string
}

type SpamStatistics struct {
	ChainID           string
	Proposals         SpamStatistic
	Delegations       SpamStatistic
	Transfers         SpamStatistic
	NodeAnnouncements SpamStatistic
	Votes             VoteSpamStatistics
	PoW               PoWStatistics // TODO change
}

type SpamStatistic struct {
	CountForEpoch uint64
	MaxForEpoch   uint64
	BannedUntil   *string
}

type VoteSpamStatistics struct {
	Proposals   map[string]uint64
	MaxForEpoch uint64
	BannedUntil *string
}

type PoWStatistics struct {
	PowBlockStates []PoWBlockState
	BannedUntil    *string
}

type PoWBlockState struct {
	BlockHeight        uint64
	BlockHash          string
	TransactionsSeen   uint64
	ExpectedDifficulty *uint64
	HashFunction       string
}

type LastBlock struct {
	ChainID                         string
	BlockHeight                     uint64
	BlockHash                       string
	ProofOfWorkHashFunction         string
	ProofOfWorkDifficulty           uint32
	ProofOfWorkPastBlocks           uint32
	ProofOfWorkTxPerBlock           uint32
	ProofOfWorkIncreasingDifficulty bool
}
