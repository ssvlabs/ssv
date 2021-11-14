package valcheck

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/pkg/errors"
)

// AttestationValueCheck checks for an Attestation type value
type AttestationValueCheck struct {
	signer beacon.Signer
}

// Check returns error if value is invalid
func (v *AttestationValueCheck) Check(value []byte, pk []byte) error {
	// try and parse to attestation data
	inputValue := &spec.AttestationData{}
	if err := inputValue.UnmarshalSSZ(value); err != nil {
		return errors.Wrap(err, "could not parse input value storing attestation data")
	}

	if inputValue.Slot == 100 {
		return errors.New("TEST - failed on slot 100")
	}

	return v.signer.IsAttestationSlashable(inputValue, pk)
}
