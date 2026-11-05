package records

import (
	"bytes"
	"crypto/ecdsa"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability"
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

// oldNodeSchemes verifies signatures only, as SSV nodes did before
// validRecordScheme: they admit, store and serve any well-signed record.
var oldNodeSchemes = enr.SchemeMap{"v4": enode.V4ID{}}

// v5Node is a localhost discv5 node record, signed, with a socket to listen on.
type v5Node struct {
	ln     *enode.LocalNode
	socket *net.UDPConn
	key    *ecdsa.PrivateKey
}

// newV5Node returns a node whose record has domainType as its raw domaintype
// entry.
func newV5Node(t *testing.T, domainType []byte) v5Node {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	db, err := enode.OpenDB("")
	require.NoError(t, err)
	t.Cleanup(db.Close)
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)

	ln := enode.NewLocalNode(db, key)
	addr := socket.LocalAddr().(*net.UDPAddr)
	ln.SetStaticIP(addr.IP)
	ln.SetFallbackUDP(addr.Port)
	ln.Set(enr.WithEntry(KeyDomainType, domainType))
	if len(domainType) != len(spectypes.DomainType{}) {
		// LocalNode verifies its record against enode.ValidSchemes when signing
		// it, which an invalid domaintype fails, so sign it once here under the
		// old scheme. Callers create such a node before starting any listener,
		// which would read the map, and its endpoint is static, so it isn't
		// signed again.
		ssvScheme := enode.ValidSchemes["v4"]
		enode.ValidSchemes["v4"] = enode.V4ID{}
		ln.Node()
		enode.ValidSchemes["v4"] = ssvScheme
	}
	return v5Node{ln: ln, socket: socket, key: key}
}

func (n v5Node) listen(t *testing.T, cfg discover.Config) *discover.UDPv5 {
	t.Helper()
	cfg.PrivateKey = n.key
	listener, err := discover.ListenV5(n.socket, n.ln, cfg)
	require.NoError(t, err)
	t.Cleanup(listener.Close)
	return listener
}

func startV5(t *testing.T, cfg discover.Config, domainType []byte) *discover.UDPv5 {
	t.Helper()
	return newV5Node(t, domainType).listen(t, cfg)
}

var validDomainType = []byte{0x00, 0x00, 0x05, 0x02}

// findnode asks from for its table nodes at the distances of want, until
// they include valid or the timeout passes.
func findnode(t *testing.T, querier, from *discover.UDPv5, valid *enode.Node, want ...*enode.Node) []*enode.Node {
	t.Helper()
	distances := make([]uint, 0, len(want))
	for _, n := range want {
		distances = append(distances, uint(enode.LogDist(from.Self().ID(), n.ID())))
	}
	var got []*enode.Node
	require.Eventually(t, func() bool {
		got, _ = querier.Findnode(from.Self(), distances)
		return slices.ContainsFunc(got, func(n *enode.Node) bool { return n.ID() == valid.ID() })
	}, 5*time.Second, 50*time.Millisecond, "valid node not served")
	return got
}

func containsID(nodes []*enode.Node, id enode.ID) bool {
	return slices.ContainsFunc(nodes, func(n *enode.Node) bool { return n.ID() == id })
}

// A record with an invalid domaintype that contacts this node must not enter
// its table, from where FINDNODE would serve it on.
func TestDiscV5_DoesNotAdmitInvalidDomainTypeFromHandshake(t *testing.T) {
	invalidNode := newV5Node(t, []byte{0x01, 0x02})
	// The liveness check only delays serving a node that answers pings, which this
	// node does.
	node := startV5(t, discover.Config{NoFindnodeLivenessCheck: true}, validDomainType)

	// The table drops inbound nodes until it is initialized, and a repeated ping
	// reuses the session instead of handshaking again, so try fresh valid nodes
	// until one is admitted.
	// The loop runs on the test goroutine, not in require.Eventually, because
	// startV5 calls require and t.Cleanup.
	deadline := time.Now().Add(5 * time.Second)
	var valid *discover.UDPv5
	for {
		valid = startV5(t, discover.Config{}, validDomainType)
		if _, err := valid.Ping(node.Self()); err == nil && containsID(node.AllNodes(), valid.Self().ID()) {
			break
		}
		require.True(t, time.Now().Before(deadline), "valid node never admitted")
		time.Sleep(50 * time.Millisecond)
	}

	invalid := invalidNode.listen(t, discover.Config{})
	_, err := invalid.Ping(node.Self())
	require.Error(t, err, "the handshake with the invalid record must be rejected")

	oldNode := startV5(t, discover.Config{ValidSchemes: oldNodeSchemes}, validDomainType)
	got := findnode(t, oldNode, node, valid.Self(), invalid.Self(), valid.Self())
	require.False(t, containsID(got, invalid.Self().ID()), "node served the invalid record")
	require.False(t, containsID(node.AllNodes(), invalid.Self().ID()), "invalid record in the table")
}

