package gloas

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// MaxBuilderEntries caps the configured direct-builder list (issue #2962 D2). It is SSV's own
// sub-cap of the beacon-API's MAX_BUILDER_ENTRIES (64); the tighter bound also sizes
// MaxRequestAuthDistinctRoots, the wire budget that config implies.
const MaxBuilderEntries = 8

// MaxRequestAuthDistinctRoots bounds the distinct BuilderRequestAuth signing roots one signer may put
// on the wire per proposal slot: at most one per configured entry (entries sharing auth_data share a
// root), so the budget equals the entry cap. No headroom is provisioned — auth roots don't move with
// dependent_root, and wire validation is config-independent, so every extra admitted root would
// burden clusters that never opt in. The accepted cost: a restart with a changed builder list can
// present fresh roots to a slot whose budget is already spent, and the excess is IGNOREd until that
// slot passes (self-healing as the lookahead rolls). Message validation enforces the bound per
// (slot, signer); the §5 dispatcher sizes its pending stash from it.
const MaxRequestAuthDistinctRoots = MaxBuilderEntries

// defaultBuilderBoostFactor is the neutral bid multiplier (keymanager-APIs#88 / beacon-APIs#630).
const defaultBuilderBoostFactor = 100

// BuilderIdentity is the identity of a configured builder relationship: the (URL, auth data) pair,
// per keymanager-APIs#88 (multiple entries MAY share a URL with different auth data; URL is compared
// exactly and auth data by decoded bytes). It keys the per-slot reconstructed-auth cache and the
// config duplicate check.
func BuilderIdentity(url string, authData []byte) string {
	return url + "\x00" + string(authData)
}

// BuilderConfig is the cluster's direct-builder configuration for the ePBS (Gloas) external-builder
// overlay (issue #2962), in keymanager-APIs#88's BuilderConfig vocabulary. The top-level MinBid and
// BuilderBoostFactor apply to p2p (gossiped) bids and, per #88, double as the default for any Entry
// that omits its own; each Entry names one builder to solicit a builder-API bid from.
//
// The whole config MUST be identical across ALL operators of every cluster sharing a validator:
// AuthData is threshold-signed into BuilderRequestAuth, so any byte divergence splits the quorum and
// silently disables that builder; the unsigned knobs steer bid selection per-operator, where
// divergence is consensus-safe but leaves the effective policy to whoever leads the round. See
// docs/EXTERNAL_BUILDERS.md.
//
// Today only Entries' URL and AuthData are consumed (the request-auth signing round); the unsigned
// knobs and the resolution below take effect with the produceBlockV4 POST migration (beacon-APIs#630).
type BuilderConfig struct {
	// MinBid is the minimum total payment (Gwei) accepted from a p2p bid, and the default for any
	// Entry that omits its own MinBid. Zero means no floor.
	MinBid uint64 `yaml:"MinBid"`
	// BuilderBoostFactor is the percentage bid multiplier applied to p2p bids, and the default for
	// any Entry that omits its own; nil is the neutral 100 (0 forces local, MaxUint64 forces the
	// builder).
	BuilderBoostFactor *uint64 `yaml:"BuilderBoostFactor"`
	// Entries is the set of builders to solicit builder-API bids from, one BuilderEntry each.
	Entries []BuilderEntry `yaml:"Entries"`
}

// BuilderEntry is one configured direct builder, in keymanager-APIs#88's BuilderEntry vocabulary. An
// omitted MinBid or BuilderBoostFactor inherits the enclosing BuilderConfig's value (its resolution
// is EffectiveMinBid / EffectiveBoostFactor).
type BuilderEntry struct {
	// URL the beacon node (and, for submitBuilderPreferences, the SSV node) contacts the builder on.
	// Required and non-empty.
	URL string `yaml:"URL"`
	// AuthData is the 0x-hex form of the exact bytes signed into BuilderRequestAuth.Data — the token
	// agreed with the builder out of band. When omitted it defaults to the UTF-8 bytes of URL,
	// exactly as configured (the builder-specs default; no canonicalization anywhere).
	AuthData string `yaml:"AuthData"`
	// BuilderPubKeys optionally pins the BLS public keys (0x-hex) that bids from this builder must be
	// signed with (keymanager-APIs#88). Empty accepts a bid from any builder.
	BuilderPubKeys []string `yaml:"BuilderPubKeys"`
	// MaxExecutionPayment caps, in Gwei, the execution-layer (trusted, off-protocol) payment accepted
	// from this builder; submitted via submitBuilderPreferences and the local backstop when valuing
	// bids (builder-specs).
	MaxExecutionPayment uint64 `yaml:"MaxExecutionPayment"`
	// MinBid is the minimum bid value (Gwei) below which this builder's bids are ignored in favor of
	// the local payload; nil inherits the enclosing BuilderConfig's MinBid.
	MinBid *uint64 `yaml:"MinBid"`
	// BuilderBoostFactor is the percentage bid multiplier for this builder; nil inherits the
	// enclosing BuilderConfig's BuilderBoostFactor.
	BuilderBoostFactor *uint64 `yaml:"BuilderBoostFactor"`
}

