package gloas

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuilderEntry_AuthDataBytes(t *testing.T) {
	// Omitted AuthData defaults to the UTF-8 bytes of the URL, exactly as configured.
	e := &BuilderEntry{URL: "https://builder.example.com"}
	b, err := e.AuthDataBytes()
	require.NoError(t, err)
	require.Equal(t, []byte("https://builder.example.com"), b)

	// Explicit AuthData decodes as 0x-hex.
	e = &BuilderEntry{URL: "https://builder.example.com", AuthData: "0x1234567890abcdef"}
	b, err = e.AuthDataBytes()
	require.NoError(t, err)
	require.Equal(t, []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef}, b)

	_, err = (&BuilderEntry{URL: "https://x.example", AuthData: "0xzz"}).AuthDataBytes()
	require.ErrorContains(t, err, "invalid AuthData hex")

	_, err = (&BuilderEntry{URL: "https://x.example", AuthData: "0x" + strings.Repeat("00", MaxBuilderAuthDataSize+1)}).AuthDataBytes()
	require.ErrorContains(t, err, "exceeding")
}

func TestBuilderEntry_Effective(t *testing.T) {
	// Config-level boost factor defaults to the neutral 100; an entry inherits it when unset.
	empty := &BuilderConfig{}
	require.Equal(t, uint64(100), empty.EffectiveBoostFactor())
	require.Equal(t, uint64(100), (&BuilderEntry{}).EffectiveBoostFactor(empty))
	require.Equal(t, uint64(0), (&BuilderEntry{}).EffectiveMinBid(empty))

	// Entry values, when set, win over the config default (including an explicit zero).
	zero, seven, nine := uint64(0), uint64(7), uint64(9)
	cfg := &BuilderConfig{MinBid: 5, BuilderBoostFactor: &nine}
	require.Equal(t, uint64(9), cfg.EffectiveBoostFactor())
	require.Equal(t, uint64(0), (&BuilderEntry{BuilderBoostFactor: &zero}).EffectiveBoostFactor(cfg))
	require.Equal(t, uint64(7), (&BuilderEntry{MinBid: &seven}).EffectiveMinBid(cfg))

	// An entry that omits its own inherits the config's (keymanager-APIs#88 resolution).
	require.Equal(t, uint64(9), (&BuilderEntry{}).EffectiveBoostFactor(cfg))
	require.Equal(t, uint64(5), (&BuilderEntry{}).EffectiveMinBid(cfg))
}

func TestBuilderConfig_Configured(t *testing.T) {
	require.False(t, (&BuilderConfig{}).Configured(), "zero value is not configured -> §4 uses the enshrined GET")
	require.True(t, (&BuilderConfig{Entries: []BuilderEntry{{URL: "https://x.example"}}}).Configured(), "entries -> configured")
	require.True(t, (&BuilderConfig{MinBid: 1}).Configured(), "top-level MinBid -> configured (knobs-only)")
	zero := uint64(0)
	require.True(t, (&BuilderConfig{BuilderBoostFactor: &zero}).Configured(), "explicit boost 0 -> configured (not the nil zero value)")
}