// A record with an invalid domaintype in a NODES response must be dropped, or
// it enters the table and is served on.
func TestDiscV5_DropsInvalidDomainTypeFromNodesResponse(t *testing.T) {
	invalid := startV5(t, discover.Config{}, []byte{0x01, 0x02})
	oldNode := startV5(t, discover.Config{ValidSchemes: oldNodeSchemes}, validDomainType)
	valid := startV5(t, discover.Config{}, validDomainType)
	oldNode.AddKnownNode(invalid.Self())
	oldNode.AddKnownNode(valid.Self())
	node := startV5(t, discover.Config{}, validDomainType)

	got := findnode(t, node, oldNode, valid.Self(), invalid.Self(), valid.Self())
	require.False(t, containsID(got, invalid.Self().ID()), "invalid record accepted from NODES")
}

// A scheme left unwrapped would admit a malformed domain type without error.
func TestValidSchemes_AllWrapped(t *testing.T) {
	require.NotEmpty(t, enode.ValidSchemes)
	for name, scheme := range enode.ValidSchemes {
		require.IsType(t, validRecordScheme{}, scheme, name)
	}
}

func TestValidRecordScheme(t *testing.T) {
	for _, key := range []ENRKey{KeyDomainType, KeyNextDomainType} {
		for n := range 6 {
			raw, err := rlp.EncodeToBytes(bytes.Repeat([]byte{0x01}, n))
			require.NoError(t, err)

			_, err = enode.New(enode.ValidSchemes, recordWithEntry(t, key, raw))

			if n == len(spectypes.DomainType{}) {
				require.NoError(t, err, "key=%s len=%d", key, n)
				continue
			}
			require.ErrorIs(t, err, ErrInvalidDomainTypeEntry, "key=%s len=%d", key, n)
		}
	}

	// A record with neither key is accepted.
	_, err := enode.New(enode.ValidSchemes, recordWithEntry(t, "other", rlp.RawValue{0x80}))
	require.NoError(t, err)
}

func TestValidRecordScheme_CountsAndLogsRejections(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	rejectLogMu.Lock()
	lastRejectLog, rejectsNotLogged = time.Time{}, 0
	rejectLogMu.Unlock()

	valid, err := rlp.EncodeToBytes(bytes.Repeat([]byte{0x01}, 4))
	require.NoError(t, err)
	invalid, err := rlp.EncodeToBytes([]byte{0x01, 0x02})
	require.NoError(t, err)

	for _, key := range []ENRKey{KeyDomainType, KeyNextDomainType} {
		before := recordsRejected(t, key)

		_, err := enode.New(enode.ValidSchemes, recordWithEntry(t, key, valid))
		require.NoError(t, err)
		require.Equal(t, before, recordsRejected(t, key), "valid record counted, key=%s", key)

		_, err = enode.New(enode.ValidSchemes, recordWithEntry(t, key, invalid))
		require.ErrorIs(t, err, ErrInvalidDomainTypeEntry)
		require.Equal(t, before+1, recordsRejected(t, key), "invalid record not counted, key=%s", key)
	}

	// Two rejections within rejectLogInterval: the first is logged, the second
	// only counted as not logged.
	require.Len(t, logs.All(), 1)
	require.Equal(t, string(KeyDomainType), logs.All()[0].ContextMap()["enr_key"])
	rejectLogMu.Lock()
	require.EqualValues(t, 1, rejectsNotLogged)
	lastRejectLog = time.Now().Add(-rejectLogInterval)
	rejectLogMu.Unlock()

	rec := recordWithEntry(t, KeyDomainType, invalid)
	unchecked, err := enode.New(enode.V4ID{}, rec)
	require.NoError(t, err)
	_, err = enode.New(enode.ValidSchemes, rec)
	require.ErrorIs(t, err, ErrInvalidDomainTypeEntry)
	require.Len(t, logs.All(), 2)
	require.Equal(t, zapcore.DebugLevel, logs.All()[1].Level)
	require.Equal(t, unchecked.ID().String(), logs.All()[1].ContextMap()["node_id"])
	require.EqualValues(t, 1, logs.All()[1].ContextMap()["rejects_not_logged"])
	rejectLogMu.Lock()
	require.Zero(t, rejectsNotLogged, "skipped count not reset after logging")
	rejectLogMu.Unlock()
}

// recordsRejected returns the package-global rejected-records counter's total
// for key, read via pkgTestMetricReader; 0 if no record was rejected for it yet.
func recordsRejected(t *testing.T, key ENRKey) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, pkgTestMetricReader.Collect(t.Context(), &rm))

	keyAttr := rejectedKeyAttribute(key)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != observability.InstrumentName(observabilityNamespace, "rejected") {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "expected Sum[int64], got %T", m.Data)
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value(keyAttr.Key); ok && v == keyAttr.Value {
					return dp.Value
				}
			}
		}
	}
	return 0
}
