package dkg

import (
	"context"
	"sync"
	"testing"
	"time"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/share"
	kyber_dkg "github.com/drand/kyber/share/dkg"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
)

// inMemBus is a synthetic broadcast bus that fans out each operator's
// outbound bytes to every other operator's inbox. Used by Phase B unit
// tests; production wires SSV's P2P broadcaster behind the Transport
// interface in Phase C.
type inMemBus struct {
	mu      sync.Mutex
	inboxes map[uint64]chan []byte
}

func newInMemBus() *inMemBus {
	return &inMemBus{inboxes: make(map[uint64]chan []byte)}
}

// register adds an operator to the bus and returns a Transport bound to
// that operator. Calling Broadcast on the returned Transport delivers to
// every other registered operator's inbox.
func (b *inMemBus) register(operatorID uint64) Transport {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 256)
	b.inboxes[operatorID] = ch
	return &busTransport{bus: b, owner: operatorID, inbox: ch}
}

func (b *inMemBus) deliver(from uint64, payload []byte) {
	b.mu.Lock()
	targets := make([]chan []byte, 0, len(b.inboxes)-1)
	for id, ch := range b.inboxes {
		if id == from {
			continue
		}
		targets = append(targets, ch)
	}
	b.mu.Unlock()
	for _, ch := range targets {
		select {
		case ch <- payload:
		default:
			// Drop on full inbox — tests size buffers generously, so
			// this only fires if a peer is offline (test asserts that
			// scenario explicitly).
		}
	}
}

type busTransport struct {
	bus   *inMemBus
	owner uint64
	inbox chan []byte
}

func (t *busTransport) Broadcast(env []byte) error {
	cp := append([]byte(nil), env...)
	t.bus.deliver(t.owner, cp)
	return nil
}

func (t *busTransport) Inbox() <-chan []byte {
	return t.inbox
}

// runCluster spins up `len(committee)` Coordinators wired through an
// in-memory bus, calls Run on all of them concurrently, and collects
// results. Returns the per-operator DistKeyShare map and any per-operator
// errors.
func runCluster(
	t *testing.T,
	ctx context.Context,
	committee []uint64,
	threshold int,
	clusterID [32]byte,
	generation uint64,
	online []uint64, // subset of committee that actually participates; nil = all online
) (map[uint64]*kyber_dkg.DistKeyShare, map[uint64]error) {
	t.Helper()
	bus := newInMemBus()

	suite := bls12381.NewBLS12381Suite()
	results := make(map[uint64]*kyber_dkg.DistKeyShare)
	errs := make(map[uint64]error)
	var resultsMu sync.Mutex

	if online == nil {
		online = committee
	}

	// Pre-register every online operator on the bus before any goroutine
	// starts broadcasting. If we registered inside the goroutine, a fast
	// goroutine could broadcast its Exchange before slower goroutines
	// have registered their inbox, losing those messages.
	transports := make(map[uint64]Transport, len(online))
	for _, opID := range online {
		transports[opID] = bus.register(opID)
	}

	var wg sync.WaitGroup
	for _, opID := range online {
		opID := opID
		tx := transports[opID]
		wg.Add(1)
		go func() {
			defer wg.Done()
			coord, err := NewCoordinator(CoordinatorOpts{
				Logger:          zaptest.NewLogger(t).Named("op").With(),
				OperatorID:      opID,
				Suite:           suite,
				Transport:       tx,
				ExchangeTimeout: 5 * time.Second,
				PhaserPeriod:    1 * time.Second,
			})
			require.NoError(t, err)
			share, err := coord.Run(ctx, clusterID, committee, threshold, generation)
			resultsMu.Lock()
			defer resultsMu.Unlock()
			if err != nil {
				errs[opID] = err
				return
			}
			results[opID] = share
		}()
	}
	wg.Wait()
	return results, errs
}

func cidFor(seed byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = seed
	}
	return out
}