func TestResolveBuilderConfig(t *testing.T) {
	// A valid config decodes and resolves once: Identity, AuthData bytes, effective knobs, pubkeys.
	five, nine := uint64(5), uint64(9)
	cfg := BuilderConfig{
		MinBid:             5,
		BuilderBoostFactor: &nine,
		Entries: []BuilderEntry{
			{URL: "https://a.example", MaxExecutionPayment: 250},                                                                     // AuthData -> URL bytes; knobs inherited
			{URL: "https://b.example", AuthData: "0x0102", MinBid: &five, BuilderPubKeys: []string{"0x" + strings.Repeat("ab", 48)}}, // explicit auth + pinned key
		},
	}
	resolved, err := ResolveBuilderConfig(cfg)
	require.NoError(t, err)
	require.True(t, resolved.Configured())
	require.Equal(t, uint64(5), resolved.MinBid)
	require.Equal(t, uint64(9), resolved.BoostFactor)
	require.Len(t, resolved.Entries, 2)

	a := resolved.Entries[0]
	require.Equal(t, BuilderIdentity("https://a.example", []byte("https://a.example")), a.Identity)
	require.Equal(t, []byte("https://a.example"), a.AuthData, "omitted AuthData -> URL bytes")
	require.Equal(t, uint64(250), a.MaxExecutionPayment)
	require.Equal(t, uint64(5), a.MinBid, "inherits config MinBid")
	require.Equal(t, uint64(9), a.BoostFactor, "inherits config BoostFactor")
	require.Empty(t, a.BuilderPubKeys)

	b := resolved.Entries[1]
	require.Equal(t, BuilderIdentity("https://b.example", []byte{0x01, 0x02}), b.Identity)
	require.Equal(t, []byte{0x01, 0x02}, b.AuthData, "explicit AuthData decoded from hex")
	require.Equal(t, uint64(5), b.MinBid, "entry MinBid wins over config default")
	require.Len(t, b.BuilderPubKeys, 1)

	// The zero config resolves to an empty, unconfigured result.
	empty, err := ResolveBuilderConfig(BuilderConfig{})
	require.NoError(t, err)
	require.False(t, empty.Configured())
	require.Empty(t, empty.Entries)
}

func TestValidateBuilderConfig(t *testing.T) {
	validate := func(entries ...BuilderEntry) error {
		return ValidateBuilderConfig(BuilderConfig{Entries: entries})
	}

	require.NoError(t, validate(
		BuilderEntry{URL: "https://builder-a.example.com"},
		BuilderEntry{URL: "https://builder-b.example.com", AuthData: "0x0102"},
		// Same URL, different auth data — a distinct identity per keymanager-APIs#88.
		BuilderEntry{URL: "https://builder-b.example.com", AuthData: "0x0304"},
	))
	require.NoError(t, ValidateBuilderConfig(BuilderConfig{}))

	require.ErrorContains(t,
		ValidateBuilderConfig(BuilderConfig{Entries: make([]BuilderEntry, MaxBuilderEntries+1)}),
		"exceed")
	// A non-empty http(s) URL is required (no empty-URL "default" entry any more).
	require.ErrorContains(t, validate(BuilderEntry{}), "must be http(s)")
	require.ErrorContains(t, validate(BuilderEntry{URL: "ftp://builder.example.com"}), "must be http(s)")
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://"}), "must be http(s)")
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://x.example", AuthData: "0x"}), "zero bytes")
	require.ErrorContains(t, validate(
		BuilderEntry{URL: "https://x.example"},
		BuilderEntry{URL: "https://x.example"},
	), "duplicate")
	// Same identity via explicit auth data equal to another entry's URL-derived default.
	require.ErrorContains(t, validate(
		BuilderEntry{URL: "https://x.example"},
		BuilderEntry{URL: "https://x.example", AuthData: "0x" + hex.EncodeToString([]byte("https://x.example"))},
	), "duplicate")

	// BuilderPubKeys is a list; each must be 48-byte 0x-hex; empty accepts any builder.
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://x.example", BuilderPubKeys: []string{"0x01"}}), "48 bytes")
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://x.example", BuilderPubKeys: []string{"0xzz"}}), "invalid hex")
	require.NoError(t, validate(BuilderEntry{
		URL:            "https://x.example",
		BuilderPubKeys: []string{"0x" + strings.Repeat("ab", 48), "0x" + strings.Repeat("cd", 48)},
	}))

	// A URL longer than the auth-data limit only matters when its bytes ARE the auth data.
	longURL := "https://x.example/" + strings.Repeat("a", MaxBuilderAuthDataSize)
	require.ErrorContains(t, validate(BuilderEntry{URL: longURL}), "exceeding")
	require.NoError(t, validate(BuilderEntry{URL: longURL, AuthData: "0x0102"}))
}
