package gloas

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
// Entries' URL/AuthData drive the request-auth signing round (§5); the top-level knobs and the
// per-entry resolution below drive the produceBlockV4 POST body (§4, beacon-APIs#630).
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

// Configured reports whether the operator set any direct-builder configuration — entries or the top-level
// p2p knobs (MinBid / BuilderBoostFactor); the zero value is false. §4 POSTs produceBlockV4 when true and
// uses the enshrined GET when false (imposing no proposer knobs on an unconfigured cluster) — so a
// knobs-only config is honored, and clearing entries for a remote signer keeps the p2p knobs.
func (c *BuilderConfig) Configured() bool {
	return len(c.Entries) > 0 || c.MinBid != 0 || c.BuilderBoostFactor != nil
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

// ResolvedBuilderEntry is a BuilderEntry with its config strings decoded and knobs resolved once, at load
// (ResolveBuilderConfig). Identity — BuilderIdentity(URL, AuthData) — is the single key shared by the §5
// signing round and the §4 auth-cache lookup, so the two match by construction. The slices are shared,
// never copied (frozen auths alias AuthData, produce bodies alias BuilderPubKeys) — treat them as immutable.
type ResolvedBuilderEntry struct {
	Identity            string             // BuilderIdentity(URL, AuthData)
	URL                 string             // the builder URL, verbatim
	AuthData            []byte             // exact bytes signed into BuilderRequestAuth.Data
	BuilderPubKeys      []phase0.BLSPubKey // decoded bid-signing keys to pin; empty accepts any builder
	MaxExecutionPayment uint64             // Gwei cap on trusted execution-layer payment
	MinBid              uint64             // resolved (effective) bid floor, Gwei
	BoostFactor         uint64             // resolved (effective) bid multiplier, percent
}

// ResolvedBuilderConfig is a BuilderConfig decoded and resolved once (ResolveBuilderConfig) — the runtime
// form the §4 produce path and §5 signing round read, so neither re-parses config on the hot path.
type ResolvedBuilderConfig struct {
	MinBid      uint64 // top-level p2p-bid floor, Gwei
	BoostFactor uint64 // top-level p2p-bid multiplier, percent (resolved; neutral 100 by default)
	Entries     []ResolvedBuilderEntry
	configured  bool
}

// Configured mirrors BuilderConfig.Configured for the resolved form: any entries or top-level p2p knobs.
func (c *ResolvedBuilderConfig) Configured() bool { return c.configured }

// ResolveBuilderConfig validates cfg and decodes it into its runtime form in one pass: it applies every
// builder-set check (entry cap, http(s) URLs, decodable within-limit auth data, no duplicate (URL, auth
// data) identity, 48-byte builder pubkeys) and, per entry, decodes AuthData, resolves the effective knobs,
// and computes the Identity. Decoding once here is why the §4/§5 read paths carry no parse-error branches.
// ValidateBuilderConfig is this, discarding the result.
func ResolveBuilderConfig(cfg BuilderConfig) (ResolvedBuilderConfig, error) {
	if len(cfg.Entries) > MaxBuilderEntries {
		return ResolvedBuilderConfig{}, fmt.Errorf("%d builder entries exceed the %d limit", len(cfg.Entries), MaxBuilderEntries)
	}
	resolved := ResolvedBuilderConfig{
		MinBid:      cfg.MinBid,
		BoostFactor: cfg.EffectiveBoostFactor(),
		configured:  cfg.Configured(),
		Entries:     make([]ResolvedBuilderEntry, 0, len(cfg.Entries)),
	}
	seen := make(map[string]struct{}, len(cfg.Entries))
	for i := range cfg.Entries {
		e := &cfg.Entries[i]
		u, err := url.Parse(e.URL)
		if err != nil {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: invalid URL: %w", i, err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: URL must be http(s) with a host, got %q", i, e.URL)
		}
		// The URL's bytes are signed when they serve as the default auth data.
		if e.AuthData == "" && len(e.URL) > MaxBuilderAuthDataSize {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: URL is %d bytes, exceeding the %d auth-data limit its bytes default to", i, len(e.URL), MaxBuilderAuthDataSize)
		}
		data, err := e.AuthDataBytes()
		if err != nil {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: %w", i, err)
		}
		if len(data) == 0 {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: AuthData decodes to zero bytes — omit it to default to the URL bytes", i)
		}
		identity := BuilderIdentity(e.URL, data)
		if _, dup := seen[identity]; dup {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: duplicate (URL, AuthData) identity", i)
		}
		seen[identity] = struct{}{}
		pubkeys, err := e.builderPubKeys()
		if err != nil {
			return ResolvedBuilderConfig{}, fmt.Errorf("builder entry %d: %w", i, err)
		}
		resolved.Entries = append(resolved.Entries, ResolvedBuilderEntry{
			Identity:            identity,
			URL:                 e.URL,
			AuthData:            data,
			BuilderPubKeys:      pubkeys,
			MaxExecutionPayment: e.MaxExecutionPayment,
			MinBid:              e.EffectiveMinBid(&cfg),
			BoostFactor:         e.EffectiveBoostFactor(&cfg),
		})
	}
	return resolved, nil
}

// builderPubKeys parses the entry's 0x-hex BuilderPubKeys into BLS public keys (empty = accept any
// builder). Called by ResolveBuilderConfig, which surfaces any error at load.
func (e *BuilderEntry) builderPubKeys() ([]phase0.BLSPubKey, error) {
	if len(e.BuilderPubKeys) == 0 {
		return nil, nil
	}
	out := make([]phase0.BLSPubKey, 0, len(e.BuilderPubKeys))
	for j, s := range e.BuilderPubKeys {
		b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
		if err != nil {
			return nil, fmt.Errorf("BuilderPubKeys[%d]: invalid hex: %w", j, err)
		}
		if len(b) != 48 {
			return nil, fmt.Errorf("BuilderPubKeys[%d]: must be 48 bytes, got %d", j, len(b))
		}
		var pk phase0.BLSPubKey
		copy(pk[:], b)
		out = append(out, pk)
	}
	return out, nil
}

// ValidateBuilderConfig reports whether cfg is a well-formed builder set — the ResolveBuilderConfig checks,
// discarding the resolved result; used at startup, before the config reaches the runners. The property that
// matters most — every operator of every shared cluster holding the identical config — cannot be checked
// here and stays an operational requirement (docs/EXTERNAL_BUILDERS.md).
func ValidateBuilderConfig(cfg BuilderConfig) error {
	_, err := ResolveBuilderConfig(cfg)
	return err
}
