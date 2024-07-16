package beacon

import (
	"github.com/AKorpusenko/genesis-go-eth2-client/spec/phase0"
)

// DutyData represent unified duty types data
type DutyData struct {
	// Types that are valid to be assigned to Data:
	//	*InputValueAttestationData
	//	*InputValue_AggregationData
	//	*InputValue_BeaconBlock
	Data IsInputValueData `protobuf_oneof:"data"`
	// Types that are valid to be assigned to SignedData:
	//	*InputValueAttestation
	//	*InputValue_Aggregation
	//	*InputValue_Block
	SignedData IsInputValueSignedData `protobuf_oneof:"signed_data"`
}

// IsInputValueData interface representing input data
type IsInputValueData interface {
	isInputValueData()
}

// InputValueAttestationData implementing IsInputValueData
type InputValueAttestationData struct {
	AttestationData *phase0.AttestationData
}

// isInputValueData implementation
func (*InputValueAttestationData) isInputValueData() {}

// GetData returns input data
func (m *DutyData) GetData() IsInputValueData {
	if m != nil {
		return m.Data
	}
	return nil
}

// GetAttestationData return cast input data
func (m *DutyData) GetAttestationData() *phase0.AttestationData {
	if x, ok := m.GetData().(*InputValueAttestationData); ok {
		return x.AttestationData
	}
	return nil
}

// IsInputValueSignedData interface representing input signed data
type IsInputValueSignedData interface {
	isInputValueSignedData()
}

// InputValueAttestation implementing IsInputValueSignedData
type InputValueAttestation struct {
	Attestation *phase0.Attestation
}

// isInputValueSignedData implementation
func (*InputValueAttestation) isInputValueSignedData() {}

// GetSignedData returns input data
func (m *DutyData) GetSignedData() IsInputValueSignedData {
	if m != nil {
		return m.SignedData
	}
	return nil
}

// GetAttestation return cast attestation input data
func (m *DutyData) GetAttestation() *phase0.Attestation {
	if x, ok := m.GetSignedData().(*InputValueAttestation); ok {
		return x.Attestation
	}
	return nil
}
