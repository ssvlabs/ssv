package web3signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
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
)

type BeaconBlockData struct {
	Version     DataVersion               `json:"version"`
	BlockHeader *phase0.BeaconBlockHeader `json:"block_header"`
}

type DataVersion spec.DataVersion

// MarshalJSON implements json.Marshaler.
func (d *DataVersion) MarshalJSON() ([]byte, error) {
	specDV := spec.DataVersion(*d)

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

// AggregateAndProof is a union of *phase0.AggregateAndProof or *electra.AggregateAndProof.
// Setting both is not allowed.
type AggregateAndProof struct {
	Phase0  *phase0.AggregateAndProof
	Electra *electra.AggregateAndProof
}

func (ap *AggregateAndProof) MarshalJSON() ([]byte, error) {
	if ap == nil || ap.Phase0 == nil && ap.Electra == nil {
		return json.Marshal(nil)
	}

	if ap.Phase0 != nil && ap.Electra != nil {
		return nil, errors.New("both phase0 and electra cannot be set")
	}

	if ap.Electra != nil {
		return json.Marshal(ap.Electra)
	}

	return json.Marshal(ap.Phase0)
}

func (ap *AggregateAndProof) UnmarshalJSON(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if attestation, ok := m["aggregate"].(map[string]any); ok {
		if _, ok := attestation["committee_bits"]; ok {
			return json.Unmarshal(data, &ap.Electra)
		}
	}

	return json.Unmarshal(data, &ap.Phase0)
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
	Err    error
	Status int
}

func (h HTTPResponseError) Error() string {
	return fmt.Sprintf("error status %d: %s", h.Status, h.Err.Error())
}

func (h HTTPResponseError) Unwrap() error {
	return h.Err
}
