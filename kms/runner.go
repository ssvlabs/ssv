package kms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/kms/chains"
	"github.com/ssvlabs/ssv/kms/kmstypes"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	// maxBlockLag bounds how old the block a proposal pins its policy check to may be.
	maxBlockLag = 64
	// shareTimeout is how long the signers of a decided request have to send their shares.
	shareTimeout = 30 * time.Second
	// liveWindow: an operator counts as live if a message from it (at least a heartbeat) arrived
	// this recently.
	liveWindow        = 15 * time.Second
	heartbeatInterval = 4 * time.Second
	// presignWait is how long a signer waits for a presignature the leader already holds.
	presignWait = 10 * time.Second
	maxPending  = 256
	maxFuture   = 128
)

// keyRunner runs one dKMS key, following SSV's runner phases:
//   - pre-consensus: presigning in the background (presignPool)
//   - consensus: QBFT (the node's protocol/v2/qbft controller) on {request, presignature}, with the
//     policy check as the value check
//   - post-consensus: each signer sends its signature share; anyone combines them.
//
// Heights are the key's request sequence numbers. All state is owned by the loop goroutine.
type keyRunner struct {
	s      *Service
	key    *Key
	self   spectypes.OperatorID
	logger *zap.Logger
	msgID  spectypes.MessageID
	ctrl   *controller.Controller
	member *spectypes.CommitteeMember
	pool   *presignPool
	events chan func()

	// stateMu guards state for readers outside the loop (request admission); the loop is the only writer.
	stateMu sync.RWMutex
	state   *runnerState
	height  uint64
	running bool
	pending []*kmstypes.SignRequest
	future  map[uint64][]*queue.SSVMessage
	signing map[uint64]*signingState
	early   map[uint64]map[uint64][]byte

	liveMu   sync.Mutex
	lastSeen map[spectypes.OperatorID]time.Time

	checkMu    sync.Mutex
	checkCache map[[32]byte]error
}

type signingState struct {
	height   uint64
	id       string
	value    *kmstypes.ConsensusValue
	digest   []byte
	shares   map[uint64][]byte
	deadline time.Time
}

func newKeyRunner(s *Service, key *Key) (*keyRunner, error) {
	self := s.operatorID()
	domain := s.netCfg.CurrentDomainType()
	member, err := key.committeeMember(self, domain)
	if err != nil {
		return nil, err
	}
	st, err := s.store.loadState(key.IDHex)
	if err != nil {
		return nil, err
	}
	r := &keyRunner{
		s:          s,
		key:        key,
		self:       self,
		logger:     key.logger(s.logger),
		msgID:      kmstypes.NewMessageID(domain, key.ID, ssvtypes.RoleKMS),
		events:     make(chan func(), 4096),
		state:      st,
		height:     st.Height,
		future:     map[uint64][]*queue.SSVMessage{},
		signing:    map[uint64]*signingState{},
		early:      map[uint64]map[uint64][]byte{},
		lastSeen:   map[spectypes.OperatorID]time.Time{},
		checkCache: map[[32]byte]error{},
	}
	r.member = member
	r.resetController()
	r.pool = newPresignPool(r)
	if err := r.pool.load(st.Consumed); err != nil {
		return nil, err
	}
	return r, nil
}

// resetController starts a fresh QBFT controller at the current height. An instance that gave up
// without deciding can't be restarted in its controller; a fresh one can run the same height again,
// so an operator never moves past a height the committee hasn't decided.
func (r *keyRunner) resetController() {
	committee := slices.Clone(r.key.Committee)
	cfg := &qbft.Config{
		ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			// Round robin over the committee, shifted by one per height.
			idx := (uint64(state.Height) + uint64(round) - uint64(specqbft.FirstRound)) % uint64(len(committee))
			return committee[idx]
		},
		Network:     &committeeNetwork{s: r.s, committee: committee},
		CutOffRound: roundtimer.CutOffRoundFor(ssvtypes.RoleKMS),
	}
	r.ctrl = controller.NewController(r.msgID[:], r.member, cfg, r.s.signer, false)
	r.ctrl.LatestInstanceHeight = specqbft.Height(r.height)
}

