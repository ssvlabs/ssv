package observability

import (
	"math"
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// TestBeaconRoleAttribute_Creates verifies BeaconRoleAttribute creates correct key-value attribute.
func TestBeaconRoleAttribute_Creates(t *testing.T) {
	t.Parallel()

	role := types.BeaconRole(1)

	attr := BeaconRoleAttribute(role)

	require.Equal(t, "ssv.beacon.role", string(attr.Key))
	require.Equal(t, role.String(), attr.Value.AsString())
}

// TestRunnerRoleAttribute_Creates verifies RunnerRoleAttribute creates correct key-value attribute.
func TestRunnerRoleAttribute_Creates(t *testing.T) {
	t.Parallel()

	role := types.RunnerRole(1)

	attr := RunnerRoleAttribute(role)

	require.Equal(t, "ssv.runner.role", string(attr.Key))
	require.Equal(t, role.String(), attr.Value.AsString())
}

// TestDutyRoundAttribute_Creates verifies DutyRoundAttribute creates correct key-value attribute.
func TestDutyRoundAttribute_Creates(t *testing.T) {
	t.Parallel()

	round := qbft.Round(10)

	attr := DutyRoundAttribute(round)

	require.Equal(t, "ssv.validator.duty.round", string(attr.Key))
	require.Equal(t, int64(round), attr.Value.AsInt64())
}

// TestNetworkDirectionAttribute_Creates verifies NetworkDirectionAttribute creates correct key-value attribute.
func TestNetworkDirectionAttribute_Creates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		direction     network.Direction
		expectedValue string
	}{
		{
			name:          "inbound direction",
			direction:     network.DirInbound,
			expectedValue: "inbound",
		},
		{
			name:          "outbound direction",
			direction:     network.DirOutbound,
			expectedValue: "outbound",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			attr := NetworkDirectionAttribute(tc.direction)

			require.Equal(t, "ssv.p2p.connection.direction", string(attr.Key))
			require.Equal(t, tc.expectedValue, attr.Value.AsString())
		})
	}
}

// TestUint64AttributeValue_TypeConversion verifies Uint64AttributeValue handles values correctly.
func TestUint64AttributeValue_TypeConversion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		value          uint64
		expectedType   attribute.Type
		checkInt64     bool
		checkString    bool
		expectedInt64  int64
		expectedString string
	}{
		{
			name:          "value within int64 range",
			value:         100,
			expectedType:  attribute.INT64,
			checkInt64:    true,
			expectedInt64: 100,
		},
		{
			name:           "value exceeds int64 range",
			value:          uint64(math.MaxInt64) + 1,
			expectedType:   attribute.STRING,
			checkString:    true,
			expectedString: "9223372036854775808",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			attrValue := Uint64AttributeValue(tc.value)

			require.Equal(t, tc.expectedType, attrValue.Type())

			if tc.checkInt64 {
				require.Equal(t, tc.expectedInt64, attrValue.AsInt64())
			}

			if tc.checkString {
				require.Equal(t, tc.expectedString, attrValue.AsString())
			}
		})
	}
}
