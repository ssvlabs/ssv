package migrations

//go:generate sszgen -path ./migration_6_model.go --objs migration_6_OldStorageShare

const addressLength = 20

type migration_6_OldStorageOperator struct {
	OperatorID uint64
	PubKey     []byte `ssz-max:"48"`
}

type migration_6_OldStorageShare struct {
	ValidatorIndex        uint64
	ValidatorPubKey       []byte                            `ssz-size:"48"`
	SharePubKey           []byte                            `ssz-max:"48"` // empty for not own shares
	Committee             []*migration_6_OldStorageOperator `ssz-max:"13"`
	Quorum, PartialQuorum uint64
	DomainType            [4]byte `ssz-size:"4"`
	FeeRecipientAddress   [addressLength]byte
	Graffiti              []byte `ssz-max:"32"`

	Status          uint64
	ActivationEpoch uint64
	OwnerAddress    [addressLength]byte
	Liquidated      bool
}
