package faultval

import (
	"context"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/qa/faults"
)

// rejectAll stands in for the real validator: if the decorator ever consults it, these tests see
// ValidationReject and fail. That is the point — Wrap must not merely *usually* accept.
type rejectAll struct{ calls int }

func (r *rejectAll) Validate(context.Context, peer.ID, *pubsub.Message) pubsub.ValidationResult {
	r.calls++
	return pubsub.ValidationReject
}

func (r *rejectAll) SetSelfPID(peer.ID) {}

func (r *rejectAll) ValidatorForTopic(string) func(context.Context, peer.ID, *pubsub.Message) pubsub.ValidationResult {
	return r.Validate
}

func TestParseNoLocalValidation(t *testing.T) {
	for _, in := range []string{"", "false", "0", "no", "off", "FALSE", " off "} {
		got, err := faults.ParseNoLocalValidation(in)
		require.NoError(t, err, "input %q", in)
		require.False(t, got, "input %q", in)
	}
	for _, in := range []string{"true", "1", "yes", "on", "TRUE", " On "} {
		got, err := faults.ParseNoLocalValidation(in)
		require.NoError(t, err, "input %q", in)
		require.True(t, got, "input %q", in)
	}
}

// A typo must abort startup rather than silently leaving the gate ON while the tester believes it
// was bypassed — the same fail-fast contract faults.Parse has for an unknown FAULT.
func TestParseNoLocalValidationRejectsTypo(t *testing.T) {
	_, err := faults.ParseNoLocalValidation("ture")
	require.Error(t, err)
	require.Contains(t, err.Error(), "FAULT_NO_LOCAL_VALIDATION")
}

func TestWrapIsNoOpWhenModifierUnset(t *testing.T) {
	faults.InitNoLocalValidation(false)
	t.Cleanup(func() { faults.InitNoLocalValidation(false) })

	inner := &rejectAll{}
	got := Wrap(inner, zap.NewNop())

	// Identity, not merely equivalence: an unset modifier must leave the production validator
	// untouched, so a node without the modifier behaves byte-identically to one built without
	// this package.
	require.Same(t, validation.MessageValidator(inner), got)
	require.Equal(t, pubsub.ValidationReject, got.Validate(context.Background(), "p", nil))
	require.Equal(t, 1, inner.calls, "the real validator must still be consulted")
}

func TestWrapAcceptsEverythingWhenModifierSet(t *testing.T) {
	faults.InitNoLocalValidation(true)
	t.Cleanup(func() { faults.InitNoLocalValidation(false) })

	inner := &rejectAll{}
	got := Wrap(inner, zap.NewNop())

	// NotSame needs two pointers and acceptAll is a value type, so assert on the concrete type:
	// the returned validator must be the decorator, not the production one.
	require.IsType(t, acceptAll{}, got)
	require.Equal(t, pubsub.ValidationAccept, got.Validate(context.Background(), "p", nil))

	// Per-topic path must accept too — p2p registers per topic, so a decorator that only fixed
	// Validate would leave the gate live on every real topic.
	require.Equal(t, pubsub.ValidationAccept,
		got.ValidatorForTopic("ssv.v2.114")(context.Background(), "p", nil))

	require.Zero(t, inner.calls, "the real validator must NOT be consulted while bypassed")
}
