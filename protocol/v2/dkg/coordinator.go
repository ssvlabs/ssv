package dkg

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/drand/kyber/pairing"
	kyber_dkg "github.com/drand/kyber/share/dkg"
	"github.com/drand/kyber/sign/bdn"
	"go.uber.org/zap"

	dkgwire "github.com/ssvlabs/ssv/protocol/v2/dkg/wire"
)

// Default timing parameters. Production callers can override via
// CoordinatorOpts.
const (
	defaultExchangeTimeout = 30 * time.Second
	defaultPhaserPeriod    = 2 * time.Second
)

// CoordinatorOpts configures a Coordinator. OperatorID, Suite and
// Transport are required; the rest have sensible defaults.
type CoordinatorOpts struct {
	Logger          *zap.Logger
	OperatorID      uint64
	Suite           pairing.Suite
	Transport       Transport
	ExchangeTimeout time.Duration
	PhaserPeriod    time.Duration
}

// Coordinator drives one operator's participation in DKG ceremonies for
// any number of clusters. Each call to Run executes a single ceremony
// to completion or error.
//
// One Coordinator instance per SSV node (not per cluster). Concurrent
// Run calls are safe — they don't share state with each other.
type Coordinator struct {
	log             *zap.Logger
	operatorID      uint64
	suite           pairing.Suite
	dkgSuite        kyber_dkg.Suite // suite.G1() type-asserted once at construction
	transport       Transport
	exchangeTimeout time.Duration
	phaserPeriod    time.Duration
}

// NewCoordinator constructs a Coordinator from the given options.
func NewCoordinator(opts CoordinatorOpts) (*Coordinator, error) {
	if opts.Suite == nil {
		return nil, errors.New("dkg: nil suite")
	}
	if opts.Transport == nil {
		return nil, errors.New("dkg: nil transport")
	}
	if opts.OperatorID == 0 {
		return nil, errors.New("dkg: operator id must be > 0 (kyber Index = id - 1)")
	}
	dkgSuite, ok := opts.Suite.G1().(kyber_dkg.Suite)
	if !ok {
		return nil, errors.New("dkg: suite.G1() does not implement kyber dkg.Suite")
	}
	log := opts.Logger
	if log == nil {
		log = zap.NewNop()
	}
	exchangeTimeout := opts.ExchangeTimeout
	if exchangeTimeout == 0 {
		exchangeTimeout = defaultExchangeTimeout
	}
	phaserPeriod := opts.PhaserPeriod
	if phaserPeriod == 0 {
		phaserPeriod = defaultPhaserPeriod
	}
	return &Coordinator{
		log:             log,
		operatorID:      opts.OperatorID,
		suite:           opts.Suite,
		dkgSuite:        dkgSuite,
		transport:       opts.Transport,
		exchangeTimeout: exchangeTimeout,
		phaserPeriod:    phaserPeriod,
	}, nil
}

// Run executes one DKG ceremony for the given (clusterID, generation,
// committee, threshold) parameters and returns the kyber DistKeyShare on
// success.
//
// Sequence:
//
//  1. Generate a fresh kyber long-term keypair for this ceremony.
//  2. Broadcast our Exchange message with the kyber pubkey.
//  3. Wait until an Exchange has arrived from every committee member
//     (including ourselves) or the exchange timeout fires.
//  4. Build kyber's `dkg.Config.NewNodes` from the collected pubkeys.
//  5. Construct a kyber DKG Protocol in FastSync mode.
//  6. Launch a goroutine that pumps inbound DKG envelopes from the
//     transport into the Board.
//  7. Wait for the protocol to finish via Protocol.WaitEnd().
//  8. Return the DistKeyShare.
//
// Error categories: ctx.Done before completion; exchange-phase timeout;
// kyber DKG error (insufficient honest participants, bad inputs, etc).
func (c *Coordinator) Run(
	ctx context.Context,
	clusterID [32]byte,
	committee []uint64,
	threshold int,
	generation uint64,
) (*kyber_dkg.DistKeyShare, error) {
	if len(committee) == 0 {
		return nil, errors.New("dkg: empty committee")
	}
	if threshold < 2 || threshold > len(committee) {
		return nil, fmt.Errorf("dkg: threshold %d out of range [2, %d]", threshold, len(committee))
	}
	if !contains(committee, c.operatorID) {
		return nil, fmt.Errorf("dkg: operator %d not in committee", c.operatorID)
	}

	keypair, err := GenerateKeypair(c.dkgSuite)
	if err != nil {
		return nil, fmt.Errorf("dkg: generate keypair: %w", err)
	}

	exchanges, err := c.runExchangePhase(ctx, clusterID, generation, committee, keypair)
	if err != nil {
		return nil, fmt.Errorf("dkg: exchange phase: %w", err)
	}

	nodes, err := c.buildNodes(exchanges)
	if err != nil {
		return nil, fmt.Errorf("dkg: build nodes: %w", err)
	}

	cfg := &kyber_dkg.Config{
		Suite:     c.dkgSuite,
		Longterm:  keypair.Secret,
		NewNodes:  nodes,
		OldNodes:  nodes, // fresh DKG: old == new
		Threshold: threshold,
		Nonce:     deriveNonce(clusterID, generation),
		Auth:      bdn.NewSchemeOnG2(c.suite),
		FastSync:  true,
	}

	board := newKyberBoard(c.log, c.transport.Broadcast, clusterID, generation)
	phaser := kyber_dkg.NewTimePhaser(c.phaserPeriod)

	proto, err := kyber_dkg.NewProtocol(cfg, board, phaser, false)
	if err != nil {
		return nil, fmt.Errorf("dkg: kyber NewProtocol: %w", err)
	}

	// Run the inbox pump in its own goroutine; cancel + wait on every
	// return path so the goroutine doesn't outlive Run.
	pumpCtx, cancelPump := context.WithCancel(ctx)
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		c.pumpInbox(pumpCtx, board)
	}()
	defer func() {
		cancelPump()
		<-pumpDone
	}()

	go phaser.Start()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-proto.WaitEnd():
		if res.Error != nil {
			return nil, fmt.Errorf("dkg: kyber protocol: %w", res.Error)
		}
		if res.Result == nil || res.Result.Key == nil {
			return nil, errors.New("dkg: kyber returned nil result key")
		}
		return res.Result.Key, nil
	}
}

