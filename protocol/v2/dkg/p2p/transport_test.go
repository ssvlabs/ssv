package p2p

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	bls12381 "github.com/drand/kyber-bls12381"
	kyber_dkg "github.com/drand/kyber/share/dkg"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	dkgcore "github.com/ssvlabs/ssv/protocol/v2/dkg"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

// ---- test fakes -------------------------------------------------------

type captureNetwork struct {
	mu       sync.Mutex
	captured []*spectypes.SignedSSVMessage
	err      error // returned from Broadcast if non-nil
}

func (n *captureNetwork) Broadcast(_ spectypes.MessageID, m *spectypes.SignedSSVMessage) error {
	if n.err != nil {
		return n.err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.captured = append(n.captured, m)
	return nil
}

func (n *captureNetwork) snapshot() []*spectypes.SignedSSVMessage {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*spectypes.SignedSSVMessage, len(n.captured))
	copy(out, n.captured)
	return out
}

type fakeSigner struct {
	id     spectypes.OperatorID
	prefix string
}

func (s *fakeSigner) SignSSVMessage(msg *spectypes.SSVMessage) ([]byte, error) {
	encoded, err := msg.Encode()
	if err != nil {
		return nil, err
	}
	// Toy signature: prefix-tag + length + first 8 bytes of the encoded msg.
	// Enough to be distinguishable across signers; not cryptographically valid.
	out := append([]byte(s.prefix), byte(len(encoded)))
	if len(encoded) >= 8 {
		out = append(out, encoded[:8]...)
	}
	return out, nil
}

func (s *fakeSigner) GetOperatorID() spectypes.OperatorID {
	return s.id
}

func sampleMsgID() spectypes.MessageID {
	var out spectypes.MessageID
	for i := range out {
		out[i] = byte(i + 1)
	}
	return out
}

// ---- unit tests -------------------------------------------------------

func TestNew_Validation(t *testing.T) {
	_, err := New(Options{})
	require.Error(t, err)

	_, err = New(Options{Network: &captureNetwork{}})
	require.Error(t, err)

	tx, err := New(Options{
		Network: &captureNetwork{},
		Signer:  &fakeSigner{id: 1, prefix: "s1:"},
		MsgID:   sampleMsgID(),
	})
	require.NoError(t, err)
	require.NotNil(t, tx)
}

func TestTransport_BroadcastShape(t *testing.T) {
	net := &captureNetwork{}
	signer := &fakeSigner{id: 7, prefix: "op7:"}
	msgID := sampleMsgID()

	tx, err := New(Options{Network: net, Signer: signer, MsgID: msgID})
	require.NoError(t, err)

	body := []byte("envelope-bytes")
	require.NoError(t, tx.Broadcast(body))

	captured := net.snapshot()
	require.Len(t, captured, 1)
	got := captured[0]

	require.Equal(t, ssvmessage.SSVDKGMsgType, got.SSVMessage.MsgType)
	require.Equal(t, msgID, got.SSVMessage.MsgID)
	require.Equal(t, body, got.SSVMessage.Data)
	require.Equal(t, []spectypes.OperatorID{7}, got.OperatorIDs)
	require.Len(t, got.Signatures, 1)
	require.NotEmpty(t, got.Signatures[0])
}

func TestTransport_BroadcastEmpty(t *testing.T) {
	tx, _ := New(Options{
		Network: &captureNetwork{},
		Signer:  &fakeSigner{id: 1, prefix: "s:"},
		MsgID:   sampleMsgID(),
	})
	require.Error(t, tx.Broadcast(nil))
	require.Error(t, tx.Broadcast([]byte{}))
}

