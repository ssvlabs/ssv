package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. The phase0 --include is resolved from the module
// graph (`go list -m`), so it tracks go-eth2-client across dependency bumps rather than pinning.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./beacon_vote.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0 --objs GloasBeaconVote"

// GloasBeaconVote is the Gloas (ePBS) variant of spectypes.BeaconVote — the value the
// committee runner agrees on for Gloas slots. It mirrors BeaconVote (BlockRoot +
// Source/Target checkpoints, 112 bytes) and appends the BN-supplied
// AttestationData.Index for a fixed 120-byte SSZ encoding (field order per SIP #94).
// The 120B-vs-112B length difference makes a cross-fork decode fail cleanly.
//
// Not yet wired: this is the foundation for the Gloas committee runner (the §2 attestation track);
// the PTC slice doesn't use it. Tracked so it isn't mistaken for dead code.
type GloasBeaconVote struct {
	BlockRoot            phase0.Root `ssz-size:"32"`
	Source               *phase0.Checkpoint
	Target               *phase0.Checkpoint
	AttestationDataIndex phase0.CommitteeIndex
}

// Encode returns the SSZ-encoded GloasBeaconVote.
func (g *GloasBeaconVote) Encode() ([]byte, error) {
	return g.MarshalSSZ()
}

// Decode reads an SSZ-encoded GloasBeaconVote.
func (g *GloasBeaconVote) Decode(data []byte) error {
	return g.UnmarshalSSZ(data)
}
