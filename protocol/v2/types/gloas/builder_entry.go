package gloas

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// MaxBuilderEntries caps the configured direct-builder list (issue #2962 D2) and, through
// MaxRequestAuthDistinctRoots, the wire budget that config implies.
const MaxBuilderEntries = 8

// MaxRequestAuthDistinctRoots bounds the distinct RequestAuthV1 signing roots one signer may put on
// the wire per proposal slot: exactly one per authenticatable builder entry, so the budget equals
// the entry cap. No headroom is provisioned — auth roots don't move with dependent_root, and wire
// validation is config-independent, so every extra admitted root would burden clusters that never
// opt in. The accepted cost: a restart with a changed builder list can present fresh roots to a
// slot whose budget is already spent, and the excess is IGNOREd until that slot passes
// (self-healing as the lookahead rolls). Message validation enforces the bound per (slot, signer);
// the §5 dispatcher sizes its pending stash from it.
const MaxRequestAuthDistinctRoots = MaxBuilderEntries

// BuilderIdentity is the identity of a configured builder relationship: the (URL, auth data) pair,
// per keymanager-APIs#87 (multiple entries MAY share a URL with different auth data). It keys the
// per-slot reconstructed-auth cache and the config duplicate check.
func BuilderIdentity(url string, authData []byte) string {
	return url + "\x00" + string(authData)
}

// BuilderEntry is one configured direct builder for the ePBS (Gloas) external-builder overlay
// (issue #2962), in keymanager-APIs#87's field vocabulary plus beacon-APIs#625's max_trusted_bid.
//
// Entries MUST be identical across ALL operators of every cluster sharing a validator: AuthData is
// threshold-signed into RequestAuthV1, so any byte divergence splits the quorum and silently
// disables that builder; the unsigned knobs steer bid selection per-operator, where divergence is
// consensus-safe but leaves the effective policy to whoever leads the round. See
// docs/EXTERNAL_BUILDERS.md.
//
// Today only URL and AuthData are consumed; the unsigned knobs take effect with the produceBlockV4
// POST migration and track beacon-APIs#625 until it merges.
type BuilderEntry struct {
	// URL the beacon node (and, for submitBuilderPreferences, the SSV node) contacts the builder
	// on. An empty URL denotes the single default entry: unsigned preferences applied to any
	// contacted builder without a matching entry (beacon-APIs#625); it cannot be authenticated.
	URL string `yaml:"URL"`
	// AuthData is the 0x-hex form of the exact bytes signed into RequestAuthV1.Data — the token
	// agreed with the builder out of band. When omitted it defaults to the UTF-8 bytes of URL,
	// exactly as configured (the builder-specs default; no canonicalization anywhere).
	AuthData string `yaml:"AuthData"`
	// BuilderBoostFactor is the percentage multiplier applied to this builder's bid value when the
	// beacon node chooses between builder bids and the local payload; nil defaults to the neutral
	// 100 (0 forces local, MaxUint64 forces the builder — beacon-APIs#625).
	BuilderBoostFactor *uint64 `yaml:"BuilderBoostFactor"`
	// MaxTrustedBid caps, in Gwei, how much of this builder's bid value the beacon node may trust
	// off-protocol (beacon-APIs#625).
	MaxTrustedBid uint64 `yaml:"MaxTrustedBid"`
	// MinBid is the minimum bid value in Gwei below which this builder's bids are ignored in favor
	// of the local payload (beacon-APIs#625).
	MinBid uint64 `yaml:"MinBid"`
	// MaxExecutionPayment caps, in Gwei, the execution-layer (trusted, off-protocol) payment
	// accepted from this builder; submitted via submitBuilderPreferences and used as the local
	// backstop when validating bids (builder-specs).
	MaxExecutionPayment uint64 `yaml:"MaxExecutionPayment"`
	// PubKey optionally pins the BLS public key bids from this builder must be signed with
	// (keymanager-APIs#87), 0x-hex.
	PubKey string `yaml:"PubKey"`
}

// defaultBuilderBoostFactor is the neutral bid multiplier (beacon-APIs#625).
const defaultBuilderBoostFactor = 100

