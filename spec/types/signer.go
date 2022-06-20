package types

import (
	"bytes"
	"crypto/rsa"
	altair "github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
)

// DomainType is a unique identifier for signatures, 2 identical pieces of data signed with different domains will result in different sigs
type DomainType []byte

var (
	PrimusTestnet = DomainType("primus_testnet")
)

type SignatureType [4]byte

func (sigType SignatureType) Equal(other SignatureType) bool {
	return bytes.Equal(sigType[:], other[:])
}

var (
	QBFTSignatureType    SignatureType = [4]byte{1, 0, 0, 0}
	PartialSignatureType SignatureType = [4]byte{2, 0, 0, 0}
	DKGSignatureType     SignatureType = [4]byte{3, 0, 0, 0}
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

// EncryptionCalls captures all RSA share encryption calls
type EncryptionCalls interface {
	// Decrypt given a rsa pubkey and a PKCS1v15 cipher text byte array, returns the decrypted data
	Decrypt(pk *rsa.PublicKey, cipher []byte) ([]byte, error)
	// Encrypt given a rsa pubkey and data returns an PKCS1v15 encrypted cipher
	Encrypt(pk *rsa.PublicKey, data []byte) ([]byte, error)
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
	EncryptionCalls
	SignRoot(data Root, sigType SignatureType, pk []byte) (Signature, error)
}

type DKGSigner interface {
	SSVSigner
	// SignDKGOutput signs output according to the SIP https://docs.google.com/document/d/1TRVUHjFyxINWW2H9FYLNL2pQoLy6gmvaI62KL_4cREQ/edit
	SignDKGOutput(output Root, address common.Address) (Signature, error)
}

// KeyManager is an interface responsible for all key manager functions
type KeyManager interface {
	BeaconSigner
	SSVSigner
	// AddShare saves a share key
	AddShare(shareKey *bls.SecretKey) error
}
