package records

import (
	"bytes"
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// recordWithEntry returns a signed record whose key entry holds the given raw
// RLP value.
func recordWithEntry(t testing.TB, key ENRKey, raw rlp.RawValue) *enr.Record {
	t.Helper()
	sk, err := crypto.GenerateKey()
	require.NoError(t, err)
	var r enr.Record
	r.Set(enr.WithEntry(string(key), raw))
	require.NoError(t, enode.SignV4(&r, sk))
	return &r
}

func TestGetDomainTypeEntry_Length(t *testing.T) {
	payload := make([]byte, 32)
	for i := range payload {
		payload[i] = byte(i + 1)
	}

	for _, key := range []ENRKey{KeyDomainType, KeyNextDomainType} {
		for _, n := range []int{0, 1, 2, 3, 4, 5, 32} {
			raw, err := rlp.EncodeToBytes(payload[:n])
			require.NoError(t, err)
			record := recordWithEntry(t, key, raw)

			got, getErr := GetDomainTypeEntry(record, key)

			if n == len(spectypes.DomainType{}) {
				require.NoError(t, getErr, "key=%s len=%d", key, n)
				require.Equal(t, spectypes.DomainType{0x01, 0x02, 0x03, 0x04}, got)
				continue
			}
			require.ErrorIs(t, getErr, ErrInvalidDomainTypeEntry, "key=%s len=%d", key, n)
			require.NotErrorIs(t, getErr, ErrEntryNotFound)
			require.Equal(t, spectypes.DomainType{}, got)
		}
	}
}

func TestDomainTypeEntry_RoundTrip(t *testing.T) {
	sk, err := crypto.GenerateKey()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", net.ParseIP("127.0.0.1"), 12000, 13000)
	require.NoError(t, err)

	current := spectypes.DomainType{0x00, 0x00, 0x05, 0x02}
	next := spectypes.DomainType{0x00, 0x00, 0x05, 0x03}
	require.NoError(t, SetDomainTypeEntry(node, KeyDomainType, current))
	require.NoError(t, SetDomainTypeEntry(node, KeyNextDomainType, next))

	got, err := GetDomainTypeEntry(node.Node().Record(), KeyDomainType)
	require.NoError(t, err)
	require.Equal(t, current, got)

	got, err = GetDomainTypeEntry(node.Node().Record(), KeyNextDomainType)
	require.NoError(t, err)
	require.Equal(t, next, got)

	_, err = GetDomainTypeEntry(node.Node().Record(), "missing")
	require.ErrorIs(t, err, ErrEntryNotFound)
}

func FuzzDomainTypeEntryDecode(f *testing.F) {
	for _, n := range []int{0, 1, 3, 4, 5} {
		raw, err := rlp.EncodeToBytes(bytes.Repeat([]byte{0xab}, n))
		require.NoError(f, err)
		f.Add(raw)
	}
	f.Add([]byte{0xc0})       // empty list
	f.Add([]byte{0x84, 0x01}) // truncated string

	sk, err := crypto.GenerateKey()
	require.NoError(f, err)

	f.Fuzz(func(t *testing.T, raw []byte) {
		var r enr.Record
		r.Set(enr.WithEntry(KeyDomainType, rlp.RawValue(raw)))
		if err := enode.SignV4(&r, sk); err != nil {
			t.Skip("record cannot be signed (e.g. over the 300-byte ENR limit), so it is not a valid ENR")
		}

		got, err := GetDomainTypeEntry(&r, KeyDomainType)
		if err != nil {
			require.Equal(t, spectypes.DomainType{}, got)
			return
		}
		// Only a canonical 4-byte RLP string decodes successfully.
		var want []byte
		require.NoError(t, rlp.DecodeBytes(raw, &want))
		require.Equal(t, want, got[:])
	})
}