// IsDefault reports whether this is the empty-URL default entry (unsigned preferences for any
// contacted builder without a matching entry).
func (e *BuilderEntry) IsDefault() bool { return e.URL == "" }

// AuthDataBytes returns the exact bytes signed into RequestAuthV1.Data for this builder: the
// decoded AuthData, or the UTF-8 bytes of URL when AuthData is omitted. The default entry yields
// no bytes (it cannot be authenticated).
func (e *BuilderEntry) AuthDataBytes() ([]byte, error) {
	if e.AuthData == "" {
		return []byte(e.URL), nil
	}
	b, err := hex.DecodeString(strings.TrimPrefix(e.AuthData, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid AuthData hex: %w", err)
	}
	if len(b) > MaxRequestAuthDataSize {
		return nil, fmt.Errorf("AuthData is %d bytes, exceeding the %d limit", len(b), MaxRequestAuthDataSize)
	}
	return b, nil
}

// EffectiveBoostFactor resolves the configured boost factor, defaulting to the neutral 100.
func (e *BuilderEntry) EffectiveBoostFactor() uint64 {
	if e.BuilderBoostFactor == nil {
		return defaultBuilderBoostFactor
	}
	return *e.BuilderBoostFactor
}

// ValidateBuilderEntries checks a configured builder list: entry cap, at most one default
// (empty-URL) entry carrying no AuthData, parseable http(s) URLs, decodable within-limit auth
// data, no duplicate (URL, auth data) identities (multiple entries MAY share a URL with different
// auth data), and well-formed optional pubkeys. The property that matters most — every operator of
// every shared cluster holding the identical list — cannot be checked here and stays an
// operational requirement (docs/EXTERNAL_BUILDERS.md).
func ValidateBuilderEntries(entries []BuilderEntry) error {
	if len(entries) > MaxBuilderEntries {
		return fmt.Errorf("%d builder entries exceed the %d limit", len(entries), MaxBuilderEntries)
	}
	seen := make(map[string]struct{}, len(entries))
	haveDefault := false
	for i := range entries {
		e := &entries[i]
		if e.IsDefault() {
			if haveDefault {
				return fmt.Errorf("builder entry %d: at most one default (empty-URL) entry is allowed", i)
			}
			haveDefault = true
			if e.AuthData != "" {
				return fmt.Errorf("builder entry %d: the default (empty-URL) entry cannot carry AuthData — there is no single builder to authenticate to", i)
			}
		} else {
			u, err := url.Parse(e.URL)
			if err != nil {
				return fmt.Errorf("builder entry %d: invalid URL: %w", i, err)
			}
			if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("builder entry %d: URL must be http(s) with a host, got %q", i, e.URL)
			}
			// The URL's bytes are signed only when they serve as the default auth data.
			if e.AuthData == "" && len(e.URL) > MaxRequestAuthDataSize {
				return fmt.Errorf("builder entry %d: URL is %d bytes, exceeding the %d auth-data limit its bytes default to", i, len(e.URL), MaxRequestAuthDataSize)
			}
		}
		data, err := e.AuthDataBytes()
		if err != nil {
			return fmt.Errorf("builder entry %d: %w", i, err)
		}
		if !e.IsDefault() && len(data) == 0 {
			return fmt.Errorf("builder entry %d: explicitly empty AuthData — omit the field to default to the URL bytes", i)
		}
		identity := BuilderIdentity(e.URL, data)
		if _, dup := seen[identity]; dup {
			return fmt.Errorf("builder entry %d: duplicate (URL, AuthData) identity", i)
		}
		seen[identity] = struct{}{}
		if e.PubKey != "" {
			pk, err := hex.DecodeString(strings.TrimPrefix(e.PubKey, "0x"))
			if err != nil {
				return fmt.Errorf("builder entry %d: invalid PubKey hex: %w", i, err)
			}
			if len(pk) != 48 {
				return fmt.Errorf("builder entry %d: PubKey must be 48 bytes, got %d", i, len(pk))
			}
		}
	}
	return nil
}
