package migrations

//go:generate go tool -modfile=../tool.mod sszgen -path ./migration_9_model.go --objs migration_9_CommitteeDutyTraceV1

type migration_9_ConsensusTrace struct {
	Rounds   []*migration_9_RoundTrace   `ssz-max:"15"`
	Decideds []*migration_9_DecidedTrace `ssz-max:"256"`
}

type migration_9_DecidedTrace struct {
	Round        uint64
	BeaconRoot   [32]byte `ssz-size:"32"`
	Signers      []uint64 `ssz-max:"13"`
	ReceivedTime uint64
}

type migration_9_RoundTrace struct {
	Proposer      uint64
	ProposalTrace *migration_9_ProposalTrace

	Prepares     []*migration_9_QBFTTrace        `ssz-max:"13"`
	Commits      []*migration_9_QBFTTrace        `ssz-max:"13"`
	RoundChanges []*migration_9_RoundChangeTrace `ssz-max:"13"`
}

type migration_9_RoundChangeTrace struct {
	migration_9_QBFTTrace
	PreparedRound   uint64
	PrepareMessages []*migration_9_QBFTTrace `ssz-max:"13"`
}

type migration_9_ProposalTrace struct {
	migration_9_QBFTTrace
	RoundChanges    []*migration_9_RoundChangeTrace `ssz-max:"13"`
	PrepareMessages []*migration_9_QBFTTrace        `ssz-max:"13"`
}

type migration_9_QBFTTrace struct {
	Round        uint64
	BeaconRoot   [32]byte `ssz-size:"32"`
	Signer       uint64
	ReceivedTime uint64
}

type migration_9_SignerData struct {
	Signer       uint64
	ValidatorIdx []uint64 `ssz-max:"3000"`
	ReceivedTime uint64
}

// migration_9_CommitteeDutyTraceV1 matches the pre-role committee duty trace schema.
// It is used to decode legacy SSZ values during migration.
type migration_9_CommitteeDutyTraceV1 struct {
	migration_9_ConsensusTrace

	Slot uint64

	CommitteeID [32]byte `ssz-size:"32"`
	OperatorIDs []uint64 `ssz-max:"13"`

	ProposalData []byte `ssz-max:"4194532"`

	SyncCommittee []*migration_9_SignerData `ssz-max:"1512"`
	Attester      []*migration_9_SignerData `ssz-max:"1512"`
}
