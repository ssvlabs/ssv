package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/drand/kyber/pairing"
	kyber_dkg "github.com/drand/kyber/share/dkg"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	dkgcore "github.com/ssvlabs/ssv/protocol/v2/dkg"
	dkgp2p "github.com/ssvlabs/ssv/protocol/v2/dkg/p2p"
	dkgwire "github.com/ssvlabs/ssv/protocol/v2/dkg/wire"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// DKGOrchestrator is the per-node coordinator that drives Pedersen DKG
// ceremonies for clusters this operator participates in. It owns the
// per-cluster `*dkgp2p.Transport` instances active during a ceremony,
// routes inbound DKG envelopes to the right ceremony by clusterID, and
// persists the resulting IBE share bytes to the operator's
// `LocalKeyManager` on success.
//
// Lifecycle (see docs/TBFT-DKG-TASKS.md Phase E):
//
//   - The Controller constructs one DKGOrchestrator per node at startup.
//   - On startup and on `ValidatorAdded` events for own-validator shares,
//     EnsureClusterIBE is called for the cluster. It is synchronous —
//     duties don't proceed until DKG completes (per D7).
//   - Inbound `SSVDKGMsgType` messages are routed via Receive, which
//     looks up the right Transport by the envelope's clusterID and
//     delivers the bytes.
//
// EnsureClusterIBE is idempotent: a cluster with a persisted IBE share
// returns immediately.
type DKGOrchestrator struct {
	log        *zap.Logger
	operatorID spectypes.OperatorID
	domain     spectypes.DomainType
	suite      pairing.Suite
	dkgSuite   dkgwire.KyberSuite
	network    protocolp2p.Broadcaster
	signer     ssvtypes.OperatorSigner
	store      ibeShareStore

	mu         sync.RWMutex
	transports map[[32]byte]*dkgp2p.Transport
	// pending buffers inbound envelopes for clusters whose Transport is
	// not registered yet. Bounded per cluster to prevent unbounded
	// memory under accidental flooding; drained into the Transport at
	// registerTransport time. Solves the small startup race where peer
	// A's broadcast arrives before peer B has finished registering its
	// own ceremony Transport.
	pending map[[32]byte][][]byte
}

// pendingPerClusterCap upper-bounds buffered inbound messages per
// not-yet-registered cluster. Generously sized for any realistic SSV
// cluster (n ≤ 13 → ~4n = 52 messages across the four DKG phases).
const pendingPerClusterCap = 64

// ibeShareStore is the subset of LocalKeyManager the orchestrator depends
// on. Defined here so the orchestrator can be tested without instantiating
// a full LocalKeyManager.
type ibeShareStore interface {
	ekm.IBEShareBytesProvider
	ekm.IBEShareWriter
}

// DKGOrchestratorOptions parameterises the orchestrator. All fields are
// required.
type DKGOrchestratorOptions struct {
	Logger     *zap.Logger
	OperatorID spectypes.OperatorID
	Domain     spectypes.DomainType
	Suite      pairing.Suite
	Network    protocolp2p.Broadcaster
	Signer     ssvtypes.OperatorSigner
	Store      ibeShareStore
}

// NewDKGOrchestrator constructs an orchestrator from the given options.
func NewDKGOrchestrator(opts DKGOrchestratorOptions) (*DKGOrchestrator, error) {
	if opts.Suite == nil {
		return nil, errors.New("dkg orchestrator: nil suite")
	}
	if opts.Network == nil {
		return nil, errors.New("dkg orchestrator: nil network broadcaster")
	}
	if opts.Signer == nil {
		return nil, errors.New("dkg orchestrator: nil operator signer")
	}
	if opts.Store == nil {
		return nil, errors.New("dkg orchestrator: nil IBE share store")
	}
	dkgSuite, ok := opts.Suite.G1().(dkgwire.KyberSuite)
	if !ok {
		return nil, errors.New("dkg orchestrator: suite.G1() does not implement kyber dkg.Suite")
	}
	log := opts.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return &DKGOrchestrator{
		log:        log,
		operatorID: opts.OperatorID,
		domain:     opts.Domain,
		suite:      opts.Suite,
		dkgSuite:   dkgSuite,
		network:    opts.Network,
		signer:     opts.Signer,
		store:      opts.Store,
		transports: make(map[[32]byte]*dkgp2p.Transport),
		pending:    make(map[[32]byte][][]byte),
	}, nil
}

