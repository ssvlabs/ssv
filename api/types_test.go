package api

import (
	"encoding/json"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHexMarshalJSON tests hex marshaling to JSON.
func TestHexMarshalJSON(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		hex      Hex
		expected string
	}{
		{
			name:     "empty hex",
			hex:      Hex{},
			expected: `""`,
		},
		{
			name:     "simple hex",
			hex:      Hex{0x01, 0x02, 0x03},
			expected: `"010203"`,
		},
		{
			name:     "complex hex",
			hex:      Hex{0xDE, 0xAD, 0xBE, 0xEF},
			expected: `"deadbeef"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := tc.hex.MarshalJSON()

			require.NoError(t, err)
			assert.Equal(t, tc.expected, string(data))
		})
	}
}

// TestHexUnmarshalJSON tests hex unmarshaling from JSON.
func TestHexUnmarshalJSON(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		json     string
		expected Hex
		hasError bool
	}{
		{
			name:     "empty hex",
			json:     `""`,
			expected: Hex{},
			hasError: false,
		},
		{
			name:     "simple hex",
			json:     `"010203"`,
			expected: Hex{0x01, 0x02, 0x03},
			hasError: false,
		},
		{
			name:     "hex with 0x prefix",
			json:     `"0xdeadbeef"`,
			expected: Hex{0xDE, 0xAD, 0xBE, 0xEF},
			hasError: false,
		},
		{
			name:     "invalid hex string - no quotes",
			json:     `deadbeef`,
			expected: nil,
			hasError: true,
		},
		{
			name:     "invalid hex chars",
			json:     `"zzz"`,
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var h Hex
			err := h.UnmarshalJSON([]byte(tc.json))

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, h)
			}
		})
	}
}

// TestHexBind tests binding string to Hex.
func TestHexBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected Hex
		hasError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: Hex{},
			hasError: false,
		},
		{
			name:     "simple hex",
			input:    "010203",
			expected: Hex{0x01, 0x02, 0x03},
			hasError: false,
		},
		{
			name:     "hex with 0x prefix",
			input:    "0xdeadbeef",
			expected: Hex{0xDE, 0xAD, 0xBE, 0xEF},
			hasError: false,
		},
		{
			name:     "invalid hex chars",
			input:    "zzz",
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var h Hex
			err := h.Bind(tc.input)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, h)
			}
		})
	}
}

// TestHexSliceBind tests binding string to HexSlice.
func TestHexSliceBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected HexSlice
		hasError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			hasError: false,
		},
		{
			name:     "single hex",
			input:    "010203",
			expected: HexSlice{Hex{0x01, 0x02, 0x03}},
			hasError: false,
		},
		{
			name:  "multiple hex values",
			input: "010203,0xdeadbeef,abcdef",
			expected: HexSlice{
				Hex{0x01, 0x02, 0x03},
				Hex{0xDE, 0xAD, 0xBE, 0xEF},
				Hex{0xAB, 0xCD, 0xEF},
			},
			hasError: false,
		},
		{
			name:     "invalid hex in list",
			input:    "010203,zzz",
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var hs HexSlice
			err := hs.Bind(tc.input)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, hs)
			}
		})
	}
}

// TestUint64SliceBind tests binding string to Uint64Slice.
func TestUint64SliceBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected Uint64Slice
		hasError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			hasError: false,
		},
		{
			name:     "single number",
			input:    "123",
			expected: Uint64Slice{123},
			hasError: false,
		},
		{
			name:     "multiple numbers",
			input:    "123,456,789",
			expected: Uint64Slice{123, 456, 789},
			hasError: false,
		},
		{
			name:     "invalid number in list",
			input:    "123,abc",
			expected: nil,
			hasError: true,
		},
		{
			name:     "overflow uint64",
			input:    "18446744073709551616", // 2^64
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var us Uint64Slice
			err := us.Bind(tc.input)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, us)
			}
		})
	}
}

// TestRoleBind tests binding string to Role.
func TestRoleBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected Role
		hasError bool
	}{
		{
			name:     "attester role",
			input:    "ATTESTER",
			expected: Role(spectypes.BNRoleAttester),
			hasError: false,
		},
		{
			name:     "proposer role",
			input:    "PROPOSER",
			expected: Role(spectypes.BNRoleProposer),
			hasError: false,
		},
		{
			name:     "unknown role",
			input:    "UNKNOWN_ROLE",
			expected: Role(0),
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var r Role
			err := r.Bind(tc.input)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, r)
			}
		})
	}
}

// TestRoleMarshalJSON tests marshaling Role to JSON.
func TestRoleMarshalJSON(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		role     Role
		expected string
	}{
		{
			name:     "attester role",
			role:     Role(spectypes.BNRoleAttester),
			expected: `"ATTESTER"`,
		},
		{
			name:     "proposer role",
			role:     Role(spectypes.BNRoleProposer),
			expected: `"PROPOSER"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := tc.role.MarshalJSON()

			require.NoError(t, err)
			assert.Equal(t, tc.expected, string(data))
		})
	}
}