// TestCoordinator_HappyPath_7Of7 — the canonical happy path. Seven
// operators, threshold 3 (= f+1 for f=2), all honest, all online. DKG
// completes; every operator's Commits[0] is the same point.
func TestCoordinator_HappyPath_7Of7(t *testing.T) {
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, errs := runCluster(t, ctx, committee, 3, cidFor(0xa1), 0, nil)
	require.Empty(t, errs, "all operators should succeed")
	require.Len(t, results, len(committee))

	// All Commits[0] must be the same point — that IS the cluster IBE
	// pubkey, computed identically by every operator from their share
	// of the same polynomial.
	var ref *kyber_dkg.DistKeyShare
	for _, r := range results {
		if ref == nil {
			ref = r
			continue
		}
		require.True(t, ref.Commits[0].Equal(r.Commits[0]), "Commits[0] must match across operators")
	}
}

// TestCoordinator_ThresholdProperty — any threshold-sized subset of the
// resulting shares Lagrange-interpolates to the same master scalar
// (= s such that Commits[0] = s · G). This is THE property that makes
// the IBE keypair usable as a threshold trust anchor.
func TestCoordinator_ThresholdProperty(t *testing.T) {
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	threshold := 3
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, errs := runCluster(t, ctx, committee, threshold, cidFor(0xa2), 0, nil)
	require.Empty(t, errs)
	require.Len(t, results, len(committee))

	suite := bls12381.NewBLS12381Suite()
	g1 := suite.G1().(kyber_dkg.Suite)

	// Take exactly `threshold` shares (operators 1..3) and reconstruct
	// the master scalar via Lagrange interpolation. The reconstructed
	// scalar's public point must equal Commits[0].
	subset := make([]*share.PriShare, 0, threshold)
	for _, opID := range committee[:threshold] {
		subset = append(subset, results[opID].Share)
	}
	masterScalar, err := share.RecoverSecret(g1, subset, threshold, len(committee))
	require.NoError(t, err)

	expectedPub := results[committee[0]].Commits[0]
	gotPub := g1.Point().Mul(masterScalar, nil)
	require.True(t, expectedPub.Equal(gotPub), "Lagrange-recovered scalar must match the cluster IBE pubkey")
}

// TestCoordinator_BelowThreshold — fewer than `threshold` shares cannot
// recover the master. Lagrange interpolation requires exactly `threshold`
// distinct points; with fewer, kyber's RecoverSecret returns an error.
func TestCoordinator_BelowThreshold(t *testing.T) {
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	threshold := 3
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, errs := runCluster(t, ctx, committee, threshold, cidFor(0xa3), 0, nil)
	require.Empty(t, errs)

	suite := bls12381.NewBLS12381Suite()
	g1 := suite.G1().(kyber_dkg.Suite)

	// Two shares < threshold of 3.
	subset := []*share.PriShare{
		results[committee[0]].Share,
		results[committee[1]].Share,
	}
	_, err := share.RecoverSecret(g1, subset, threshold, len(committee))
	require.Error(t, err, "below-threshold reconstruction must fail")
}

// TestCoordinator_LivenessLimit — when more than (n - threshold) operators
// are offline, no operator can complete the exchange phase (we wait for
// every committee member). Each surviving operator times out cleanly.
func TestCoordinator_LivenessLimit(t *testing.T) {
	threshold := 3
	// Committee is 7; 5 of 7 are online (2 offline). Even though
	// threshold (3) is reachable arithmetically, the exchange phase
	// requires ALL committee members; this test asserts the surviving
	// operators time out cleanly rather than hang.
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	online := []uint64{1, 2, 3, 4, 5}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Tighten the per-coordinator exchange timeout so the test is fast.
	bus := newInMemBus()
	suite := bls12381.NewBLS12381Suite()
	errs := make(map[uint64]error)
	transports := make(map[uint64]Transport, len(online))
	for _, opID := range online {
		transports[opID] = bus.register(opID)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, opID := range online {
		opID := opID
		tx := transports[opID]
		wg.Add(1)
		go func() {
			defer wg.Done()
			coord, err := NewCoordinator(CoordinatorOpts{
				Logger:          zaptest.NewLogger(t),
				OperatorID:      opID,
				Suite:           suite,
				Transport:       tx,
				ExchangeTimeout: 500 * time.Millisecond,
				PhaserPeriod:    1 * time.Second,
			})
			require.NoError(t, err)
			_, err = coord.Run(ctx, cidFor(0xa4), committee, threshold, 0)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[opID] = err
			}
		}()
	}
	wg.Wait()

	require.Len(t, errs, len(online), "all online operators should error out cleanly")
	for opID, err := range errs {
		require.Contains(t, err.Error(), "exchange", "op %d: expected exchange-phase error, got %v", opID, err)
	}
}