// EnsureClusterIBE runs DKG for the given cluster if no IBE share is
// persisted yet, and blocks until the share lands on disk (or fails).
// Idempotent — a cluster with an existing persisted share returns nil
// immediately.
//
// `committee` is the cluster's full operator-ID list; `threshold` is
// computed from len(committee) per the protocol's `qEnc = 2f+1` rule
// (see docs/TBFT.md "Why it's safe" — unified threshold for cryptographic
// safety against byzantine cross-signing).
// `generation` is the per-cluster monotonic counter (0 for fresh DKG;
// reconfig in Phase F bumps it).
func (o *DKGOrchestrator) EnsureClusterIBE(
	ctx context.Context,
	clusterID [32]byte,
	committee []spectypes.OperatorID,
	generation uint64,
) error {
	if _, err := o.store.GetIBEShareBytes(clusterID); err == nil {
		return nil // already persisted
	} else if !errors.Is(err, ekm.ErrIBEShareNotFound) {
		return fmt.Errorf("check existing IBE share: %w", err)
	}

	threshold := ibeThresholdForCommitteeSize(len(committee))
	if threshold < 2 {
		return fmt.Errorf("committee size %d too small for IBE threshold", len(committee))
	}

	msgID := spectypes.NewMsgID(o.domain, clusterID[:], ssvmessage.RoleDKG)
	tx, err := dkgp2p.New(dkgp2p.Options{
		Network: o.network,
		Signer:  o.signer,
		MsgID:   msgID,
	})
	if err != nil {
		return fmt.Errorf("build P2P transport: %w", err)
	}

	if err := o.registerTransport(clusterID, tx); err != nil {
		return err
	}
	defer o.unregisterTransport(clusterID)

	coord, err := dkgcore.NewCoordinator(dkgcore.CoordinatorOpts{
		Logger:     o.log.Named("ceremony"),
		OperatorID: uint64(o.operatorID),
		Suite:      o.suite,
		Transport:  tx,
	})
	if err != nil {
		return fmt.Errorf("build per-ceremony coordinator: %w", err)
	}

	committeeUint64 := make([]uint64, len(committee))
	for i, id := range committee {
		committeeUint64[i] = uint64(id)
	}

	o.log.Info("starting DKG ceremony",
		zap.String("cluster_id", fmt.Sprintf("%x", clusterID[:8])),
		zap.Int("committee_size", len(committee)),
		zap.Int("threshold", threshold),
		zap.Uint64("generation", generation),
	)

	share, err := coord.Run(ctx, clusterID, committeeUint64, threshold, generation)
	if err != nil {
		return fmt.Errorf("run DKG ceremony: %w", err)
	}

	shareBytes, pubKeyBytes, polyCommits, err := serializeDistKeyShare(share)
	if err != nil {
		return fmt.Errorf("serialize DKG result: %w", err)
	}

	if err := o.store.AddIBEShare(clusterID, generation, shareBytes, pubKeyBytes, polyCommits); err != nil {
		return fmt.Errorf("persist IBE share: %w", err)
	}

	o.log.Info("DKG ceremony completed",
		zap.String("cluster_id", fmt.Sprintf("%x", clusterID[:8])),
		zap.Uint64("generation", generation),
	)
	return nil
}

// RemoveClusterIBE removes the persisted IBE share for `clusterID`.
// Called from the share-removal lifecycle path when the last validator
// on a committee is removed — orphaned IBE shares are cleaned up so a
// future cluster with the same committee starts fresh. Idempotent —
// removing a clusterID with no record is a no-op.
func (o *DKGOrchestrator) RemoveClusterIBE(clusterID [32]byte) error {
	return o.store.RemoveIBEShare(clusterID)
}

// Receive routes an inbound DKG envelope to the right per-cluster
// Transport. Returns nil (drop) when no ceremony is currently active for
// the envelope's clusterID — the message arrived after the ceremony
// completed or before this node started its own.
//
// Errors only on malformed envelopes the orchestrator can't even peek at.
func (o *DKGOrchestrator) Receive(envelope []byte) error {
	if len(envelope) == 0 {
		return errors.New("dkg orchestrator: empty envelope")
	}
	parsed, err := dkgwire.Unwrap(envelope, o.dkgSuite)
	if err != nil {
		return fmt.Errorf("dkg orchestrator: decode envelope: %w", err)
	}
	cid, err := envelopeClusterID(parsed)
	if err != nil {
		return err
	}
	o.mu.Lock()
	tx, ok := o.transports[cid]
	if !ok {
		// Buffer until our own EnsureClusterIBE registers a transport.
		// Bounded to prevent runaway memory under accidental flooding.
		buf := o.pending[cid]
		if len(buf) >= pendingPerClusterCap {
			o.mu.Unlock()
			o.log.Debug("dkg orchestrator: pending buffer full; dropping",
				zap.String("cluster_id", fmt.Sprintf("%x", cid[:8])),
				zap.Uint8("kind", uint8(parsed.Kind)),
			)
			return nil
		}
		o.pending[cid] = append(buf, append([]byte(nil), envelope...))
		o.mu.Unlock()
		return nil
	}
	o.mu.Unlock()
	return tx.Deliver(envelope)
}