func TestTransport_BroadcastNetworkError(t *testing.T) {
	net := &captureNetwork{err: errors.New("publish failed")}
	tx, _ := New(Options{
		Network: net,
		Signer:  &fakeSigner{id: 1, prefix: "s:"},
		MsgID:   sampleMsgID(),
	})
	err := tx.Broadcast([]byte("hello"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "publish failed")
}

func TestTransport_DeliverInbox(t *testing.T) {
	tx, _ := New(Options{
		Network: &captureNetwork{},
		Signer:  &fakeSigner{id: 1, prefix: "s:"},
		MsgID:   sampleMsgID(),
	})
	body := []byte("envelope-bytes")
	require.NoError(t, tx.Deliver(body))

	select {
	case got := <-tx.Inbox():
		require.Equal(t, body, got)
	case <-time.After(time.Second):
		t.Fatal("expected envelope on inbox")
	}
}

func TestTransport_DeliverEmpty(t *testing.T) {
	tx, _ := New(Options{
		Network: &captureNetwork{},
		Signer:  &fakeSigner{id: 1, prefix: "s:"},
		MsgID:   sampleMsgID(),
	})
	require.Error(t, tx.Deliver(nil))
	require.Error(t, tx.Deliver([]byte{}))
}

func TestTransport_DeliverFull(t *testing.T) {
	tx, _ := New(Options{
		Network:     &captureNetwork{},
		Signer:      &fakeSigner{id: 1, prefix: "s:"},
		MsgID:       sampleMsgID(),
		InboxBuffer: 2,
	})
	require.NoError(t, tx.Deliver([]byte("a")))
	require.NoError(t, tx.Deliver([]byte("b")))

	err := tx.Deliver([]byte("c"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "full")
}

// ---- end-to-end DKG over P2P transports -------------------------------
//
// Wires n=4 operators through P2PTransports backed by a shared dkgBus.
// Each operator's broadcaster view fans out to every other registered
// operator's Deliver. Proves the new transport plumbing works under a
// real Coordinator run, not just at the unit level.

// dkgBus owns the operator → deliver-fn registry. Per-operator
// Broadcaster views (busBroadcaster) reference the bus through a single
// pointer; the bus's mutex is the only thing serializing the map.
type dkgBus struct {
	mu       sync.Mutex
	delivers map[spectypes.OperatorID]func([]byte)
}

func newDKGBus() *dkgBus {
	return &dkgBus{delivers: make(map[spectypes.OperatorID]func([]byte))}
}

func (b *dkgBus) register(op spectypes.OperatorID, deliver func([]byte)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.delivers[op] = deliver
}

func (b *dkgBus) broadcasterFor(owner spectypes.OperatorID) *busBroadcaster {
	return &busBroadcaster{bus: b, owner: owner}
}

type busBroadcaster struct {
	bus   *dkgBus
	owner spectypes.OperatorID
}

func (b *busBroadcaster) Broadcast(_ spectypes.MessageID, m *spectypes.SignedSSVMessage) error {
	b.bus.mu.Lock()
	targets := make([]func([]byte), 0, len(b.bus.delivers)-1)
	for id, deliver := range b.bus.delivers {
		if id == b.owner {
			continue
		}
		targets = append(targets, deliver)
	}
	b.bus.mu.Unlock()
	for _, deliver := range targets {
		deliver(m.SSVMessage.Data)
	}
	return nil
}

func TestTransport_EndToEnd_DKG_4Of4(t *testing.T) {
	committee := []uint64{1, 2, 3, 4}
	threshold := 2 // f+1 for n=4

	bus := newDKGBus()
	transports := make(map[uint64]*Transport, len(committee))
	for _, opID := range committee {
		op := spectypes.OperatorID(opID)
		tx, err := New(Options{
			Network: bus.broadcasterFor(op),
			Signer:  &fakeSigner{id: op, prefix: "op:"},
			MsgID:   sampleMsgID(),
		})
		require.NoError(t, err)
		transports[opID] = tx
		bus.register(op, func(env []byte) { _ = tx.Deliver(env) })
	}

	suite := bls12381.NewBLS12381Suite()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := make(map[uint64]*kyber_dkg.DistKeyShare)
	errs := make(map[uint64]error)
	var resultsMu sync.Mutex
	var wg sync.WaitGroup
	for _, opID := range committee {
		opID := opID
		wg.Add(1)
		go func() {
			defer wg.Done()
			coord, err := dkgcore.NewCoordinator(dkgcore.CoordinatorOpts{
				Logger:          zaptest.NewLogger(t),
				OperatorID:      opID,
				Suite:           suite,
				Transport:       transports[opID],
				ExchangeTimeout: 5 * time.Second,
				PhaserPeriod:    1 * time.Second,
			})
			require.NoError(t, err)

			var clusterID [32]byte
			for i := range clusterID {
				clusterID[i] = byte(0xb0 | (i & 0x0f))
			}
			share, err := coord.Run(ctx, clusterID, committee, threshold, 0)

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

	require.Empty(t, errs, "all operators should succeed")
	require.Len(t, results, len(committee))

	// All Commits[0] equal — same cluster IBE pubkey across operators.
	var ref *kyber_dkg.DistKeyShare
	for _, r := range results {
		if ref == nil {
			ref = r
			continue
		}
		require.True(t, ref.Commits[0].Equal(r.Commits[0]))
	}
}
