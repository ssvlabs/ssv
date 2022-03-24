package signer

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	v1 "github.com/bloxapp/ssv/protocol/v1"
	"github.com/bloxapp/ssv/protocol/v1/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// DomainType is a unique identifier for signatures, 2 identical pieces of data signed with different domains will result in different sigs
type DomainType []byte

var (
	PrimusTestnet = DomainType("primus_testnet")
)

type SignatureType []byte

var (
	QBFTSigType          = []byte{1, 0, 0, 0}
	PostConsensusSigType = []byte{2, 0, 0, 0}
)

type BeaconSigner interface {
	// SignAttestation signs the given attestation
	SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error)
	// IsAttestationSlashable returns error if attestation is slashable
	IsAttestationSlashable(data *spec.AttestationData) error
}

// SSVSigner used for all SSV specific signing
type SSVSigner interface {
	SignRoot(data v1.Root, sigType SignatureType, pk []byte) (crypto.Signature, error)
}

// KeyManager is an interface responsible for all key manager functions
type KeyManager interface {
	BeaconSigner
	SSVSigner
	// AddShare saves a share key
	AddShare(shareKey *bls.SecretKey) error
}

// SSVKeyManager implements the KeyManager interface with all of its funcs
type SSVKeyManager struct {
	keys               map[string]*bls.SecretKey // holds pub keys as key and secret key as value
	domain             DomainType
	highestAttestation *spec.AttestationData
}

func NewSSVKeyManager(domain DomainType) KeyManager {
	return &SSVKeyManager{
		keys:   make(map[string]*bls.SecretKey),
		domain: domain,
	}
}

// SignAttestation signs the given attestation
func (s *SSVKeyManager) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	if err := s.IsAttestationSlashable(data); err != nil {
		return nil, nil, errors.Wrap(err, "can't sign slashalbe attestation")
	}
	s.highestAttestation = data
	panic("implement from beacon ")
}

// IsAttestationSlashable returns error if attestation data is slashable
func (s *SSVKeyManager) IsAttestationSlashable(data *spec.AttestationData) error {
	if s.highestAttestation == nil {
		return nil
	}
	if data.Slot <= s.highestAttestation.Slot {
		return errors.New("attestation data slot potentially slashable")
	}
	if data.Source.Epoch <= s.highestAttestation.Source.Epoch {
		return errors.New("attestation data source epoch potentially slashable")
	}
	if data.Target.Epoch <= s.highestAttestation.Target.Epoch {
		return errors.New("attestation data target epoch potentially slashable")
	}
	return nil
}

func (s *SSVKeyManager) SignRoot(data v1.Root, sigType SignatureType, pk []byte) (crypto.Signature, error) {
	if k, found := s.keys[hex.EncodeToString(pk)]; found {
		computedRoot, err := crypto.ComputeSigningRoot(data, crypto.ComputeSignatureDomain(s.domain, sigType))
		if err != nil {
			return nil, errors.Wrap(err, "could not sign root")
		}

		return k.SignByte(computedRoot).Serialize(), nil
	}
	return nil, errors.New("pk not found")
}

// AddShare saves a share key
func (s *SSVKeyManager) AddShare(sk *bls.SecretKey) error {
	s.keys[hex.EncodeToString(sk.GetPublicKey().Serialize())] = sk
	return nil
}