// runExchangePhase broadcasts our Exchange and waits for one Exchange
// from each committee member. Returns once the exchange map is full
// (one entry per member) or the timeout fires.
func (c *Coordinator) runExchangePhase(
	ctx context.Context,
	clusterID [32]byte,
	generation uint64,
	committee []uint64,
	keypair *Keypair,
) (map[uint64]*dkgwire.Exchange, error) {
	ourPub, err := keypair.Public.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal own pub: %w", err)
	}
	ours := &dkgwire.Exchange{
		ClusterID:  clusterID,
		Generation: generation,
		OperatorID: c.operatorID,
		PubKey:     ourPub,
	}
	body, err := dkgwire.WrapExchange(ours)
	if err != nil {
		return nil, fmt.Errorf("wrap own exchange: %w", err)
	}
	if err := c.transport.Broadcast(body); err != nil {
		return nil, fmt.Errorf("broadcast own exchange: %w", err)
	}

	exchanges := map[uint64]*dkgwire.Exchange{c.operatorID: ours}
	expected := len(committee)
	committeeSet := make(map[uint64]struct{}, expected)
	for _, id := range committee {
		committeeSet[id] = struct{}{}
	}

	timeout := time.NewTimer(c.exchangeTimeout)
	defer timeout.Stop()

	for len(exchanges) < expected {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, fmt.Errorf("exchange timeout: have %d/%d", len(exchanges), expected)
		case raw, ok := <-c.transport.Inbox():
			if !ok {
				return nil, errors.New("transport inbox closed")
			}
			env, err := dkgwire.Unwrap(raw, c.dkgSuite)
			if err != nil {
				c.log.Debug("dkg: drop malformed envelope", zap.Error(err))
				continue
			}
			if env.Kind != dkgwire.KindExchange {
				// DKG-phase messages can arrive before exchange completes
				// if peers progress fast. Drop here; peers will resend
				// or kyber's protocol will recover via FastSync.
				continue
			}
			e := env.Exchange
			if e.ClusterID != clusterID || e.Generation != generation {
				continue
			}
			if _, ok := committeeSet[e.OperatorID]; !ok {
				continue
			}
			if _, dup := exchanges[e.OperatorID]; dup {
				continue
			}
			exchanges[e.OperatorID] = e
		}
	}
	return exchanges, nil
}

// buildNodes constructs kyber's NewNodes list from collected exchanges,
// sorted by Index. Each node's Index is the operator ID minus one
// (kyber expects 0-based indices; SSV operator IDs are 1-based).
func (c *Coordinator) buildNodes(exchanges map[uint64]*dkgwire.Exchange) ([]kyber_dkg.Node, error) {
	nodes := make([]kyber_dkg.Node, 0, len(exchanges))
	for opID, e := range exchanges {
		pt := c.dkgSuite.Point()
		if err := pt.UnmarshalBinary(e.PubKey); err != nil {
			return nil, fmt.Errorf("unmarshal pubkey for op %d: %w", opID, err)
		}
		// Operator IDs in SSV are uint64 but practically small; opID-1
		// must fit in uint32 for kyber's Index type.
		if opID == 0 || opID > uint64(^uint32(0)) {
			return nil, fmt.Errorf("operator id %d out of kyber Index range", opID)
		}
		nodes = append(nodes, kyber_dkg.Node{
			Index:  kyber_dkg.Index(opID - 1), //nolint:gosec // bounds-checked above
			Public: pt,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })
	return nodes, nil
}

// pumpInbox drains the transport's inbox and routes each DKG-phase
// envelope (Deal/Response/Justification) into the Board's channels.
// Returns when ctx is cancelled or the inbox closes.
//
// Exchange envelopes are dropped here — those belong to the exchange
// phase, which has already completed by the time pumpInbox is running.
// (Late Exchanges from peers running slightly behind are harmless — we
// already have everyone's pubkey.)
func (c *Coordinator) pumpInbox(ctx context.Context, board *kyberBoard) {
	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-c.transport.Inbox():
			if !ok {
				return
			}
			env, err := dkgwire.Unwrap(raw, c.dkgSuite)
			if err != nil {
				c.log.Debug("dkg: drop malformed envelope (pump)", zap.Error(err))
				continue
			}
			if env.Kind == dkgwire.KindExchange {
				continue
			}
			if err := board.Receive(env); err != nil {
				c.log.Debug("dkg: drop envelope at board", zap.Error(err))
			}
		}
	}
}

// deriveNonce produces the 32-byte ceremony nonce per D5 of
// docs/TBFT-DKG-TASKS.md: H(clusterID || generation). All operators in
// the same cluster ceremony compute the same nonce; cross-cluster and
// cross-generation replay is structurally prevented.
func deriveNonce(clusterID [32]byte, generation uint64) []byte {
	h := sha256.New()
	h.Write(clusterID[:])
	var gen [8]byte
	binary.BigEndian.PutUint64(gen[:], generation)
	h.Write(gen[:])
	return h.Sum(nil)
}

func contains(haystack []uint64, needle uint64) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
