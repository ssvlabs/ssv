package topics

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log"
)

func TestSubFilter(t *testing.T) {
	l := log.TestLogger(t)
	sf := newSubFilter(l, 2)

	require.False(t, sf.CanSubscribe("xxx"))
	require.False(t, sf.CanSubscribe(commons.GetTopicFullName("xxx")))
	sf.(Whitelist).Register(commons.GetTopicFullName("1"))
	require.True(t, sf.CanSubscribe(commons.GetTopicFullName("1")))
	require.False(t, sf.CanSubscribe(commons.GetTopicFullName("2")))
}

// TestSubFilter_CanSubscribeBoole guards against regressing #2943: CanSubscribe must
// recognize Boole-fork topics ("/ssv/<network>/boole/<subnet>"), not just Alan topics.
// Before the fix, every Boole topic was rejected as "not of the same fork", causing a
// total post-Boole outage (self-subscribe crash-loop + QBFT broadcast failures).
func TestSubFilter_CanSubscribeBoole(t *testing.T) {
	const networkName = "hoodi-stage"
	l := log.TestLogger(t)
	sf := newSubFilter(l, 2)

	booleTopic := commons.BooleTopic(networkName, 51)

	// A valid Boole topic is still gated by the whitelist: rejected until registered...
	require.False(t, sf.CanSubscribe(booleTopic))
	sf.(Whitelist).Register(booleTopic)
	require.True(t, sf.CanSubscribe(booleTopic))

	// ...and a different, unregistered Boole subnet stays rejected.
	require.False(t, sf.CanSubscribe(commons.BooleTopic(networkName, 52)))

	// Alan and Boole topics coexist in the same filter.
	sf.(Whitelist).Register(commons.GetTopicFullName("1"))
	require.True(t, sf.CanSubscribe(commons.GetTopicFullName("1")))
	require.True(t, sf.CanSubscribe(booleTopic))

	// Malformed / foreign-fork topics are rejected outright, even if "whitelisted",
	// because they don't parse as a known subnet topic.
	for _, bad := range []string{
		"/ssv/" + networkName + "/boole/",      // missing subnet
		"/ssv/" + networkName + "/otherfork/1", // unknown fork segment
		"/ssv/" + networkName + "/boole/abc",   // non-numeric subnet
	} {
		sf.(Whitelist).Register(bad)
		require.Falsef(t, sf.CanSubscribe(bad), "expected %q to be rejected", bad)
	}
}
