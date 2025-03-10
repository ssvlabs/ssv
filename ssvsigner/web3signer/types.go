package web3signer

import (
	"encoding/json"
	"errors"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type ListKeysResponse []phase0.BLSPubKey

type KeyData struct {
	ValidatingPubkey phase0.BLSPubKey `json:"validating_pubkey"`
}

type ImportKeystoreRequest struct {
	Keystores          []Keystore `json:"keystores"`
	Passwords          []string   `json:"passwords"`
	SlashingProtection string     `json:"slashing_protection,omitempty"`
}

type Keystore map[string]any

type ImportKeystoreResponse struct {
	Data    []KeyManagerResponseData `json:"data"`
	Message string                   `json:"message,omitempty"`
}

type DeleteKeystoreRequest struct {
	Pubkeys []phase0.BLSPubKey `json:"pubkeys"`
}

type DeleteKeystoreResponse struct {
	Data               []KeyManagerResponseData `json:"data"`
	SlashingProtection string                   `json:"slashing_protection"`
	Message            string                   `json:"message,omitempty"`
}

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

type SignRequest struct {
	ForkInfo                    ForkInfo                          `json:"fork_info"`
	SigningRoot                 phase0.Root                       `json:"signing_root,omitempty"`
	Type                        SignedObjectType                  `json:"type"`
	Attestation                 *phase0.AttestationData           `json:"attestation,omitempty"`
	BeaconBlock                 *BeaconBlockData                  `json:"beacon_block,omitempty"`
	VoluntaryExit               *phase0.VoluntaryExit             `json:"voluntary_exit,omitempty"`
	AggregateAndProof           *AggregateAndProof                `json:"aggregate_and_proof,omitempty"`
	AggregationSlot             *AggregationSlot                  `json:"aggregation_slot,omitempty"`
	RandaoReveal                *RandaoReveal                     `json:"randao_reveal,omitempty"`
	SyncCommitteeMessage        *SyncCommitteeMessage             `json:"sync_committee_message,omitempty"`
	SyncAggregatorSelectionData *SyncCommitteeAggregatorSelection `json:"sync_aggregator_selection_data,omitempty"`
	ContributionAndProof        *altair.ContributionAndProof      `json:"contribution_and_proof,omitempty"`
	ValidatorRegistration       *v1.ValidatorRegistration         `json:"validator_registration,omitempty"`
}

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
	Version     spec.DataVersion          `json:"version"`
	BlockHeader *phase0.BeaconBlockHeader `json:"block_header"`
}

// AggregateAndProof is a union of *phase0.AggregateAndProof or *electra.AggregateAndProof.
// If Electra is set, Phase0 is ignored.
type AggregateAndProof struct {
	Phase0  *phase0.AggregateAndProof
	Electra *electra.AggregateAndProof
}

func (ap *AggregateAndProof) MarshalJSON() ([]byte, error) {
	if ap == nil {
		return json.Marshal(nil)
	}

	if ap.Electra != nil {
		return json.Marshal(ap.Electra)
	}

	return json.Marshal(ap.Phase0)
}

func (ap *AggregateAndProof) UnmarshalJSON(data []byte) error {
	electraErr := json.Unmarshal(data, &ap.Electra)
	phase0Err := json.Unmarshal(data, &ap.Phase0)

	if electraErr != nil && phase0Err != nil {
		return errors.Join(electraErr, phase0Err)
	}

	return nil
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
