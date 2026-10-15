package kms

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/taurusgroup/multi-party-sig/pkg/protocol"
	"go.uber.org/zap"

	dkgkms "github.com/ssvlabs/ssv-dkg/pkgs/kms"

	"github.com/ssvlabs/ssv/kms/kmstypes"
)

const (
	// presignTarget is how many unused presignatures a coordinator keeps for each signer set.
	presignTarget = 2
	// presignSessionTTL abandons sessions that stalled (e.g. a member restarted and lost the
	// session). A healthy session takes a few seconds. Sessions with an offline member are abandoned
	// sooner (gc): while they run they count towards presignTarget and block replacements.
	presignSessionTTL = 30 * time.Second
	// maxPresignSessions bounds concurrent sessions per key: presigning is CPU-heavy.
	maxPresignSessions = 4
	maxEarlyMPC        = 512
)

// presignPool runs presigning in the background (SSV's pre-consensus phase) and keeps the results.
//
// Presignatures are bound to a signer set. Every signer set is coordinated by its lowest operator,
// which keeps presignTarget of them ready by announcing new sessions (KindPresignStart); members
// run the session and store the result. A presignature is removed (and so never reused) as soon as
// a decided request names it.
type presignPool struct {
	r      *keyRunner
	logger *zap.Logger

	ticking atomic.Bool

	mu       sync.Mutex
	ready    map[string]*storedPresig
	sessions map[string]*presignSession
	early    map[string][]earlyMPC
	nEarly   int
	waiters  map[string][]chan struct{}
}

type presignSession struct {
	id      string
	sid     []byte
	signers []uint64
	h       *protocol.MultiHandler
	inbox   chan *protocol.Message
	started time.Time
}

type earlyMPC struct {
	from uint64
	msg  *kmstypes.Message
	at   time.Time
}

func newPresignPool(r *keyRunner) *presignPool {
	return &presignPool{
		r:        r,
		logger:   r.logger.Named("presign"),
		ready:    map[string]*storedPresig{},
		sessions: map[string]*presignSession{},
		early:    map[string][]earlyMPC{},
		waiters:  map[string][]chan struct{}{},
	}
}

func setKey(signers []uint64) string { return fmt.Sprint(signers) }

func (p *presignPool) load(consumed map[string]bool) error {
	stored, err := p.r.s.store.presigs(p.r.key.IDHex)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sp := range stored {
		if consumed[sp.ID] {
			_ = p.r.s.store.deletePresig(p.r.key.IDHex, sp.ID)
			continue
		}
		p.ready[sp.ID] = sp
	}
	return nil
}

// tick starts sessions for the signer sets this operator coordinates.
func (p *presignPool) tick() {
	if !p.ticking.CompareAndSwap(false, true) {
		return
	}
	defer p.ticking.Store(false)
	self := uint64(p.r.self)
	for _, set := range p.r.key.signerSets(p.r.self) {
		if set[0] != self {
			continue
		}
		p.mu.Lock()
		have := 0
		for _, sp := range p.ready {
			if slices.Equal(sp.Signers, set) {
				have++
			}
		}
		for _, s := range p.sessions {
			if slices.Equal(s.signers, set) {
				have++
			}
		}
		busy := len(p.sessions) >= maxPresignSessions
		p.mu.Unlock()
		if have >= presignTarget || busy {
			continue
		}
		if !p.r.allLive(set) {
			p.logger.Debug("not presigning: a signer is offline", zap.Uint64s("signers", set))
			continue
		}
		sid := make([]byte, 32)
		if _, err := rand.Read(sid); err != nil {
			continue
		}
		if err := p.start(sid, set); err != nil {
			p.logger.Warn("could not start presigning", zap.Error(err))
			continue
		}
		p.r.send(&kmstypes.Message{Kind: kmstypes.KindPresignStart, Session: sid, Signers: set})
	}
	p.gc()
}