func (r *keyRunner) run(ctx context.Context) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	presignTick := time.NewTicker(2 * time.Second)
	defer presignTick.Stop()
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	r.sendHeartbeat()
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-r.events:
			fn()
		case <-tick.C:
			r.maybeStart(ctx)
			r.expireSigning()
		case <-presignTick.C:
			go r.pool.tick()
		case <-heartbeat.C:
			go r.sendHeartbeat()
		}
	}
}

// do runs fn on the loop goroutine.
func (r *keyRunner) do(fn func()) bool {
	select {
	case r.events <- fn:
		return true
	default:
		r.logger.Warn("dKMS event queue full, dropping event")
		return false
	}
}

func (r *keyRunner) timerF() ssv.QBFTRoundTimerF {
	return func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
		height := uint64(slot)
		return roundtimer.New(ctx, r.s.netCfg.Beacon, ssvtypes.RoleKMS, slot, func(round specqbft.Round) {
			r.do(func() { r.onTimeout(ctx, height, round) })
		})
	}
}

func (r *keyRunner) seen(id spectypes.OperatorID) {
	r.liveMu.Lock()
	r.lastSeen[id] = time.Now()
	r.liveMu.Unlock()
}

func (r *keyRunner) allLive(set []uint64) bool {
	r.liveMu.Lock()
	defer r.liveMu.Unlock()
	for _, id := range set {
		if spectypes.OperatorID(id) == r.self {
			continue
		}
		if time.Since(r.lastSeen[spectypes.OperatorID(id)]) > liveWindow {
			return false
		}
	}
	return true
}

func (r *keyRunner) sendHeartbeat() {
	r.send(&kmstypes.Message{Kind: kmstypes.KindHeartbeat, Time: time.Now().UnixMilli()})
}

// send signs and gossips a dKMS message to the key's committee.
func (r *keyRunner) send(m *kmstypes.Message) {
	data, err := m.Encode()
	if err != nil {
		r.logger.Warn("could not encode dKMS message", zap.Error(err))
		return
	}
	msg := &spectypes.SSVMessage{MsgType: kmsMsgType, MsgID: r.msgID, Data: data}
	sig, err := r.s.signer.SignSSVMessage(msg)
	if err != nil {
		r.logger.Warn("could not sign dKMS message", zap.Error(err))
		return
	}
	signed := &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{r.self}, Signatures: [][]byte{sig}, SSVMessage: msg}
	if err := r.s.net.BroadcastToOperators(r.key.Committee, signed); err != nil {
		r.logger.Debug("could not broadcast dKMS message", zap.String("kind", string(m.Kind)), zap.Error(err))
	}
}

// ---------- incoming ----------

func (r *keyRunner) onMessage(ctx context.Context, msg *queue.SSVMessage) {
	if len(msg.SignedSSVMessage.OperatorIDs) == 1 {
		r.seen(msg.SignedSSVMessage.OperatorIDs[0])
	}
	switch body := msg.Body.(type) {
	case *specqbft.Message:
		r.onConsensus(ctx, msg, body)
	case *kmstypes.Message:
		from := uint64(msg.SignedSSVMessage.OperatorIDs[0])
		if body.Kind != kmstypes.KindHeartbeat {
			r.logger.Debug("dKMS message", zap.String("kind", string(body.Kind)), zap.Uint64("from", from))
		}
		switch body.Kind {
		case kmstypes.KindRequest:
			if from != uint64(r.self) && body.Request != nil {
				if err := body.Request.VerifyRequester(); err == nil {
					r.enqueue(body.Request)
				}
			}
		case kmstypes.KindPresignStart:
			go r.pool.onStart(from, body)
		case kmstypes.KindMPC:
			r.pool.onMPC(from, body)
		case kmstypes.KindShare:
			if from != uint64(r.self) {
				r.onShare(from, body)
			}
		}
	}
}

