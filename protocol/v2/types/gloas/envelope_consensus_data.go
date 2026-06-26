package gloas

import (
	"github.com/attestantio/go-eth2-client/spec"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Regenerate with `go generate ./...`. ValidatorDuty resolves from ssv-spec and DataVersion from
// go-eth2-client/spec; both are tracked via the module graph (`go list -m`) rather than pinned.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./envelope_consensus_data.go --include $(go list -m -f '{{.Dir}}' github.com/ssvlabs/ssv-spec)/types,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0 --objs EnvelopeConsensusData"

// EnvelopeConsensusData is the §6 QBFT value for the envelope-signing duty (SIP #94 §6). It shares
// spectypes.ProposerConsensusData's wire shape (Duty + Version + DataSSZ) but is a distinct type so the
// envelope path reads as its own role rather than borrowing the proposer's. DataSSZ carries the
// SSZ-encoded BlindedExecutionPayloadEnvelope.
type EnvelopeConsensusData struct {
	Duty    spectypes.ValidatorDuty
	Version spec.DataVersion
	DataSSZ []byte `ssz-max:"8388608"`
}

// Encode/Decode wrap SSZ (de)serialization — the form the §6 QBFT instance agrees on.
func (e *EnvelopeConsensusData) Encode() ([]byte, error)  { return e.MarshalSSZ() }
func (e *EnvelopeConsensusData) Decode(data []byte) error { return e.UnmarshalSSZ(data) }
