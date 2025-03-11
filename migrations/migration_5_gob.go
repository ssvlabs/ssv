package migrations

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

var sharesPrefixGOB = []byte("shares/")

type storageShareGOB struct {
	Share
	Metadata
}

// Decode decodes Share using gob.
func (s *storageShareGOB) Decode(data []byte) error {
	if len(data) > types.MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), types.MaxAllowedShareSize)
	}

	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("decode storageShareGOB: %w", err)
	}
	s.Quorum, s.PartialQuorum = types.ComputeQuorumAndPartialQuorum(uint64(len(s.Committee)))
	return nil
}

// Share represents a storage share.
// The better name of the struct is storageShareGOB,
// but we keep the name Share to avoid conflicts with gob encoding.
type Share struct {
	OperatorID            spectypes.OperatorID
	ValidatorPubKey       []byte
	SharePubKey           []byte
	Committee             []*storageOperatorGOB
	Quorum, PartialQuorum uint64
	DomainType            spectypes.DomainType
	FeeRecipientAddress   [20]byte
	Graffiti              []byte
}

type storageOperatorGOB struct {
	OperatorID spectypes.OperatorID
	PubKey     []byte `ssz-size:"48"`
}

// Metadata represents metadata of SSVShare.
type Metadata struct {
	BeaconMetadata *beaconprotocol.ValidatorMetadata
	OwnerAddress   common.Address
	Liquidated     bool
}

func storageShareGOBToDomainShare(share *storageShareGOB) (*types.SSVShare, error) {
	committee := make([]*spectypes.ShareMember, len(share.Committee))
	for i, c := range share.Committee {
		committee[i] = &spectypes.ShareMember{
			Signer:      c.OperatorID,
			SharePubKey: c.PubKey,
		}
	}

	if len(share.ValidatorPubKey) != phase0.PublicKeyLength {
		return nil, fmt.Errorf("invalid ValidatorPubKey length: got %v, expected 48", len(share.ValidatorPubKey))
	}

	var validatorPubKey spectypes.ValidatorPK
	copy(validatorPubKey[:], share.ValidatorPubKey)

	domainShare := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey:     validatorPubKey,
			SharePubKey:         share.SharePubKey,
			Committee:           committee,
			DomainType:          share.DomainType,
			FeeRecipientAddress: share.FeeRecipientAddress,
			Graffiti:            share.Graffiti,
		},
		OwnerAddress: share.OwnerAddress,
		Liquidated:   share.Liquidated,
	}

	if share.BeaconMetadata != nil {
		domainShare.ValidatorIndex = share.BeaconMetadata.Index
		domainShare.Status = share.BeaconMetadata.Status
		domainShare.ActivationEpoch = share.BeaconMetadata.ActivationEpoch
	}

	return domainShare, nil
}