func (r *keyRunner) onConsensus(ctx context.Context, msg *queue.SSVMessage, qm *specqbft.Message) {
	h := uint64(qm.Height)
	signed := msg.SignedSSVMessage
	isDecided := qm.MsgType == specqbft.CommitMsgType && r.ctrl.CommitteeMember.HasQuorum(len(signed.OperatorIDs))

	if !(r.running && h == r.height) && !isDecided && h >= r.height {
		if qm.MsgType == specqbft.ProposalMsgType && !r.running {
			// Another operator leads this height: join with its proposal as our own start value.
			if h > r.height {
				r.logger.Info("catching up to the committee's height", zap.Uint64("from", r.height), zap.Uint64("to", h))
				r.height = h
			}
			if !r.startInstance(ctx, signed.FullData) {
				return
			}
		} else {
			if len(r.future[h]) < maxFuture && h < r.height+16 {
				r.future[h] = append(r.future[h], msg)
			}
			return
		}
	}

	decided, err := r.ctrl.ProcessMsg(ctx, r.logger, signed, r.timerF())
	if err != nil {
		r.logger.Debug("qbft message not processed", zap.Uint64("height", h), zap.Error(err))
	}
	if decided != nil && !isDecided {
		// Our instance just decided: share the aggregated commit, so operators that missed this
		// height (or gave up on it) can apply the decision too.
		if err := r.s.net.BroadcastToOperators(r.key.Committee, decided); err != nil {
			r.logger.Debug("could not broadcast decided message", zap.Error(err))
		}
	}
	r.checkDecided(ctx, h)
}

func (r *keyRunner) checkDecided(ctx context.Context, h uint64) {
	inst := r.ctrl.RecentInstances.FindInstance(specqbft.Height(h))
	if inst == nil {
		return
	}
	decided, value := inst.IsDecided()
	if !decided {
		return
	}
	if _, done := r.signing[h]; done {
		return
	}
	r.onDecided(ctx, h, value)
}

func (r *keyRunner) onTimeout(ctx context.Context, h uint64, round specqbft.Round) {
	inst := r.ctrl.RecentInstances.FindInstance(specqbft.Height(h))
	if inst == nil || inst.State.Round != round {
		return
	}
	if decided, _ := inst.IsDecided(); decided {
		return
	}
	if err := inst.UponRoundTimeout(ctx, r.logger); err != nil {
		r.logger.Debug("round timeout", zap.Error(err))
	}
	if !inst.IsRelevant() && r.running && r.height == h {
		// The instance gave up without deciding. Stay at this height (only a decision moves it, or
		// the committee would drift apart) and run it again with a fresh controller; the requests
		// stay queued.
		r.logger.Warn("dKMS height gave up without a decision, retrying it", zap.Uint64("height", h))
		r.running = false
		r.resetController()
	}
}

// ---------- consensus ----------

// maybeStart starts the next height when there is a request to sign.
func (r *keyRunner) maybeStart(ctx context.Context) {
	if r.running {
		return
	}
	var req *kmstypes.SignRequest
	for len(r.pending) > 0 {
		id, err := r.pending[0].ID()
		if _, decided := r.state.Decided[id]; err != nil || decided {
			r.pending = r.pending[1:]
			continue
		}
		req = r.pending[0]
		break
	}
	if req == nil {
		return
	}
	sp := r.pool.pick(r.allLive, r.state.Consumed)
	if sp == nil {
		return // wait for presigning; another operator may lead in the meantime
	}
	block, err := r.s.policyBlock(ctx)
	if err != nil {
		r.logger.Warn("could not read chain head", zap.Error(err))
		return
	}
	v := &kmstypes.ConsensusValue{Request: *req, BlockNumber: block, PresignID: mustHex(sp.ID), Signers: sp.Signers, Commitment: sp.Commitment}
	data, err := v.Encode()
	if err != nil {
		return
	}
	if err := r.checkValue(data); err != nil {
		var rejected *RejectedError
		if !errors.As(err, &rejected) {
			return // transient (chain, presignature): retry on the next tick
		}
		// The request no longer passes the policy (it changed since admission): drop it.
		r.pending = r.pending[1:]
		r.reject(req, err)
		return
	}
	if !r.startInstance(ctx, data) {
		r.pending = r.pending[1:]
	}
}

