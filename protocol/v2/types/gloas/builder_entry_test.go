package gloas

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuilderEntry_AuthDataBytes(t *testing.T) {
	// Omitted AuthData defaults to the URL's hostname.
	e := &BuilderEntry{URL: "https://builder.example.com"}
	b, err := e.AuthDataBytes()
	require.NoError(t, err)
	require.Equal(t, []byte("builder.example.com"), b)

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

// The default auth data is builder-specs' get_default_auth_data (SIP #94 §5), derived through ssv-spec so the
// node signs the bytes every other client derives: the upstream table verbatim, plus cases it only implies.
func TestDefaultAuthData(t *testing.T) {
	for url, want := range map[string]string{
		// builder-specs' table.
		"https://builder.example.com/":             "builder.example.com",
		"HTTPS://Builder.Example.com:443/bids?x=1": "builder.example.com",
		"https://builder.example.com:8080":         "builder.example.com",
		"https://user:pw@builder.example.com/":     "builder.example.com",
		"https://10.0.0.5:18550/eth/v1/builder":    "10.0.0.5",
		"https://[0:0:0:0:0:0:0:1]:8443/":          "[::1]",
		"https://[::ffff:192.0.2.1]/":              "[::ffff:c000:201]",
		// Implied: the fragment goes too, and IPv6 hex is lowercased.
		"https://builder.example.com/bids#top": "builder.example.com",
		"https://[2001:DB8::1]/":               "[2001:db8::1]",
	} {
		got, err := DefaultAuthData(url)
		require.NoError(t, err, url)
		require.Equal(t, want, string(got), url)
	}

	for _, url := range []string{
		"https://exämple.com/",           // an internationalized hostname must be given in punycode
		"https://[fe80::1%25eth0]:8443/", // a zoned IPv6 literal: Go and Python render the zone differently
		"not a url at all",
	} {
		_, err := DefaultAuthData(url)
		require.ErrorContains(t, err, "no default auth data", url)
	}
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
	require.False(t, (&BuilderConfig{}).Configured(), "zero value is not configured -> §4 produces with a neutral local-build config")
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
			{URL: "https://a.example", MaxExecutionPayment: 250},                                                                     // AuthData -> the URL's hostname; knobs inherited
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
	require.Equal(t, BuilderIdentity("https://a.example", []byte("a.example")), a.Identity)
	require.Equal(t, []byte("a.example"), a.AuthData, "omitted AuthData -> the URL's hostname")
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
		BuilderEntry{URL: "https://x.example", AuthData: "0x" + hex.EncodeToString([]byte("x.example"))},
	), "duplicate")
	// Different URLs on one host are distinct identities that default to the same auth data (and so share
	// one signed auth).
	require.NoError(t, validate(
		BuilderEntry{URL: "https://x.example/relay-a"},
		BuilderEntry{URL: "https://x.example:8443/relay-b"},
	))
	// A URL with no default auth data needs AuthData set explicitly.
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://exämple.com"}), "no default auth data")
	require.NoError(t, validate(BuilderEntry{URL: "https://exämple.com", AuthData: "0x0102"}))

	// BuilderPubKeys is a list; each must be 48-byte 0x-hex; empty accepts any builder.
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://x.example", BuilderPubKeys: []string{"0x01"}}), "48 bytes")
	require.ErrorContains(t, validate(BuilderEntry{URL: "https://x.example", BuilderPubKeys: []string{"0xzz"}}), "invalid hex")
	require.NoError(t, validate(BuilderEntry{
		URL:            "https://x.example",
		BuilderPubKeys: []string{"0x" + strings.Repeat("ab", 48), "0x" + strings.Repeat("cd", 48)},
	}))

	// URLs are bounded by the beacon-API's MAX_BUILDER_URL_SIZE, whatever the auth data.
	prefix := "https://x.example/"
	require.NoError(t, validate(BuilderEntry{URL: prefix + strings.Repeat("a", MaxBuilderURLSize-len(prefix))}))
	longURL := prefix + strings.Repeat("a", MaxBuilderURLSize-len(prefix)+1)
	require.ErrorContains(t, validate(BuilderEntry{URL: longURL}), "exceeding")
	require.ErrorContains(t, validate(BuilderEntry{URL: longURL, AuthData: "0x0102"}), "exceeding")
}
