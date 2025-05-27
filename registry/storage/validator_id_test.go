package storage

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidatorPubKey tests the ValidatorPubKey implementation.
func TestValidatorPubKey(t *testing.T) {
	t.Parallel()

	pubKeyBytes := make([]byte, 48)
	for i := range pubKeyBytes {
		pubKeyBytes[i] = byte(i)
	}

	var pk spectypes.ValidatorPK
	copy(pk[:], pubKeyBytes)
	vpk := ValidatorPubKey(pk)

	t.Run("String", func(t *testing.T) {
		t.Parallel()

		expected := "pubkey:0x" + hex.EncodeToString(pubKeyBytes)

		assert.Equal(t, expected, vpk.String())
	})

	t.Run("Equal", func(t *testing.T) {
		t.Parallel()

		assert.True(t, vpk.Equal(vpk))

		samePK := ValidatorPubKey(pk)
		assert.True(t, vpk.Equal(samePK))

		var differentPK spectypes.ValidatorPK
		differentPK[0] = 255

		assert.False(t, vpk.Equal(ValidatorPubKey(differentPK)))
		assert.False(t, vpk.Equal(ValidatorIndex(123)))
		assert.False(t, vpk.Equal(nil))
	})

	t.Run("Bytes", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, pubKeyBytes, vpk.Bytes())
	})

	t.Run("ToValidatorPK", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, pk, vpk.ToValidatorPK())
	})
}

// TestValidatorIndex tests the ValidatorIndex implementation.
func TestValidatorIndex(t *testing.T) {
	t.Parallel()

	vi := ValidatorIndex(12345)

	t.Run("String", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "index:12345", vi.String())
	})

	t.Run("Equal", func(t *testing.T) {
		t.Parallel()

		assert.True(t, vi.Equal(vi))
		assert.True(t, vi.Equal(ValidatorIndex(12345)))
		assert.False(t, vi.Equal(ValidatorIndex(54321)))

		var pk spectypes.ValidatorPK

		assert.False(t, vi.Equal(ValidatorPubKey(pk)))
		assert.False(t, vi.Equal(nil))
	})

	t.Run("Uint64", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, uint64(12345), vi.Uint64())
	})

	t.Run("ToPhase0", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, phase0.ValidatorIndex(12345), vi.ToPhase0())
	})
}

// TestNewValidatorPubKey tests the NewValidatorPubKey constructor.
func TestNewValidatorPubKey(t *testing.T) {
	t.Parallel()

	t.Run("Valid", func(t *testing.T) {
		t.Parallel()

		pubKey := make([]byte, 48)
		for i := range pubKey {
			pubKey[i] = byte(i)
		}

		id, err := NewValidatorPubKey(pubKey)
		require.NoError(t, err)
		require.NotNil(t, id)

		vpk, ok := id.(ValidatorPubKey)
		require.True(t, ok)
		assert.Equal(t, pubKey, vpk.Bytes())
	})

	t.Run("InvalidLength", func(t *testing.T) {
		t.Parallel()

		testCases := []int{0, 47, 49, 100}

		for _, length := range testCases {
			t.Run(string(rune(length))+"bytes", func(t *testing.T) {
				pubKey := make([]byte, length)
				id, err := NewValidatorPubKey(pubKey)
				assert.Error(t, err)
				assert.Nil(t, id)
				assert.Contains(t, err.Error(), "invalid public key length")
			})
		}
	})
}

// TestNewValidatorIndex tests the NewValidatorIndex constructor.
func TestNewValidatorIndex(t *testing.T) {
	t.Parallel()

	testCases := []uint64{0, 1, 12345, ^uint64(0)}

	for _, index := range testCases {
		t.Run(string(rune(index)), func(t *testing.T) {
			id := NewValidatorIndex(index)
			require.NotNil(t, id)

			vi, ok := id.(ValidatorIndex)
			require.True(t, ok)
			assert.Equal(t, index, vi.Uint64())
		})
	}
}