// reject fails a queued request that will never be signed, so clients waiting on it get an answer.
func (r *keyRunner) reject(req *kmstypes.SignRequest, err error) {
	id, idErr := req.ID()
	if idErr != nil {
		return
	}
	if res, ok := r.s.Result(id); ok && res.Status != StatusPending {
		return
	}
	r.s.putResult(&Result{ID: id, KeyID: r.key.IDHex, Status: StatusFailed, Error: err.Error(), Request: req})
	r.logger.Info("dKMS request rejected at consensus", zap.String("request", id), zap.Error(err))
}

func (r *keyRunner) startInstance(ctx context.Context, value []byte) bool {
	h := r.height
	if _, err := r.ctrl.StartNewInstance(ctx, r.logger, specqbft.Height(h), value, r.checker(), r.timerF()); err != nil {
		r.logger.Debug("could not start dKMS instance", zap.Uint64("height", h), zap.Error(err))
		return false
	}
	r.running = true
	r.logger.Debug("dKMS instance started", zap.Uint64("height", h))
	buffered := r.future[h]
	delete(r.future, h)
	for _, m := range buffered {
		r.onConsensus(ctx, m, m.Body.(*specqbft.Message))
	}
	return true
}

func (r *keyRunner) checker() ssv.ValueChecker { return valueChecker{r} }

type valueChecker struct{ r *keyRunner }

func (c valueChecker) CheckValue(value []byte) error { return c.r.checkValue(value) }

// checkValue is the QBFT value check: an operator only agrees to sign what it independently
// verified. Results are cached per value: QBFT re-checks the same value on every round.
func (r *keyRunner) checkValue(data []byte) error {
	h := sha256.Sum256(data)
	r.checkMu.Lock()
	if err, ok := r.checkCache[h]; ok {
		r.checkMu.Unlock()
		return err
	}
	r.checkMu.Unlock()
	err := r.verifyValue(data)
	r.checkMu.Lock()
	if len(r.checkCache) > 1024 {
		r.checkCache = map[[32]byte]error{}
	}
	r.checkCache[h] = err
	r.checkMu.Unlock()
	if err != nil {
		r.logger.Info("rejected dKMS proposal", zap.Error(err))
	}
	return err
}

// clearCheckCache forgets cached value checks: they depend on the decided state (requests, nonces,
// presignatures), which just changed.
func (r *keyRunner) clearCheckCache() {
	r.checkMu.Lock()
	r.checkCache = map[[32]byte]error{}
	r.checkMu.Unlock()
}

