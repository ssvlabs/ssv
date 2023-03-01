package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"sort"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

// SSVShare is a combination of spectypes.Share and its Metadata.
type SSVShare struct {
	spectypes.Share
	Metadata
}

// Encode encodes SSVShare using gob.
func (s *SSVShare) Encode() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(s); err != nil {
		return nil, fmt.Errorf("encode SSVShare: %w", err)
	}

	return b.Bytes(), nil
}

// Decode decodes SSVShare using gob.
func (s *SSVShare) Decode(data []byte) error {
	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("decode SSVShare: %w", err)
	}

	return nil
}

// BelongsToOperator checks whether the share belongs to operator.
func (s *SSVShare) BelongsToOperator(operatorID spectypes.OperatorID) bool {
	return s.OperatorID == operatorID
}

// BelongsToOperatorID checks whether the share belongs to operator ID.
func (s *SSVShare) BelongsToOperatorID(operatorID spectypes.OperatorID) bool {
	for _, operator := range s.Committee {
		if operator.OperatorID == operatorID {
			return true
		}
	}
	return false
}

// HasBeaconMetadata checks whether the BeaconMetadata field is not nil.
func (s *SSVShare) HasBeaconMetadata() bool {
	return s != nil && s.BeaconMetadata != nil
}

func (s *SSVShare) SetShareFeeRecipient(recipientsCollection registrystorage.RecipientsCollection) error {
	r, found, err := recipientsCollection.GetRecipientData(s.OwnerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get recipient data")
	}
	if !found {
		// use owner address as a default for the fee recipient
		s.FeeRecipient = s.OwnerAddress
		return nil
	}

	s.FeeRecipient = r.Fee
	return nil
}

// SetClusterID set the given share object with computed cluster ID
func (s *SSVShare) SetClusterID() error {
	oids := make([]uint64, 0)
	for _, o := range s.Committee {
		oids = append(oids, uint64(o.OperatorID))
	}

	hash, err := ComputeClusterIDHash(s.OwnerAddress.Bytes(), oids)
	if err != nil {
		return errors.New("could not compute share cluster id")
	}

	s.ClusterID = hash
	return nil
}

// ComputeClusterIDHash will compute cluster ID hash with given owner address and operator ids
func ComputeClusterIDHash(ownerAddress []byte, operatorIds []uint64) ([]byte, error) {
	// Create a new hash
	hash := sha256.New()

	// Write the binary representation of the owner address to the hash
	hash.Write(ownerAddress)

	// Sort the array in ascending order
	sort.Slice(operatorIds, func(i, j int) bool {
		return operatorIds[i] < operatorIds[j]
	})

	// Write the values to the hash
	for _, id := range operatorIds {
		if err := binary.Write(hash, binary.BigEndian, id); err != nil {
			return nil, err
		}
	}

	return hash.Sum(nil), nil
}

// Metadata represents metadata of SSVShare.
type Metadata struct {
	BeaconMetadata *beaconprotocol.ValidatorMetadata
	OwnerAddress   common.Address
	Liquidated     bool
	ClusterID      []byte
	FeeRecipient   common.Address
}
