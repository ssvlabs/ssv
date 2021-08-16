package proto

import "github.com/bloxapp/ssv/utils/threadsafe"

type State struct {
	Stage RoundState
	// lambda is an instance unique identifier, much like a block hash in a blockchain
	Lambda *threadsafe.SafeBytes
	// sequence number is an incremental number for each instance, much like a block number would be in a blockchain
	SeqNumber     uint64
	InputValue    *threadsafe.SafeBytes
	Round         uint64
	PreparedRound uint64
	PreparedValue *threadsafe.SafeBytes
}
