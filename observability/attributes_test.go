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

// TestBeaconRoleAttribute verifies BeaconRoleAttribute creates correct key-value attribute.
func TestBeaconRoleAttribute(t *testing.T) {
	t.Parallel()

	role := types.BeaconRole(1)

	attr := BeaconRoleAttribute(role)

	require.Equal(t, "ssv.beacon.role", string(attr.Key))
	require.Equal(t, role.String(), attr.Value.AsString())
}

// TestRunnerRoleAttribute verifies RunnerRoleAttribute creates correct key-value attribute.
func TestRunnerRoleAttribute(t *testing.T) {
	t.Parallel()

	role := types.RunnerRole(1)

	attr := RunnerRoleAttribute(role)

	require.Equal(t, RunnerRoleAttrKey, string(attr.Key))
	require.Equal(t, role.String(), attr.Value.AsString())
}

// TestDutyRoundAttribute verifies DutyRoundAttribute creates correct key-value attribute.
func TestDutyRoundAttribute(t *testing.T) {
	t.Parallel()

	round := qbft.Round(10)

	attr := DutyRoundAttribute(round)

	require.Equal(t, "ssv.validator.duty.round", string(attr.Key))
	require.Equal(t, int64(round), attr.Value.AsInt64())
}

// TestNetworkDirectionAttribute verifies NetworkDirectionAttribute creates correct key-value attribute.
func TestNetworkDirectionAttribute(t *testing.T) {
	t.Parallel()

	tests := []struct {
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

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			attr := NetworkDirectionAttribute(tc.direction)

			require.Equal(t, "ssv.p2p.connection.direction", string(attr.Key))
			require.Equal(t, tc.expectedValue, attr.Value.AsString())
		})
	}
}

// TestUint64AttributeValueInt64 verifies Uint64AttributeValue correctly handles values within int64 range.
func TestUint64AttributeValueInt64(t *testing.T) {
	t.Parallel()

	var value uint64 = 100

	attrValue := Uint64AttributeValue(value)

	require.Equal(t, attribute.INT64, attrValue.Type())
	require.Equal(t, int64(value), attrValue.AsInt64())
}

// TestUint64AttributeValueString verifies Uint64AttributeValue correctly handles values exceeding int64 range.
func TestUint64AttributeValueString(t *testing.T) {
	t.Parallel()

	value := uint64(math.MaxInt64) + 1

	attrValue := Uint64AttributeValue(value)

	require.Equal(t, attribute.STRING, attrValue.Type())
	require.Equal(t, "9223372036854775808", attrValue.AsString())
}
