package types

import (
	altair "github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
)

// DomainType is a unique identifier for signatures, 2 identical pieces of data signed with different domains will result in different sigs
type DomainType []byte

var (
	PrimusTestnet = DomainType("primus_testnet")
)

type SignatureType []byte

var (
	QBFTSignatureType    = []byte{1, 0, 0, 0}
	PartialSignatureType = []byte{2, 0, 0, 0}
)

type AttesterCalls interface {
	// SignAttestation signs the given attestation
	SignAttestation(data *spec.AttestationData, duty *Duty, pk []byte) (*spec.Attestation, []byte, error)
	// IsAttestationSlashable returns error if attestation is slashable
	IsAttestationSlashable(data *spec.AttestationData) error
}

type ProposerCalls interface {
	// SignRandaoReveal signs randao
	SignRandaoReveal(epoch spec.Epoch, pk []byte) (Signature, []byte, error)
	// IsBeaconBlockSlashable returns true if the given block is slashable
	IsBeaconBlockSlashable(block *altair.BeaconBlock) error
	// SignBeaconBlock signs the given beacon block
	SignBeaconBlock(block *altair.BeaconBlock, duty *Duty, pk []byte) (*altair.SignedBeaconBlock, []byte, error)
}

type AggregatorCalls interface {
	// SignSlotWithSelectionProof signs slot for aggregator selection proof
	SignSlotWithSelectionProof(slot spec.Slot, pk []byte) (Signature, []byte, error)
	// SignAggregateAndProof returns a signed aggregate and proof msg
	SignAggregateAndProof(msg *spec.AggregateAndProof, duty *Duty, pk []byte) (*spec.SignedAggregateAndProof, []byte, error)
}

type SyncCommitteeCalls interface {
	// SignSyncCommitteeBlockRoot returns a signed sync committee msg
	SignSyncCommitteeBlockRoot(slot spec.Slot, root spec.Root, validatorIndex spec.ValidatorIndex, pk []byte) (*altair.SyncCommitteeMessage, []byte, error)
}

type SyncCommitteeContributionCalls interface {
	// SignContributionProof signs contribution proof
	SignContributionProof(slot spec.Slot, index uint64, pk []byte) (Signature, []byte, error)
	// SignContribution signs a SyncCommitteeContribution
	SignContribution(contribution *altair.ContributionAndProof, pk []byte) (*altair.SignedContributionAndProof, []byte, error)
}

type BeaconSigner interface {
	AttesterCalls
	ProposerCalls
	AggregatorCalls
	SyncCommitteeCalls
	SyncCommitteeContributionCalls
}

// SSVSigner used for all SSV specific signing
type SSVSigner interface {
	SignRoot(data Root, sigType SignatureType, pk []byte) (Signature, error)
}

// KeyManager is an interface responsible for all key manager functions
type KeyManager interface {
	BeaconSigner
	SSVSigner
	// AddShare saves a share key
	AddShare(shareKey *bls.SecretKey) error
}