// registerTransport binds `tx` to clusterID for inbound routing and
// drains any pending pre-registration messages into the Transport's
// inbox. Errors if a ceremony is already in flight for this cluster —
// the caller should treat that as a programming error (EnsureClusterIBE
// shouldn't run concurrently for the same cluster).
func (o *DKGOrchestrator) registerTransport(clusterID [32]byte, tx *dkgp2p.Transport) error {
	o.mu.Lock()
	if _, exists := o.transports[clusterID]; exists {
		o.mu.Unlock()
		return fmt.Errorf("dkg orchestrator: ceremony already in flight for cluster %x", clusterID[:8])
	}
	o.transports[clusterID] = tx
	pending := o.pending[clusterID]
	delete(o.pending, clusterID)
	o.mu.Unlock()

	for _, env := range pending {
		if err := tx.Deliver(env); err != nil {
			o.log.Debug("dkg orchestrator: drop pending envelope at deliver",
				zap.String("cluster_id", fmt.Sprintf("%x", clusterID[:8])),
				zap.Error(err),
			)
		}
	}
	return nil
}

func (o *DKGOrchestrator) unregisterTransport(clusterID [32]byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.transports, clusterID)
}

// envelopeClusterID returns the cluster identifier embedded in any DKG
// envelope kind. The wire format places clusterID + generation in every
// inner body so the orchestrator can route without having parsed the
// kyber bundle further.
func envelopeClusterID(env *dkgwire.Envelope) ([32]byte, error) {
	switch env.Kind {
	case dkgwire.KindExchange:
		if env.Exchange == nil {
			return [32]byte{}, errors.New("dkg orchestrator: exchange envelope missing body")
		}
		return env.Exchange.ClusterID, nil
	case dkgwire.KindDeal:
		if env.Deal == nil {
			return [32]byte{}, errors.New("dkg orchestrator: deal envelope missing body")
		}
		return env.Deal.ClusterID, nil
	case dkgwire.KindResponse:
		if env.Response == nil {
			return [32]byte{}, errors.New("dkg orchestrator: response envelope missing body")
		}
		return env.Response.ClusterID, nil
	case dkgwire.KindJustification:
		if env.Justification == nil {
			return [32]byte{}, errors.New("dkg orchestrator: justification envelope missing body")
		}
		return env.Justification.ClusterID, nil
	default:
		return [32]byte{}, fmt.Errorf("dkg orchestrator: unexpected envelope kind 0x%02x", byte(env.Kind))
	}
}

// serializeDistKeyShare extracts byte representations of the kyber DKG
// output for persistence. The orchestrator does this conversion (it owns
// the kyber types); LocalKeyManager stores opaque bytes.
func serializeDistKeyShare(s *kyber_dkg.DistKeyShare) (shareBytes, pubKeyBytes []byte, polyCommits [][]byte, err error) {
	if s == nil || s.Share == nil || s.Share.V == nil {
		return nil, nil, nil, errors.New("dkg orchestrator: nil DistKeyShare")
	}
	if len(s.Commits) == 0 {
		return nil, nil, nil, errors.New("dkg orchestrator: DistKeyShare has empty Commits")
	}
	shareBytes, err = s.Share.V.MarshalBinary()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal share scalar: %w", err)
	}
	pubKeyBytes, err = s.Commits[0].MarshalBinary()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal cluster IBE pubkey: %w", err)
	}
	polyCommits = make([][]byte, len(s.Commits))
	for i, p := range s.Commits {
		b, err := p.MarshalBinary()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("marshal poly commit %d: %w", i, err)
		}
		polyCommits[i] = b
	}
	return shareBytes, pubKeyBytes, polyCommits, nil
}

// ibeThresholdForCommitteeSize returns qEnc = 2f+1 where n = 3f+1. SSV
// committees are sized at n ∈ {4, 7, 10, 13}; this maps each to the
// expected IBE threshold (3, 5, 7, 9). Matches qV (the V-keypair
// threshold) per docs/TBFT.md "Why it's safe" — the unified threshold
// gives cryptographic safety against byzantine cross-signing. The IBE
// keypair is still distinct from the V-keypair (different DST / signing
// backend), only the threshold value coincides.
func ibeThresholdForCommitteeSize(n int) int {
	if n < 4 {
		return 0
	}
	f := (n - 1) / 3
	return 2*f + 1
}
