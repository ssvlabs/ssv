package gloas

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// BuilderPreferencesEntry is one entry in the beacon-APIs#630 submitBuilderPreferences body (issue #2962
// phase 3): the ahead-of-time per-builder preference a proposer asks its beacon node to forward. The
// beacon node routes it by URL to that builder's submitBuilderPreferences for ProposerPubKey, so the
// builder holds MaxExecutionPayment (authenticated by Auth) before the bid request arrives. JSON-encoded
// on the wire, with uint64 as a decimal string per the beacon-API convention.
type BuilderPreferencesEntry struct {
	ProposerPubKey      phase0.BLSPubKey
	URL                 string
	Auth                *SignedBuilderRequestAuth
	MaxExecutionPayment uint64
}

type builderPreferencesEntryJSON struct {
	ProposerPubKey      string                    `json:"proposer_pubkey"`
	URL                 string                    `json:"url"`
	Auth                *SignedBuilderRequestAuth `json:"auth"`
	MaxExecutionPayment string                    `json:"max_execution_payment"`
}

// MarshalJSON implements json.Marshaler, emitting the beacon-APIs#630 shape.
func (e *BuilderPreferencesEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(&builderPreferencesEntryJSON{
		ProposerPubKey:      fmt.Sprintf("%#x", e.ProposerPubKey),
		URL:                 e.URL,
		Auth:                e.Auth,
		MaxExecutionPayment: strconv.FormatUint(e.MaxExecutionPayment, 10),
	})
}