// TestCoordinator_DKGOutput_KyberSignerRoundTrip — the indexing-alignment
// regression test. Verifies that a DKG-output share, fed into KyberSigner,
// produces partial sigs that aggregate into a valid threshold signature
// under the cluster IBE pubkey (= Commits[0]).
//
// The kyber-DKG node Index, kyber's PubPoly.Eval x-coord (= 1+Index), and
// KyberSigner.AggregatePartials Lagrange x-coord (= operator ID) must all
// align — Index = opID-1 places each share at x = opID, matching what
// KyberSigner expects. If that alignment ever drifts, VerifyAggregate
// here fails and this test goes red before downstream integration paths
// silently produce wrong threshold signatures.
func TestCoordinator_DKGOutput_KyberSignerRoundTrip(t *testing.T) {
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	threshold := 3 // f+1 for n=7

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, errs := runCluster(t, ctx, committee, threshold, cidFor(0xc1), 0, nil)
	require.Empty(t, errs)
	require.Len(t, results, len(committee))

	// Each operator's KyberSigner consumes the marshaled Share.V bytes —
	// the same shape Phase E3's setup_tbft.go would produce from the
	// IBEShareWriter persistence. Sign a fixed test tag with each
	// operator and aggregate any threshold-sized subset.
	tag := []byte("dkg-roundtrip-tag/v1")
	signer := blsbackend.NewKyberSigner(nil) // verify-only base instance

	partials := make(map[tbftcore.OperatorID]tbftcore.Signature, len(committee))
	for _, opID := range committee {
		shareBytes, err := results[opID].Share.V.MarshalBinary()
		require.NoError(t, err)
		opSigner := blsbackend.NewKyberSigner(shareBytes)
		sig, err := opSigner.SignPartial(tag)
		require.NoError(t, err)
		partials[tbftcore.OperatorID(opID)] = sig
	}

	// Take the first `threshold` partials and aggregate.
	subset := make(map[tbftcore.OperatorID]tbftcore.Signature, threshold)
	for i, opID := range committee[:threshold] {
		subset[tbftcore.OperatorID(opID)] = partials[tbftcore.OperatorID(opID)]
		_ = i
	}
	aggregate, err := signer.AggregatePartials(subset)
	require.NoError(t, err)

	// Cluster IBE pubkey = Commits[0], in kyber-G1 marshaled form. The
	// KyberSigner's VerifyAggregate accepts kyber-format pubkey bytes
	// directly (HerumiPubkeyToKyberG1Point round-trips byte-equally).
	pubKeyBytes, err := results[committee[0]].Commits[0].MarshalBinary()
	require.NoError(t, err)
	require.True(t, signer.VerifyAggregate(pubKeyBytes, tag, aggregate),
		"DKG share + KyberSigner threshold sig must verify against Commits[0]")

	// And: a different threshold-sized subset must produce a byte-
	// equivalent aggregate (Lagrange interpolation is subset-invariant).
	subset2 := make(map[tbftcore.OperatorID]tbftcore.Signature, threshold)
	for _, opID := range committee[len(committee)-threshold:] {
		subset2[tbftcore.OperatorID(opID)] = partials[tbftcore.OperatorID(opID)]
	}
	aggregate2, err := signer.AggregatePartials(subset2)
	require.NoError(t, err)
	require.Equal(t, aggregate, aggregate2,
		"different threshold subsets must produce identical aggregate signatures")
}
