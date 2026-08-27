package gloas

import (
	"encoding/json"
	"fmt"
	"strconv"

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

// BuildProduceConfig assembles the produceBlockV4 POST body from the resolved cluster config and the
// per-slot reconstructed auths: one entry per configured builder that has a reconstructed auth (auth-less
// builders are omitted — beacon-APIs#630 requires an auth per entry), carrying the resolved per-entry and
// top-level knobs. It also returns the number of configured builders with no reconstructed auth for the
// slot — the E1 auth-unavailable signal.
func BuildProduceConfig(cfg ResolvedBuilderConfig, auths map[string]*SignedBuilderRequestAuth) (ProduceBuilderConfig, int) {
	out := ProduceBuilderConfig{
		MinBid:             cfg.MinBid,
		BuilderBoostFactor: cfg.BoostFactor,
	}
	authUnavailable := 0
	for i := range cfg.Entries {
		e := &cfg.Entries[i]
		auth, ok := auths[e.Identity]
		if !ok {
			authUnavailable++
			continue
		}
		out.Builders = append(out.Builders, ProduceBuilderEntry{
			URL:                 e.URL,
			Auth:                auth,
			BuilderPubKeys:      e.BuilderPubKeys,
			MaxExecutionPayment: e.MaxExecutionPayment,
			MinBid:              e.MinBid,
			BuilderBoostFactor:  e.BoostFactor,
		})
	}
	return out, authUnavailable
}

// NeutralProduceBuilderConfig is the produceBlockV4 POST body a cluster with no builders configured sends:
// an empty builders list with the neutral boost factor, so the beacon node weighs any p2p bid at par with
// the local build (beacon-APIs#630).
func NeutralProduceBuilderConfig() *ProduceBuilderConfig {
	return &ProduceBuilderConfig{BuilderBoostFactor: defaultBuilderBoostFactor}
}
