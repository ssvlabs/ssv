package gloas

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ProduceBuilderConfig is the beacon-APIs#630 produceBlockV4 POST body: the resolved, auth-attached
// per-builder inputs a proposer sends to its beacon node. It is distinct from the keymanager-APIs#88
// BuilderConfig (operator config) — here each entry carries the reconstructed SignedBuilderRequestAuth,
// and the top-level MinBid/BuilderBoostFactor govern p2p bids. JSON-encoded on the wire, with uint64 as
// decimal strings per the beacon-API convention.
type ProduceBuilderConfig struct {
	MinBid             uint64
	BuilderBoostFactor uint64
	Builders           []ProduceBuilderEntry
}

// ProduceBuilderEntry is one builder-API bid request in a ProduceBuilderConfig: the builder URL, the
// reconstructed auth, and the per-builder selection knobs (already resolved against the config defaults).
type ProduceBuilderEntry struct {
	URL                 string
	Auth                *SignedBuilderRequestAuth
	BuilderPubKeys      []phase0.BLSPubKey
	MaxExecutionPayment uint64
	MinBid              uint64
	BuilderBoostFactor  uint64
}

type produceBuilderConfigJSON struct {
	MinBid             string                    `json:"min_bid"`
	BuilderBoostFactor string                    `json:"builder_boost_factor"`
	Builders           []produceBuilderEntryJSON `json:"builders"`
}

type produceBuilderEntryJSON struct {
	URL                 string                    `json:"url"`
	Auth                *SignedBuilderRequestAuth `json:"auth"`
	BuilderPubKeys      []string                  `json:"builder_pubkeys"`
	MaxExecutionPayment string                    `json:"max_execution_payment"`
	MinBid              string                    `json:"min_bid"`
	BuilderBoostFactor  string                    `json:"builder_boost_factor"`
}

// MarshalJSON implements json.Marshaler, emitting the beacon-APIs#630 shape (uint64 as decimal strings,
// pubkeys as 0x-hex, auth as the SignedBuilderRequestAuth object).
func (c *ProduceBuilderConfig) MarshalJSON() ([]byte, error) {
	entries := make([]produceBuilderEntryJSON, 0, len(c.Builders))
	for i := range c.Builders {
		e := &c.Builders[i]
		pubkeys := make([]string, 0, len(e.BuilderPubKeys))
		for _, pk := range e.BuilderPubKeys {
			pubkeys = append(pubkeys, fmt.Sprintf("%#x", pk))
		}
		entries = append(entries, produceBuilderEntryJSON{
			URL:                 e.URL,
			Auth:                e.Auth,
			BuilderPubKeys:      pubkeys,
			MaxExecutionPayment: strconv.FormatUint(e.MaxExecutionPayment, 10),
			MinBid:              strconv.FormatUint(e.MinBid, 10),
			BuilderBoostFactor:  strconv.FormatUint(e.BuilderBoostFactor, 10),
		})
	}
	return json.Marshal(&produceBuilderConfigJSON{
		MinBid:             strconv.FormatUint(c.MinBid, 10),
		BuilderBoostFactor: strconv.FormatUint(c.BuilderBoostFactor, 10),
		Builders:           entries,
	})
}

// builderPubKeys parses the entry's 0x-hex BuilderPubKeys into BLS public keys. The list is validated at
// startup (ValidateBuilderConfig), so an error here is defensive.
func (e *BuilderEntry) builderPubKeys() ([]phase0.BLSPubKey, error) {
	if len(e.BuilderPubKeys) == 0 {
		return nil, nil
	}
	out := make([]phase0.BLSPubKey, 0, len(e.BuilderPubKeys))
	for _, s := range e.BuilderPubKeys {
		b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid builder pubkey hex: %w", err)
		}
		if len(b) != len(phase0.BLSPubKey{}) {
			return nil, fmt.Errorf("builder pubkey must be %d bytes, got %d", len(phase0.BLSPubKey{}), len(b))
		}
		var pk phase0.BLSPubKey
		copy(pk[:], b)
		out = append(out, pk)
	}
	return out, nil
}

// BuildProduceConfig resolves cfg against the per-slot reconstructed auths into the produceBlockV4 POST
// body: one entry per configured builder that has a reconstructed auth (auth-less builders are omitted —
// beacon-APIs#630 requires an auth per entry), with per-entry knobs resolved against the config defaults
// (keymanager-APIs#88) and the top-level p2p knobs carried through. It also returns the number of
// configured builders with no reconstructed auth for the slot — the E1 auth-unavailable signal.
func BuildProduceConfig(cfg BuilderConfig, auths map[string]*SignedBuilderRequestAuth) (ProduceBuilderConfig, int) {
	out := ProduceBuilderConfig{
		MinBid:             cfg.MinBid,
		BuilderBoostFactor: cfg.EffectiveBoostFactor(),
	}
	authUnavailable := 0
	for i := range cfg.Entries {
		e := &cfg.Entries[i]
		data, err := e.AuthDataBytes()
		if err != nil {
			authUnavailable++ // defensive, validated at startup; count, don't drop silently
			continue
		}
		auth, ok := auths[BuilderIdentity(e.URL, data)]
		if !ok {
			authUnavailable++
			continue
		}
		pubkeys, err := e.builderPubKeys()
		if err != nil {
			authUnavailable++ // defensive, validated at startup; count, don't drop silently
			continue
		}
		out.Builders = append(out.Builders, ProduceBuilderEntry{
			URL:                 e.URL,
			Auth:                auth,
			BuilderPubKeys:      pubkeys,
			MaxExecutionPayment: e.MaxExecutionPayment,
			MinBid:              e.EffectiveMinBid(&cfg),
			BuilderBoostFactor:  e.EffectiveBoostFactor(&cfg),
		})
	}
	return out, authUnavailable
}
