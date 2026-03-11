package codec

import (
	"encoding/json"
	"fmt"
)

// DetectConsensusVersionFromSignedBlindedBeaconBlockJSON attempts to infer the consensus version
// of a SignedBlindedBeaconBlock JSON payload.
//
// This is a best-effort helper to support JSON requests that omit the Eth-Consensus-Version header,
// as allowed by the Builder API spec.
func DetectConsensusVersionFromSignedBlindedBeaconBlockJSON(data []byte) (string, error) {
	var envelope struct {
		Message struct {
			Body json.RawMessage `json:"body"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", fmt.Errorf("invalid JSON")
	}
	if len(envelope.Message.Body) == 0 {
		return "", fmt.Errorf("missing body")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Message.Body, &body); err != nil {
		return "", fmt.Errorf("invalid body")
	}

	// Electra/Fulu introduce execution requests in the beacon block body.
	if _, ok := body["execution_requests"]; ok {
		return ConsensusVersionElectra, nil
	}

	headerRaw, ok := body["execution_payload_header"]
	if !ok {
		return "", fmt.Errorf("missing execution_payload_header")
	}

	var header map[string]json.RawMessage
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return "", fmt.Errorf("invalid execution_payload_header")
	}

	// Deneb introduces blob gas fields in the execution payload header.
	if _, ok := header["blob_gas_used"]; ok {
		return ConsensusVersionDeneb, nil
	}
	if _, ok := header["excess_blob_gas"]; ok {
		return ConsensusVersionDeneb, nil
	}

	// Capella introduces withdrawals_root in the execution payload header.
	if _, ok := header["withdrawals_root"]; ok {
		return ConsensusVersionCapella, nil
	}

	return ConsensusVersionBellatrix, nil
}
