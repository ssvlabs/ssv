package web3signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type KeyManagerResponseData struct {
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type Status string

const (
	StatusImported   Status = "imported"
	StatusDuplicated Status = "duplicate"
	StatusDeleted    Status = "deleted"
	StatusNotActive  Status = "not_active"
	StatusNotFound   Status = "not_found"
	StatusError      Status = "error"
)

type ForkInfo struct {
	Fork                  *phase0.Fork `json:"fork"`
	GenesisValidatorsRoot phase0.Root  `json:"genesis_validators_root"`
}

type SignedObjectType string

const (
	TypeAggregationSlot                   SignedObjectType = "AGGREGATION_SLOT"
	TypeAggregateAndProof                 SignedObjectType = "AGGREGATE_AND_PROOF"
	TypeAggregateAndProofV2               SignedObjectType = "AGGREGATE_AND_PROOF_V2"
	TypeAttestation                       SignedObjectType = "ATTESTATION"
	TypeBlock                             SignedObjectType = "BLOCK"
	TypeBlockV2                           SignedObjectType = "BLOCK_V2"
	TypeDeposit                           SignedObjectType = "DEPOSIT"
	TypeRandaoReveal                      SignedObjectType = "RANDAO_REVEAL"
	TypeVoluntaryExit                     SignedObjectType = "VOLUNTARY_EXIT"
	TypeSyncCommitteeMessage              SignedObjectType = "SYNC_COMMITTEE_MESSAGE"
	TypeSyncCommitteeSelectionProof       SignedObjectType = "SYNC_COMMITTEE_SELECTION_PROOF"
	TypeSyncCommitteeContributionAndProof SignedObjectType = "SYNC_COMMITTEE_CONTRIBUTION_AND_PROOF"
	TypeValidatorRegistration             SignedObjectType = "VALIDATOR_REGISTRATION"

	// Gloas (ePBS) request types from remote-signing API v1.4.0 (Web3Signer 26.9.0+), each with a
	// VersionedRequest body.
	TypePayloadAttestationMessage SignedObjectType = "PAYLOAD_ATTESTATION_MESSAGE"
	TypeProposerPreferences       SignedObjectType = "PROPOSER_PREFERENCES"
	TypeBuilderRequestAuth        SignedObjectType = "BUILDER_REQUEST_AUTH"
)

// VersionedRequest is the {version, data} body of the newer remote-signing API request types: the Gloas
// ones, and AGGREGATE_AND_PROOF_V2 (see AggregateAndProof).
type VersionedRequest[T any] struct {
	Version DataVersion `json:"version"`
	Data    T           `json:"data"`
}

type BeaconBlockData struct {
	Version     DataVersion               `json:"version"`
	BlockHeader *phase0.BeaconBlockHeader `json:"block_header"`
}

type DataVersion spec.DataVersion

// MarshalJSON implements json.Marshaler. The value receiver makes a DataVersion field encode as the fork
// name even in a struct marshaled by value.
func (d DataVersion) MarshalJSON() ([]byte, error) {
	specDV := spec.DataVersion(d)

	b, err := specDV.MarshalJSON()
	if err != nil {
		return nil, err
	}

	return bytes.ToUpper(b), err
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *DataVersion) UnmarshalJSON(input []byte) error {
	loweredInput := bytes.ToLower(input)

	var specDV spec.DataVersion
	if err := specDV.UnmarshalJSON(loweredInput); err != nil {
		return err
	}

	*d = DataVersion(specDV)
	return nil
}

// AggregateAndProof is the aggregate_and_proof field, a union with at most one member set. Phase0 and
// Electra are sent as the legacy AGGREGATE_AND_PROOF body, the bare object, which older Web3Signer versions
// also accept. Gloas is sent as the AGGREGATE_AND_PROOF_V2 body, {version: GLOAS, data}, the remote-signing
// API's form for it. Either way Web3Signer takes the schema from the attestation's slot, so its fork
// schedule must match the chain's.
type AggregateAndProof struct {
	Phase0  *phase0.AggregateAndProof
	Electra *electra.AggregateAndProof
	Gloas   *gloas.AggregateAndProof
}

// RequestType returns the type of the request this body goes in: AGGREGATE_AND_PROOF_V2 for Gloas, the legacy
// AGGREGATE_AND_PROOF otherwise.
func (ap *AggregateAndProof) RequestType() SignedObjectType {
	if ap.Gloas != nil {
		return TypeAggregateAndProofV2
	}
	return TypeAggregateAndProof
}

func (ap *AggregateAndProof) MarshalJSON() ([]byte, error) {
	if ap == nil {
		return json.Marshal(nil)
	}

	set := 0
	for _, isSet := range []bool{ap.Phase0 != nil, ap.Electra != nil, ap.Gloas != nil} {
		if isSet {
			set++
		}
	}
	if set > 1 {
		return nil, errors.New("only one of phase0, electra and gloas can be set")
	}

	switch {
	case ap.Gloas != nil:
		// Fixed rather than the slot's fork: Web3Signer ignores the version and takes the schema from the
		// attestation's slot.
		return json.Marshal(&VersionedRequest[*gloas.AggregateAndProof]{
			Version: DataVersion(spec.DataVersionGloas),
			Data:    ap.Gloas,
		})
	case ap.Electra != nil:
		return json.Marshal(ap.Electra)
	case ap.Phase0 != nil:
		return json.Marshal(ap.Phase0)
	default:
		return json.Marshal(nil)
	}
}

func (ap *AggregateAndProof) UnmarshalJSON(data []byte) error {
	// A version field marks the AGGREGATE_AND_PROOF_V2 body, which only the Gloas member uses, and
	// committee_bits an Electra aggregate.
	var probe struct {
		Version   json.RawMessage `json:"version"`
		Aggregate struct {
			CommitteeBits json.RawMessage `json:"committee_bits"`
		} `json:"aggregate"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	switch {
	case probe.Version != nil:
		var versioned VersionedRequest[*gloas.AggregateAndProof]
		if err := json.Unmarshal(data, &versioned); err != nil {
			return err
		}
		if spec.DataVersion(versioned.Version) != spec.DataVersionGloas {
			return fmt.Errorf("unsupported versioned aggregate and proof: %s", spec.DataVersion(versioned.Version))
		}
		if versioned.Data == nil {
			return errors.New("versioned aggregate and proof has no data")
		}
		ap.Gloas = versioned.Data
		return nil
	case probe.Aggregate.CommitteeBits != nil:
		return json.Unmarshal(data, &ap.Electra)
	default:
		return json.Unmarshal(data, &ap.Phase0)
	}
}

type AggregationSlot struct {
	Slot phase0.Slot `json:"slot"`
}

type RandaoReveal struct {
	Epoch phase0.Epoch `json:"epoch"`
}

type SyncCommitteeMessage struct {
	BeaconBlockRoot phase0.Root `json:"beacon_block_root"`
	Slot            phase0.Slot `json:"slot"`
}

type SyncCommitteeAggregatorSelection struct {
	Slot              phase0.Slot           `json:"slot"`
	SubcommitteeIndex phase0.CommitteeIndex `json:"subcommittee_index"` // phase0.CommitteeIndex type to marshal to string
}

type ErrorMessage struct {
	Message string `json:"message"`
}

type HTTPResponseError struct {
	Err     error
	Status  int
	ErrText string
}

func (h HTTPResponseError) Error() string {
	return fmt.Sprintf("error status %d (%q): %s", h.Status, h.ErrText, h.Err.Error())
}

func (h HTTPResponseError) Unwrap() error {
	return h.Err
}