// TestRoleUnmarshalJSON tests unmarshaling Role from JSON.
func TestRoleUnmarshalJSON(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		json     string
		expected Role
		hasError bool
	}{
		{
			name:     "attester role",
			json:     `"ATTESTER"`,
			expected: Role(spectypes.BNRoleAttester),
			hasError: false,
		},
		{
			name:     "proposer role",
			json:     `"PROPOSER"`,
			expected: Role(spectypes.BNRoleProposer),
			hasError: false,
		},
		{
			name:     "invalid role",
			json:     `"INVALID_ROLE"`,
			expected: Role(0),
			hasError: true,
		},
		{
			name:     "non-string value",
			json:     `123`,
			expected: Role(0),
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var r Role
			err := r.UnmarshalJSON([]byte(tc.json))

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, r)
			}
		})
	}
}

// TestRoleSliceBind tests binding string to RoleSlice.
func TestRoleSliceBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected RoleSlice
		hasError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			hasError: false,
		},
		{
			name:     "single role",
			input:    "ATTESTER",
			expected: RoleSlice{Role(spectypes.BNRoleAttester)},
			hasError: false,
		},
		{
			name:  "multiple roles",
			input: "ATTESTER,PROPOSER,AGGREGATOR",
			expected: RoleSlice{
				Role(spectypes.BNRoleAttester),
				Role(spectypes.BNRoleProposer),
				Role(spectypes.BNRoleAggregator),
			},
			hasError: false,
		},
		{
			name:     "invalid role in list",
			input:    "ATTESTER,INVALID_ROLE",
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rs RoleSlice
			err := rs.Bind(tc.input)

			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, rs)
			}
		})
	}
}

// TestStructWithHexAndRole tests marshaling and unmarshaling a struct with Hex and Role fields.
func TestStructWithHexAndRole(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		ID   Hex      `json:"id"`
		Data Hex      `json:"data"`
		Role Role     `json:"role"`
		List HexSlice `json:"list"`
	}

	original := TestStruct{
		ID:   Hex{0x01, 0x02, 0x03},
		Data: Hex{0xDE, 0xAD, 0xBE, 0xEF},
		Role: Role(spectypes.BNRoleProposer),
		List: HexSlice{
			Hex{0xAA, 0xBB},
			Hex{0xCC, 0xDD},
		},
	}

	data, err := json.Marshal(original)

	require.NoError(t, err)

	var parsed TestStruct
	err = json.Unmarshal(data, &parsed)

	require.NoError(t, err)

	assert.Equal(t, original.ID, parsed.ID)
	assert.Equal(t, original.Data, parsed.Data)
	assert.Equal(t, original.Role, parsed.Role)
	assert.Equal(t, original.List, parsed.List)
}