// EffectiveBoostFactor resolves the config-level boost factor, defaulting to the neutral 100.
func (c *BuilderConfig) EffectiveBoostFactor() uint64 {
	if c.BuilderBoostFactor == nil {
		return defaultBuilderBoostFactor
	}
	return *c.BuilderBoostFactor
}

// AuthDataBytes returns the exact bytes signed into BuilderRequestAuth.Data for this builder: the
// decoded AuthData, or the UTF-8 bytes of URL when AuthData is omitted.
func (e *BuilderEntry) AuthDataBytes() ([]byte, error) {
	if e.AuthData == "" {
		return []byte(e.URL), nil
	}
	b, err := hex.DecodeString(strings.TrimPrefix(e.AuthData, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid AuthData hex: %w", err)
	}
	if len(b) > MaxBuilderAuthDataSize {
		return nil, fmt.Errorf("AuthData is %d bytes, exceeding the %d limit", len(b), MaxBuilderAuthDataSize)
	}
	return b, nil
}

// EffectiveMinBid resolves this entry's MinBid, inheriting the config default (keymanager-APIs#88)
// when unset.
func (e *BuilderEntry) EffectiveMinBid(cfg *BuilderConfig) uint64 {
	if e.MinBid != nil {
		return *e.MinBid
	}
	return cfg.MinBid
}

// EffectiveBoostFactor resolves this entry's boost factor, inheriting the config default
// (keymanager-APIs#88) when unset — which itself defaults to the neutral 100.
func (e *BuilderEntry) EffectiveBoostFactor(cfg *BuilderConfig) uint64 {
	if e.BuilderBoostFactor != nil {
		return *e.BuilderBoostFactor
	}
	return cfg.EffectiveBoostFactor()
}

// ValidateBuilderConfig checks a configured builder set: entry cap, non-empty parseable http(s)
// URLs, decodable within-limit auth data, no duplicate (URL, auth data) identities (multiple entries
// MAY share a URL with different auth data), and well-formed optional builder pubkeys. The property
// that matters most — every operator of every shared cluster holding the identical config — cannot
// be checked here and stays an operational requirement (docs/EXTERNAL_BUILDERS.md).
func ValidateBuilderConfig(cfg BuilderConfig) error {
	if len(cfg.Entries) > MaxBuilderEntries {
		return fmt.Errorf("%d builder entries exceed the %d limit", len(cfg.Entries), MaxBuilderEntries)
	}
	seen := make(map[string]struct{}, len(cfg.Entries))
	for i := range cfg.Entries {
		e := &cfg.Entries[i]
		u, err := url.Parse(e.URL)
		if err != nil {
			return fmt.Errorf("builder entry %d: invalid URL: %w", i, err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("builder entry %d: URL must be http(s) with a host, got %q", i, e.URL)
		}
		// The URL's bytes are signed when they serve as the default auth data.
		if e.AuthData == "" && len(e.URL) > MaxBuilderAuthDataSize {
			return fmt.Errorf("builder entry %d: URL is %d bytes, exceeding the %d auth-data limit its bytes default to", i, len(e.URL), MaxBuilderAuthDataSize)
		}
		data, err := e.AuthDataBytes()
		if err != nil {
			return fmt.Errorf("builder entry %d: %w", i, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("builder entry %d: AuthData decodes to zero bytes — omit it to default to the URL bytes", i)
		}
		identity := BuilderIdentity(e.URL, data)
		if _, dup := seen[identity]; dup {
			return fmt.Errorf("builder entry %d: duplicate (URL, AuthData) identity", i)
		}
		seen[identity] = struct{}{}
		for j, pk := range e.BuilderPubKeys {
			b, err := hex.DecodeString(strings.TrimPrefix(pk, "0x"))
			if err != nil {
				return fmt.Errorf("builder entry %d: BuilderPubKeys[%d]: invalid hex: %w", i, j, err)
			}
			if len(b) != 48 {
				return fmt.Errorf("builder entry %d: BuilderPubKeys[%d]: must be 48 bytes, got %d", i, j, len(b))
			}
		}
	}
	return nil
}
