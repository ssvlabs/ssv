package validator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	bls12381 "github.com/drand/kyber-bls12381"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// ---- test fakes -------------------------------------------------------

// fakeIBEStore is a minimal in-memory ibeShareStore for orchestrator tests.
type fakeIBEStore struct {
	mu      sync.Mutex
	records map[[32]byte]*ekm.IBEShareRecord
}

func newFakeIBEStore() *fakeIBEStore {
	return &fakeIBEStore{records: make(map[[32]byte]*ekm.IBEShareRecord)}
}

func (s *fakeIBEStore) GetIBEShareBytes(cid [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[cid]
	if !ok {
		return nil, ekm.ErrIBEShareNotFound
	}
	return append([]byte(nil), rec.ShareBytes...), nil
}

func (s *fakeIBEStore) GetClusterIBEPubKey(cid [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[cid]
	if !ok {
		return nil, ekm.ErrIBEShareNotFound
	}
	return append([]byte(nil), rec.ClusterIBEPubKey...), nil
}

func (s *fakeIBEStore) GetClusterIBEPolyCommits(cid [32]byte) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[cid]
	if !ok {
		return nil, ekm.ErrIBEShareNotFound
	}
	out := make([][]byte, len(rec.PolyCommits))
	for i, b := range rec.PolyCommits {
		out[i] = append([]byte(nil), b...)
	}
	return out, nil
}

func (s *fakeIBEStore) AddIBEShare(
	cid [32]byte,
	generation uint64,
	shareBytes []byte,
	clusterIBEPubKey []byte,
	polyCommits [][]byte,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	commits := make([][]byte, len(polyCommits))
	for i, b := range polyCommits {
		commits[i] = append([]byte(nil), b...)
	}
	s.records[cid] = &ekm.IBEShareRecord{
		Generation:       generation,
		ShareBytes:       append([]byte(nil), shareBytes...),
		ClusterIBEPubKey: append([]byte(nil), clusterIBEPubKey...),
		PolyCommits:      commits,
	}
	return nil
}

func (s *fakeIBEStore) RemoveIBEShare(cid [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, cid)
	return nil
}

// orchestratorSigner is a minimal OperatorSigner stub.
type orchestratorSigner struct {
	id spectypes.OperatorID
}

func (s *orchestratorSigner) SignSSVMessage(msg *spectypes.SSVMessage) ([]byte, error) {
	encoded, err := msg.Encode()
	if err != nil {
		return nil, err
	}
	out := []byte{byte(s.id), byte(len(encoded))}
	if len(encoded) >= 8 {
		out = append(out, encoded[:8]...)
	}
	return out, nil
}

func (s *orchestratorSigner) GetOperatorID() spectypes.OperatorID {
	return s.id
}

// orchestratorBus fans broadcasts out to peers' Receive functions.
type orchestratorBus struct {
	mu       sync.Mutex
	receives map[spectypes.OperatorID]func([]byte) error
}

func newOrchestratorBus() *orchestratorBus {
	return &orchestratorBus{receives: make(map[spectypes.OperatorID]func([]byte) error)}
}

func (b *orchestratorBus) register(op spectypes.OperatorID, recv func([]byte) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.receives[op] = recv
}

func (b *orchestratorBus) broadcasterFor(owner spectypes.OperatorID) *orchestratorBroadcaster {
	return &orchestratorBroadcaster{bus: b, owner: owner}
}

type orchestratorBroadcaster struct {
	bus   *orchestratorBus
	owner spectypes.OperatorID
}

func (b *orchestratorBroadcaster) Broadcast(_ spectypes.MessageID, m *spectypes.SignedSSVMessage) error {
	b.bus.mu.Lock()
	targets := make([]func([]byte) error, 0, len(b.bus.receives)-1)
	for id, recv := range b.bus.receives {
		if id == b.owner {
			continue
		}
		targets = append(targets, recv)
	}
	b.bus.mu.Unlock()
	for _, recv := range targets {
		_ = recv(m.SSVMessage.Data)
	}
	return nil
}

// ---- tests ------------------------------------------------------------

func sampleClusterID(seed byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = seed
	}
	return out
}

func newOrchestratorForTest(t *testing.T, opID spectypes.OperatorID, bus *orchestratorBus, store *fakeIBEStore) *DKGOrchestrator {
	t.Helper()
	o, err := NewDKGOrchestrator(DKGOrchestratorOptions{
		Logger:     zaptest.NewLogger(t).Named(fmt.Sprintf("op%d", opID)),
		OperatorID: opID,
		Domain:     spectypes.DomainType{0xa0, 0xa1, 0xa2, 0xa3},
		Suite:      bls12381.NewBLS12381Suite(),
		Network:    bus.broadcasterFor(opID),
		Signer:     &orchestratorSigner{id: opID},
		Store:      store,
	})
	require.NoError(t, err)
	bus.register(opID, o.Receive)
	return o
}

func TestNewDKGOrchestrator_Validation(t *testing.T) {
	bus := newOrchestratorBus()
	store := newFakeIBEStore()
	suite := bls12381.NewBLS12381Suite()

	_, err := NewDKGOrchestrator(DKGOrchestratorOptions{})
	require.Error(t, err)

	_, err = NewDKGOrchestrator(DKGOrchestratorOptions{
		Suite: suite,
	})
	require.Error(t, err)

	_, err = NewDKGOrchestrator(DKGOrchestratorOptions{
		Suite:   suite,
		Network: bus.broadcasterFor(1),
	})
	require.Error(t, err)

	_, err = NewDKGOrchestrator(DKGOrchestratorOptions{
		Suite:   suite,
		Network: bus.broadcasterFor(1),
		Signer:  &orchestratorSigner{id: 1},
	})
	require.Error(t, err)

	o, err := NewDKGOrchestrator(DKGOrchestratorOptions{
		Suite:   suite,
		Network: bus.broadcasterFor(1),
		Signer:  &orchestratorSigner{id: 1},
		Store:   store,
	})
	require.NoError(t, err)
	require.NotNil(t, o)
}