// TestParseValidatorID tests the ParseValidatorID function.
func TestParseValidatorID(t *testing.T) {
	t.Parallel()

	t.Run("ValidPubKey", func(t *testing.T) {
		t.Parallel()

		pubKey := make([]byte, 48)
		for i := range pubKey {
			pubKey[i] = byte(i)
		}
		hexStr := hex.EncodeToString(pubKey)

		testCases := []string{
			"pubkey:" + hexStr,
			"pubkey:0x" + hexStr,
		}

		for _, input := range testCases {
			t.Run(input[:20], func(t *testing.T) {
				id, err := ParseValidatorID(input)
				require.NoError(t, err)
				require.NotNil(t, id)

				vpk, ok := id.(ValidatorPubKey)
				require.True(t, ok)
				assert.Equal(t, pubKey, vpk.Bytes())
			})
		}
	})

	t.Run("ValidIndex", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			input    string
			expected uint64
		}{
			{"index:0", 0},
			{"index:1", 1},
			{"index:12345", 12345},
			{"index:18446744073709551615", ^uint64(0)}, // max uint64
		}

		for _, tc := range testCases {
			t.Run(tc.input, func(t *testing.T) {
				id, err := ParseValidatorID(tc.input)
				require.NoError(t, err)
				require.NotNil(t, id)

				vi, ok := id.(ValidatorIndex)
				require.True(t, ok)
				assert.Equal(t, tc.expected, vi.Uint64())
			})
		}
	})

	t.Run("Invalid", func(t *testing.T) {
		t.Parallel()

		testCases := []string{
			"",
			"invalid",
			"pubkey:",
			"pubkey:invalid",
			"pubkey:0xinvalid",
			"pubkey:0x" + hex.EncodeToString(make([]byte, 47)), // wrong length
			"index:",
			"index:abc",
			"index:-1",
			"index:18446744073709551616", // overflow
		}

		for _, input := range testCases {
			t.Run(input, func(t *testing.T) {
				id, err := ParseValidatorID(input)
				assert.Error(t, err)
				assert.Nil(t, id)
			})
		}
	})
}

// TestIsValidatorPubKey tests the IsValidatorPubKey helper function.
func TestIsValidatorPubKey(t *testing.T) {
	t.Parallel()

	var pk spectypes.ValidatorPK
	vpk := ValidatorPubKey(pk)
	vi := ValidatorIndex(123)

	assert.True(t, IsValidatorPubKey(vpk))
	assert.False(t, IsValidatorPubKey(vi))
}

// TestIsValidatorIndex tests the IsValidatorIndex helper function.
func TestIsValidatorIndex(t *testing.T) {
	t.Parallel()

	var pk spectypes.ValidatorPK
	vpk := ValidatorPubKey(pk)
	vi := ValidatorIndex(123)

	assert.False(t, IsValidatorIndex(vpk))
	assert.True(t, IsValidatorIndex(vi))
}

// TestValidatorIDInterfaceCompliance ensures both types properly implement ValidatorID.
func TestValidatorIDInterfaceCompliance(t *testing.T) {
	t.Parallel()

	var _ ValidatorID = ValidatorPubKey{}
	var _ ValidatorID = ValidatorIndex(0)

	var pk spectypes.ValidatorPK
	checkValidatorID(t, ValidatorPubKey(pk))
	checkValidatorID(t, ValidatorIndex(123))
}

func checkValidatorID(t *testing.T, id ValidatorID) {
	t.Helper()

	str := id.String()

	assert.NotEmpty(t, str)
	assert.True(t, id.Equal(id))
	assert.False(t, id.Equal(nil))
}

// BenchmarkValidatorIDString benchmarks the String() method.
func BenchmarkValidatorIDString(b *testing.B) {
	var pk spectypes.ValidatorPK
	for i := range pk {
		pk[i] = byte(i)
	}
	vpk := ValidatorPubKey(pk)
	vi := ValidatorIndex(12345)

	b.Run("PubKey", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = vpk.String()
		}
	})

	b.Run("Index", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = vi.String()
		}
	})
}

// BenchmarkValidatorIDEqual benchmarks the Equal() method.
func BenchmarkValidatorIDEqual(b *testing.B) {
	var pk1, pk2 spectypes.ValidatorPK

	vpk1 := ValidatorPubKey(pk1)
	vpk2 := ValidatorPubKey(pk2)
	vi1 := ValidatorIndex(12345)
	vi2 := ValidatorIndex(12345)

	b.Run("PubKeyEqual", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = vpk1.Equal(vpk2)
		}
	})

	b.Run("IndexEqual", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = vi1.Equal(vi2)
		}
	})
}