func (r *keyRunner) verifyValue(data []byte) error {
	v := &kmstypes.ConsensusValue{}
	if err := v.Decode(data); err != nil {
		return fmt.Errorf("decode value: %w", err)
	}
	id, err := v.Request.ID()
	if err != nil {
		return err
	}
	if _, decided := r.state.Decided[id]; decided {
		return errors.New("request already signed")
	}
	if presigID := hex.EncodeToString(v.PresignID); r.state.Consumed[presigID] {
		return errors.New("presignature already used")
	}
	set := v.Signers
	if len(set) != r.key.Threshold || !slices.IsSorted(set) {
		return errors.New("invalid signer set")
	}
	for i, op := range set {
		if !slices.Contains(r.key.Committee, spectypes.OperatorID(op)) || (i > 0 && set[i-1] == op) {
			return errors.New("invalid signer set")
		}
	}
	if slices.Contains(set, uint64(r.self)) {
		// We will sign with it: we must hold this very presignature.
		sp := r.pool.get(hex.EncodeToString(v.PresignID), presignWait)
		if sp == nil {
			return errors.New("presignature not held by this signer")
		}
		if !slices.Equal(sp.Signers, set) || !bytes.Equal(sp.Commitment, v.Commitment) {
			return errors.New("presignature does not match")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	head, err := r.s.chain.HeadNumber(ctx)
	if err != nil {
		return fmt.Errorf("chain head: %w", err)
	}
	if v.BlockNumber > head || head-v.BlockNumber > maxBlockLag {
		return fmt.Errorf("block %d is not within %d blocks of head %d", v.BlockNumber, maxBlockLag, head)
	}
	return r.s.checkRequest(ctx, r, &v.Request, v.BlockNumber)
}

// ---------- post-consensus ----------

func (r *keyRunner) onDecided(ctx context.Context, h uint64, data []byte) {
	// A decision for an earlier height (learned late, from its decided message) is applied too, but
	// never moves the height back.
	if h >= r.height {
		r.running = false
		r.height = h + 1
	}
	for fh := range r.future {
		if fh <= h {
			delete(r.future, fh)
		}
	}
	v := &kmstypes.ConsensusValue{}
	if err := v.Decode(data); err != nil {
		r.logger.Error("decided an undecodable value", zap.Error(err))
		return
	}
	id, err := v.Request.ID()
	if err != nil {
		return
	}
	if _, applied := r.state.Decided[id]; applied {
		return
	}
	r.clearCheckCache()
	chain, tx, err := v.Request.Parse()
	if err != nil {
		r.logger.Error("decided an unparseable transaction", zap.Error(err))
		return
	}
	digest := tx.Digest()
	presigID := hex.EncodeToString(v.PresignID)

	// Record the decision, and that the presignature is spent, before releasing any share.
	r.stateMu.Lock()
	if r.state.Height < r.height {
		r.state.Height = r.height
	}
	r.state.Decided[id] = h
	r.state.Consumed[presigID] = true
	if seq, ok := tx.Sequence(); ok {
		r.state.Nonces[sequenceKey(chain, seq)] = id
	}
	r.stateMu.Unlock()
	r.persist()
	r.removePending(id)
	r.logger.Info("✅ dKMS request decided", zap.Uint64("height", h), zap.String("request", id), zap.String("chain", chain.String()), zap.Uint64s("signers", v.Signers))

	ss := &signingState{height: h, id: id, value: v, digest: digest, shares: map[uint64][]byte{}, deadline: time.Now().Add(shareTimeout)}
	r.signing[h] = ss
	r.s.putResult(&Result{ID: id, KeyID: r.key.IDHex, Chain: v.Request.Chain, Status: StatusSigning, Height: h, Signers: v.Signers, Request: &v.Request})

	if slices.Contains(v.Signers, uint64(r.self)) {
		share, err := r.signShare(presigID, v, digest)
		if err != nil {
			r.logger.Error("could not produce signature share", zap.Error(err))
		} else {
			ss.shares[uint64(r.self)] = share
			r.send(&kmstypes.Message{Kind: kmstypes.KindShare, Height: h, RequestID: id, Share: share})
		}
	} else {
		r.pool.drop(presigID)
	}
	for from, share := range r.early[h] {
		r.addShare(ss, from, share)
	}
	delete(r.early, h)
	r.tryCombine(ctx, ss)
}

func (r *keyRunner) signShare(presigID string, v *kmstypes.ConsensusValue, digest []byte) ([]byte, error) {
	sp, plain, err := r.pool.take(presigID)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(sp.Commitment, v.Commitment) {
		return nil, errors.New("presignature commitment mismatch")
	}
	pre, err := r.key.Scheme.UnmarshalPresignature(plain)
	if err != nil {
		return nil, err
	}
	return r.key.Scheme.SignShare(pre, digest)
}

func (r *keyRunner) onShare(from uint64, m *kmstypes.Message) {
	ss, ok := r.signing[m.Height]
	if !ok {
		if m.Height >= r.height && m.Height < r.height+16 {
			if r.early[m.Height] == nil {
				r.early[m.Height] = map[uint64][]byte{}
			}
			r.early[m.Height][from] = m.Share
		}
		return
	}
	if m.RequestID != ss.id {
		return
	}
	r.addShare(ss, from, m.Share)
	r.tryCombine(context.Background(), ss)
}

func (r *keyRunner) addShare(ss *signingState, from uint64, share []byte) {
	if slices.Contains(ss.value.Signers, from) && len(share) > 0 {
		ss.shares[from] = share
	}
}

func (r *keyRunner) tryCombine(ctx context.Context, ss *signingState) {
	if len(ss.shares) < len(ss.value.Signers) {
		return
	}
	delete(r.signing, ss.height)
	shares := make([][]byte, 0, len(ss.value.Signers))
	for _, id := range ss.value.Signers {
		shares = append(shares, ss.shares[id])
	}
	res := &Result{ID: ss.id, KeyID: r.key.IDHex, Chain: ss.value.Request.Chain, Height: ss.height, Signers: ss.value.Signers, Request: &ss.value.Request}
	sig, err := r.key.Scheme.Combine(r.key.PublicKey, ss.value.Commitment, ss.digest, shares)
	if err != nil {
		r.fail(res, err.Error())
		return
	}
	chain, tx, err := ss.value.Request.Parse()
	if err != nil {
		r.fail(res, err.Error())
		return
	}
	raw, hash, err := tx.Assemble(sig)
	if err != nil {
		r.fail(res, err.Error())
		return
	}
	res.Status = StatusSigned
	if _, family, _ := chains.ParseID(chain.String()); family != nil && chains.IsOffchain(family) {
		// A message: the signature is the result (V as 27/28, ready for the requesting app).
		res.Signature = hexutil.Encode(raw)
		res.TxHash = hash
		r.s.putResult(res)
		r.logger.Info("🖋️ dKMS message signed", zap.String("request", ss.id), zap.String("chain", chain.String()), zap.String("digest", hash))
		return
	}
	res.Signature = hexutil.Encode(sig)
	res.RawTx = hexutil.Encode(raw)
	res.TxHash = hash
	r.s.putResult(res)
	r.logger.Info("🖋️ dKMS transaction signed", zap.String("request", ss.id), zap.String("chain", chain.String()), zap.String("tx", res.TxHash))

	if ex := r.s.executor(chain); r.s.broadcastTxs && ex != nil {
		go func() {
			bctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := ex.Broadcast(bctx, raw); err != nil {
				r.logger.Warn("could not broadcast signed transaction", zap.String("chain", chain.String()), zap.String("tx", res.TxHash), zap.Error(err))
			}
		}()
	}
}

func (r *keyRunner) fail(res *Result, reason string) {
	res.Status = StatusFailed
	res.Error = reason
	r.s.putResult(res)
	// The request may be submitted again (with fresh presignatures).
	r.stateMu.Lock()
	delete(r.state.Decided, res.ID)
	if res.Request != nil {
		if chain, tx, err := res.Request.Parse(); err == nil {
			if seq, ok := tx.Sequence(); ok && r.state.Nonces[sequenceKey(chain, seq)] == res.ID {
				delete(r.state.Nonces, sequenceKey(chain, seq))
			}
		}
	}
	r.stateMu.Unlock()
	r.persist()
	r.clearCheckCache()
	r.logger.Warn("dKMS signing failed", zap.String("request", res.ID), zap.String("reason", reason))
}

func (r *keyRunner) expireSigning() {
	for _, ss := range r.signing {
		if time.Now().After(ss.deadline) {
			delete(r.signing, ss.height)
			var missing []uint64
			for _, id := range ss.value.Signers {
				if _, ok := ss.shares[id]; !ok {
					missing = append(missing, id)
				}
			}
			r.fail(&Result{ID: ss.id, KeyID: r.key.IDHex, Height: ss.height, Signers: ss.value.Signers, Request: &ss.value.Request},
				fmt.Sprintf("signers %v did not send their shares", missing))
		}
	}
}

// ---------- queue ----------

func (r *keyRunner) enqueue(req *kmstypes.SignRequest) {
	id, err := req.ID()
	if err != nil {
		return
	}
	if _, decided := r.state.Decided[id]; decided {
		return
	}
	for _, p := range r.pending {
		if pid, _ := p.ID(); pid == id {
			return
		}
	}
	if len(r.pending) >= maxPending {
		r.logger.Warn("dKMS request queue full")
		return
	}
	r.pending = append(r.pending, req)
}

func (r *keyRunner) removePending(id string) {
	r.pending = slices.DeleteFunc(r.pending, func(p *kmstypes.SignRequest) bool {
		pid, _ := p.ID()
		return pid == id
	})
}

func (r *keyRunner) persist() {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	if err := r.s.store.saveState(r.key.IDHex, r.state); err != nil {
		r.logger.Error("could not persist dKMS state", zap.Error(err))
	}
}

func (r *keyRunner) info() KeyInfo {
	ki := r.key.Info()
	r.stateMu.RLock()
	ki.Height = r.state.Height
	r.stateMu.RUnlock()
	ki.Presignatures = r.pool.counts()
	return ki
}

func mustHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}