func TestOrchestrator_EnsureClusterIBE_FullCluster(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	bus := newOrchestratorBus()
	stores := make(map[spectypes.OperatorID]*fakeIBEStore, len(committee))
	orchestrators := make(map[spectypes.OperatorID]*DKGOrchestrator, len(committee))
	for _, opID := range committee {
		stores[opID] = newFakeIBEStore()
		orchestrators[opID] = newOrchestratorForTest(t, opID, bus, stores[opID])
	}

	clusterID := sampleClusterID(0xab)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(map[spectypes.OperatorID]error)
	var errsMu sync.Mutex
	for _, opID := range committee {
		opID := opID
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := orchestrators[opID].EnsureClusterIBE(ctx, clusterID, committee, 0)
			if err != nil {
				errsMu.Lock()
				errs[opID] = err
				errsMu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Empty(t, errs, "all operators should complete DKG")

	// All operators should have the same ClusterIBEPubKey.
	var refPub []byte
	for opID, store := range stores {
		pub, err := store.GetClusterIBEPubKey(clusterID)
		require.NoError(t, err, "op %d", opID)
		if refPub == nil {
			refPub = pub
		} else {
			require.Equal(t, refPub, pub, "op %d pubkey diverges", opID)
		}
	}
}

func TestOrchestrator_EnsureClusterIBE_Idempotent(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	bus := newOrchestratorBus()
	stores := make(map[spectypes.OperatorID]*fakeIBEStore, len(committee))
	orchestrators := make(map[spectypes.OperatorID]*DKGOrchestrator, len(committee))
	for _, opID := range committee {
		stores[opID] = newFakeIBEStore()
		orchestrators[opID] = newOrchestratorForTest(t, opID, bus, stores[opID])
	}

	clusterID := sampleClusterID(0xcd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First run.
	var wg sync.WaitGroup
	for _, opID := range committee {
		opID := opID
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, orchestrators[opID].EnsureClusterIBE(ctx, clusterID, committee, 0))
		}()
	}
	wg.Wait()

	// Second EnsureClusterIBE on a single operator should be a no-op
	// (cheap; doesn't broadcast or run a ceremony). We can verify by
	// timing: idempotent path returns within milliseconds.
	start := time.Now()
	require.NoError(t, orchestrators[1].EnsureClusterIBE(ctx, clusterID, committee, 0))
	elapsed := time.Since(start)
	require.Less(t, elapsed, 100*time.Millisecond, "idempotent path should return fast (got %v)", elapsed)
}

func TestOrchestrator_Receive_RejectsMalformed(t *testing.T) {
	bus := newOrchestratorBus()
	store := newFakeIBEStore()
	o := newOrchestratorForTest(t, 1, bus, store)

	require.Error(t, o.Receive(nil))
	require.Error(t, o.Receive([]byte{}))

	// Garbage envelope returns a decode error (not a panic).
	require.Error(t, o.Receive([]byte{0xff, 0xff, 0xff}))
}

func TestOrchestrator_RemoveClusterIBE(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	bus := newOrchestratorBus()
	stores := make(map[spectypes.OperatorID]*fakeIBEStore, len(committee))
	orchestrators := make(map[spectypes.OperatorID]*DKGOrchestrator, len(committee))
	for _, opID := range committee {
		stores[opID] = newFakeIBEStore()
		orchestrators[opID] = newOrchestratorForTest(t, opID, bus, stores[opID])
	}

	clusterID := sampleClusterID(0xee)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run DKG so each store has a record to remove.
	var wg sync.WaitGroup
	for _, opID := range committee {
		opID := opID
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, orchestrators[opID].EnsureClusterIBE(ctx, clusterID, committee, 0))
		}()
	}
	wg.Wait()

	// All stores have a share for clusterID at this point.
	for opID, store := range stores {
		_, err := store.GetIBEShareBytes(clusterID)
		require.NoError(t, err, "op %d should have a share before removal", opID)
	}

	// RemoveClusterIBE on each orchestrator removes the share from each store.
	for opID, o := range orchestrators {
		require.NoError(t, o.RemoveClusterIBE(clusterID), "op %d removal", opID)
		_, err := stores[opID].GetIBEShareBytes(clusterID)
		require.ErrorIs(t, err, ekm.ErrIBEShareNotFound, "op %d share should be gone", opID)
	}

	// Idempotent: second remove is a no-op.
	require.NoError(t, orchestrators[1].RemoveClusterIBE(clusterID))

	// After removal, EnsureClusterIBE re-runs DKG (the orchestrator's
	// idempotency check sees no share, so the ceremony fires again).
	wg = sync.WaitGroup{}
	for _, opID := range committee {
		opID := opID
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, orchestrators[opID].EnsureClusterIBE(ctx, clusterID, committee, 1))
		}()
	}
	wg.Wait()
	for opID, store := range stores {
		_, err := store.GetIBEShareBytes(clusterID)
		require.NoError(t, err, "op %d should have a share after re-DKG", opID)
	}
}

func TestIBEThresholdForCommitteeSize(t *testing.T) {
	cases := []struct {
		n        int
		expected int
	}{
		{4, 2},   // f=1, qEnc=2
		{7, 3},   // f=2, qEnc=3
		{10, 4},  // f=3, qEnc=4
		{13, 5},  // f=4, qEnc=5
		{3, 0},   // below SSV's minimum cluster size
		{0, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.expected, ibeThresholdForCommitteeSize(c.n), "n=%d", c.n)
	}
}
