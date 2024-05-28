package types

import (
	"bytes"
	"encoding/gob"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// GenesisSSVShare is a combination of spectypes.Share and its Metadata.
// DEPRECATED, TODO: remove post-fork
type GenesisSSVShare struct {
	genesisspectypes.Share
	Metadata
}

// Encode encodes GenesisSSVShare using gob.
func (s *GenesisSSVShare) Encode() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(s); err != nil {
		return nil, fmt.Errorf("encode GenesisSSVShare: %w", err)
	}

	return b.Bytes(), nil
}

// Decode decodes GenesisSSVShare using gob.
func (s *GenesisSSVShare) Decode(data []byte) error {
	if len(data) > MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), MaxAllowedShareSize)
	}

	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("decode GenesisSSVShare: %w", err)
	}
	s.Quorum, s.PartialQuorum = ComputeQuorumAndPartialQuorum(len(s.Committee))
	return nil
}

// BelongsToOperator checks whether the share belongs to operator.
func (s *GenesisSSVShare) BelongsToOperator(operatorID spectypes.OperatorID) bool {
	return operatorID != 0 && s.OperatorID == operatorID
}

// HasBeaconMetadata checks whether the BeaconMetadata field is not nil.
func (s *GenesisSSVShare) HasBeaconMetadata() bool {
	return s != nil && s.BeaconMetadata != nil
}

func (s *GenesisSSVShare) IsAttesting(epoch phase0.Epoch) bool {
	return s.HasBeaconMetadata() &&
		(s.BeaconMetadata.IsAttesting() || (s.BeaconMetadata.Status == eth2apiv1.ValidatorStatePendingQueued && s.BeaconMetadata.ActivationEpoch <= epoch))
}

func (s *GenesisSSVShare) SetFeeRecipient(feeRecipient bellatrix.ExecutionAddress) {
	s.FeeRecipientAddress = feeRecipient
}
