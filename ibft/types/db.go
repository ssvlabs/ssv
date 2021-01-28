package types

// DB is an interface for persisting chain data
type DB interface {
	SavePrepareJustification(lambda []byte, round uint64, msg *Message, signature []byte, signers []uint64)
	SaveDecidedRound(lambda []byte, msg *Message, signature []byte, signers []uint64)
}
