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

	// The default entry yields no bytes.
	b, err = (&BuilderEntry{}).AuthDataBytes()
	require.NoError(t, err)
	require.Empty(t, b)

	_, err = (&BuilderEntry{URL: "https://x.example", AuthData: "0xzz"}).AuthDataBytes()
	require.ErrorContains(t, err, "invalid AuthData hex")

	_, err = (&BuilderEntry{URL: "https://x.example", AuthData: "0x" + strings.Repeat("00", MaxRequestAuthDataSize+1)}).AuthDataBytes()
	require.ErrorContains(t, err, "exceeding")
}

func TestBuilderEntry_EffectiveBoostFactor(t *testing.T) {
	require.Equal(t, uint64(100), (&BuilderEntry{}).EffectiveBoostFactor())
	zero := uint64(0)
	require.Equal(t, uint64(0), (&BuilderEntry{BuilderBoostFactor: &zero}).EffectiveBoostFactor())
}

func TestValidateBuilderEntries(t *testing.T) {
	valid := []BuilderEntry{
		{URL: "https://builder-a.example.com"},
		{URL: "https://builder-b.example.com", AuthData: "0x0102"},
		// Same URL, different auth data — a distinct identity per keymanager-APIs#87.
		{URL: "https://builder-b.example.com", AuthData: "0x0304"},
		{}, // the default entry
	}
	require.NoError(t, ValidateBuilderEntries(valid))
	require.NoError(t, ValidateBuilderEntries(nil))

	require.ErrorContains(t,
		ValidateBuilderEntries(make([]BuilderEntry, MaxBuilderEntries+1)),
		"exceed")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{}, {}}),
		"at most one default")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{AuthData: "0x01"}}),
		"cannot carry AuthData")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "ftp://builder.example.com"}}),
		"must be http(s)")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "https://"}}),
		"must be http(s)")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "https://x.example", AuthData: "0x"}}),
		"explicitly empty AuthData")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{
			{URL: "https://x.example"},
			{URL: "https://x.example"},
		}),
		"duplicate")
	// Same identity via explicit auth data equal to another entry's URL-derived default.
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{
			{URL: "https://x.example"},
			{URL: "https://x.example", AuthData: "0x" + hex.EncodeToString([]byte("https://x.example"))},
		}),
		"duplicate")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "https://x.example", PubKey: "0x01"}}),
		"PubKey must be 48 bytes")
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "https://x.example", PubKey: "0xzz"}}),
		"invalid PubKey hex")
	require.NoError(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: "https://x.example", PubKey: "0x" + strings.Repeat("ab", 48)}}))

	// A URL longer than the auth-data limit only matters when its bytes ARE the auth data.
	longURL := "https://x.example/" + strings.Repeat("a", MaxRequestAuthDataSize)
	require.ErrorContains(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: longURL}}),
		"exceeding")
	require.NoError(t,
		ValidateBuilderEntries([]BuilderEntry{{URL: longURL, AuthData: "0x0102"}}))
}