// onStart handles a coordinator's announcement of a session.
func (p *presignPool) onStart(from uint64, m *kmstypes.Message) {
	set := m.Signers
	if !slices.Contains(set, uint64(p.r.self)) {
		return // a session for other signers
	}
	if len(m.Session) != 32 || len(set) != p.r.key.Threshold || !slices.IsSorted(set) || set[0] != from {
		p.logger.Debug("ignoring invalid presign start", zap.Uint64("from", from))
		return
	}
	for i, id := range set {
		if !slices.Contains(p.r.key.Committee, spectypes.OperatorID(id)) || (i > 0 && set[i-1] == id) {
			return
		}
	}
	if from == uint64(p.r.self) {
		return // our own announcement: already started
	}
	if err := p.start(m.Session, set); err != nil {
		p.logger.Debug("could not join presigning", zap.Error(err))
	}
}

func (p *presignPool) start(sid []byte, set []uint64) error {
	id := hex.EncodeToString(sid)
	p.mu.Lock()
	if _, ok := p.sessions[id]; ok {
		p.mu.Unlock()
		return nil
	}
	if _, ok := p.ready[id]; ok {
		p.mu.Unlock()
		return nil
	}
	if len(p.sessions) >= maxPresignSessions*2 {
		p.mu.Unlock()
		return fmt.Errorf("too many presigning sessions")
	}
	p.mu.Unlock()

	startFn, err := p.r.key.Scheme.Presign(p.r.key.Share, set)
	if err != nil {
		return err
	}
	h, err := protocol.NewMultiHandler(startFn, sid)
	if err != nil {
		return err
	}
	s := &presignSession{id: id, sid: sid, signers: set, h: h, inbox: make(chan *protocol.Message, 256), started: time.Now()}

	p.mu.Lock()
	p.sessions[id] = s
	early := p.early[id]
	delete(p.early, id)
	p.nEarly -= len(early)
	p.mu.Unlock()

	go p.pump(s)
	go func() {
		for msg := range s.inbox {
			if s.h.CanAccept(msg) {
				s.h.Accept(msg)
			}
		}
	}()
	for _, e := range early {
		p.onMPC(e.from, e.msg)
	}
	return nil
}

// pump sends the session's outgoing messages and stores its result.
func (p *presignPool) pump(s *presignSession) {
	for msg := range s.h.Listen() {
		out, err := p.outgoing(s, msg)
		if err != nil {
			p.logger.Warn("could not send presigning message", zap.Error(err))
			continue
		}
		p.r.send(out)
	}
	defer close(s.inbox)

	res, err := s.h.Result()
	p.mu.Lock()
	delete(p.sessions, s.id)
	p.mu.Unlock()
	if err != nil {
		p.logger.Debug("presigning session failed", zap.String("session", s.id), zap.Error(err))
		return
	}
	pre, err := p.r.key.Scheme.PresignatureFromResult(res)
	if err != nil {
		p.logger.Warn("bad presignature", zap.Error(err))
		return
	}
	commitment, err := pre.Commitment()
	if err != nil {
		return
	}
	plain, err := pre.Marshal()
	if err != nil {
		return
	}
	sp := &storedPresig{ID: s.id, Signers: s.signers, Commitment: commitment}
	if err := p.r.s.store.savePresig(p.r.key.IDHex, sp, plain); err != nil {
		p.logger.Warn("could not store presignature", zap.Error(err))
		return
	}
	p.mu.Lock()
	p.ready[s.id] = sp
	for _, w := range p.waiters[s.id] {
		close(w)
	}
	delete(p.waiters, s.id)
	p.mu.Unlock()
	p.logger.Debug("presignature ready", zap.String("id", s.id), zap.Uint64s("signers", s.signers))
}

func (p *presignPool) outgoing(s *presignSession, msg *protocol.Message) (*kmstypes.Message, error) {
	payload, err := msg.MarshalBinary()
	if err != nil {
		return nil, err
	}
	out := &kmstypes.Message{Kind: kmstypes.KindMPC, Session: s.sid, Signers: s.signers}
	if msg.To != "" {
		if out.To, err = dkgkms.OperatorIDOf(msg.To); err != nil {
			return nil, err
		}
		wrap, err := dkgkms.RSAWrapper(p.r.key.OperatorKeys[spectypes.OperatorID(out.To)])
		if err != nil {
			return nil, err
		}
		// Gossip reaches the whole committee: seal point-to-point messages to their recipient.
		if payload, err = dkgkms.Seal(wrap, payload, s.sid); err != nil {
			return nil, err
		}
	}
	out.Payload = payload
	return out, nil
}

// onMPC delivers a presigning message to its session, or keeps it until the session starts.
func (p *presignPool) onMPC(from uint64, m *kmstypes.Message) {
	if from == uint64(p.r.self) || !slices.Contains(m.Signers, uint64(p.r.self)) || !slices.Contains(m.Signers, from) {
		return
	}
	if m.To != 0 && m.To != uint64(p.r.self) {
		return
	}
	id := hex.EncodeToString(m.Session)
	p.mu.Lock()
	s, ok := p.sessions[id]
	if !ok {
		if _, done := p.ready[id]; !done && p.nEarly < maxEarlyMPC {
			p.early[id] = append(p.early[id], earlyMPC{from: from, msg: m, at: time.Now()})
			p.nEarly++
		}
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	payload := []byte(m.Payload)
	if m.To != 0 {
		var err error
		if payload, err = dkgkms.Open(p.r.s.decrypt, payload, m.Session); err != nil {
			p.logger.Debug("could not open presigning message", zap.Uint64("from", from), zap.Error(err))
			return
		}
	}
	msg := &protocol.Message{}
	if err := msg.UnmarshalBinary(payload); err != nil {
		return
	}
	// The SSV message signature authenticates the sender; the MPC message must claim the same one.
	if msg.From != dkgkms.PartyID(from) || (m.To == 0) != (msg.To == "") {
		return
	}
	defer func() { _ = recover() }() // the session may have just finished and closed its inbox
	select {
	case s.inbox <- msg:
	default:
		p.logger.Warn("presigning inbox full, dropping message", zap.String("session", id))
	}
}

// pick returns a ready presignature whose signers are all live, oldest signer sets first.
func (p *presignPool) pick(live func([]uint64) bool, consumed map[string]bool) *storedPresig {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ids []string
	for id := range p.ready {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		sp := p.ready[id]
		if !consumed[id] && live(sp.Signers) {
			return sp
		}
	}
	return nil
}

// get returns a ready presignature, waiting up to wait for a session that is still running.
func (p *presignPool) get(id string, wait time.Duration) *storedPresig {
	p.mu.Lock()
	if sp, ok := p.ready[id]; ok {
		p.mu.Unlock()
		return sp
	}
	if _, running := p.sessions[id]; !running || wait == 0 {
		p.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	p.waiters[id] = append(p.waiters[id], ch)
	p.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(wait):
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready[id]
}

// take removes a presignature for signing and returns its secret. It is deleted from storage before
// it is used, so a crash can never lead to signing twice with it.
func (p *presignPool) take(id string) (*storedPresig, []byte, error) {
	p.mu.Lock()
	sp, ok := p.ready[id]
	delete(p.ready, id)
	p.mu.Unlock()
	if !ok {
		return nil, nil, fmt.Errorf("presignature %s not held", id)
	}
	plain, err := p.r.s.store.openPresig(sp)
	if err != nil {
		return nil, nil, err
	}
	if err := p.r.s.store.deletePresig(p.r.key.IDHex, id); err != nil {
		return nil, nil, err
	}
	return sp, plain, nil
}

// drop forgets a presignature consumed by a decided request (e.g. one this operator holds but
// another request named it).
func (p *presignPool) drop(id string) {
	p.mu.Lock()
	_, ok := p.ready[id]
	delete(p.ready, id)
	p.mu.Unlock()
	if ok {
		_ = p.r.s.store.deletePresig(p.r.key.IDHex, id)
	}
}

func (p *presignPool) counts() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]int{}
	for _, sp := range p.ready {
		out[setKey(sp.Signers)]++
	}
	return out
}

func (p *presignPool) gc() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, s := range p.sessions {
		age := time.Since(s.started)
		stalled := age > presignSessionTTL
		// Give a session a couple of heartbeats before judging liveness (e.g. right after a restart).
		if offline := age > 2*heartbeatInterval && !p.r.allLive(s.signers); stalled || offline {
			p.logger.Debug("abandoning presigning session", zap.String("session", id), zap.Uint64s("signers", s.signers), zap.Bool("member_offline", offline))
			s.h.Stop()
			delete(p.sessions, id)
		}
	}
	for id, msgs := range p.early {
		if len(msgs) > 0 && time.Since(msgs[0].at) > presignSessionTTL {
			p.nEarly -= len(msgs)
			delete(p.early, id)
		}
	}
}
