// Package kms is the friends-mode distributed key management service (dKMS): SSV operators hold
// threshold ECDSA keys (from ssv-dkg's KMS ceremony) and sign transactions for them when the key's
// on-chain policy allows, using the SSV protocol stack (p2p, message validation, QBFT) without any
// beacon chain.
package kms

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	dkgkms "github.com/ssvlabs/ssv-dkg/pkgs/kms"

	"github.com/ssvlabs/ssv/kms/kmstypes"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const kmsMsgType = ssvmessage.SSVKMSMsgType

// Network is what the dKMS needs from the p2p network: committee topics keyed by operator set.
type Network interface {
	SubscribeOperators(operatorIDs []spectypes.OperatorID) error
	BroadcastToOperators(operatorIDs []spectypes.OperatorID, msg *spectypes.SignedSSVMessage) error
}

// OperatorKeys returns registered operators' base64-PEM RSA public keys.
type OperatorKeys func(id spectypes.OperatorID) (string, error)

// Options configures the service.
type Options struct {
	Logger        *zap.Logger
	DB            basedb.Database
	Network       Network
	NetworkConfig *networkconfig.Network
	OperatorID    func() spectypes.OperatorID
	Signer        ssvtypes.OperatorSigner
	OperatorKey   keys.OperatorPrivateKey
	OperatorKeys  OperatorKeys
	Chain         Chain
	// PolicyContract is the SSVKMSPolicy contract the chain's policy reads come from.
	PolicyContract common.Address
	// BroadcastTxs makes every operator submit signed transactions to the chain.
	BroadcastTxs bool
	// Confirmations pins policy and nonce reads this many blocks behind the chain head.
	Confirmations uint64
}

// Result statuses.
const (
	StatusPending = "pending"
	StatusSigning = "signing"
	StatusSigned  = "signed"
	StatusFailed  = "failed"
)

// Result is the outcome of a signing request.
type Result struct {
	ID        string                `json:"id"`
	KeyID     string                `json:"key_id"`
	Status    string                `json:"status"`
	Height    uint64                `json:"height"`
	Signers   []uint64              `json:"signers,omitempty"`
	Signature string                `json:"signature,omitempty"`
	RawTx     string                `json:"raw_tx,omitempty"`
	TxHash    string                `json:"tx_hash,omitempty"`
	Error     string                `json:"error,omitempty"`
	Request   *kmstypes.SignRequest `json:"request,omitempty"`
}

// Service runs every dKMS key this operator holds a share of.
type Service struct {
	logger       *zap.Logger
	store        *store
	net          Network
	netCfg       *networkconfig.Network
	operatorID   func() spectypes.OperatorID
	signer       ssvtypes.OperatorSigner
	operatorKey  keys.OperatorPrivateKey
	operatorKeys OperatorKeys
	chain        Chain
	policy       common.Address
	broadcastTxs bool
	confs        uint64

	ctx     context.Context
	mu      sync.RWMutex
	runners map[[32]byte]*keyRunner
	results map[string]*Result
}

// New creates the service.
func New(opts Options) (*Service, error) {
	st, err := newStore(opts.DB, opts.OperatorKey)
	if err != nil {
		return nil, err
	}
	return &Service{
		logger:       opts.Logger.Named("dkms"),
		store:        st,
		net:          opts.Network,
		netCfg:       opts.NetworkConfig,
		operatorID:   opts.OperatorID,
		signer:       opts.Signer,
		operatorKey:  opts.OperatorKey,
		operatorKeys: opts.OperatorKeys,
		chain:        opts.Chain,
		policy:       opts.PolicyContract,
		broadcastTxs: opts.BroadcastTxs,
		confs:        opts.Confirmations,
		runners:      map[[32]byte]*keyRunner{},
		results:      map[string]*Result{},
	}, nil
}

// Start loads the stored keys and runs them.
func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	files, err := s.store.keyFiles()
	if err != nil {
		return err
	}
	for _, kf := range files {
		if _, err := s.addKey(kf, false); err != nil {
			s.logger.Error("could not load dKMS key", zap.String("key", kf.KeyID), zap.Error(err))
		}
	}
	s.logger.Info("dKMS started", zap.Int("keys", len(files)), zap.Uint64("chain_id", s.chain.ChainID()))
	return nil
}

// SetNetwork sets the p2p network, which is created after the service (its message validation
// needs the service's key registry).
func (s *Service) SetNetwork(n Network) { s.net = n }

func (s *Service) decrypt(wrapped []byte) ([]byte, error) { return s.operatorKey.Decrypt(wrapped) }

// RegisterKey adds a key from a ceremony's key file.
func (s *Service) RegisterKey(kf *dkgkms.KeyFile) (KeyInfo, error) {
	return s.addKey(kf, true)
}

func (s *Service) addKey(kf *dkgkms.KeyFile, save bool) (KeyInfo, error) {
	key, err := openKey(kf, s.operatorID(), s.decrypt, s.operatorKeys)
	if err != nil {
		return KeyInfo{}, err
	}
	s.mu.Lock()
	if r, ok := s.runners[key.ID]; ok {
		s.mu.Unlock()
		return r.info(), nil
	}
	s.mu.Unlock()
	if save {
		if err := s.store.saveKeyFile(kf); err != nil {
			return KeyInfo{}, err
		}
	}
	r, err := newKeyRunner(s, key)
	if err != nil {
		return KeyInfo{}, err
	}
	if err := s.net.SubscribeOperators(key.Committee); err != nil {
		return KeyInfo{}, fmt.Errorf("subscribe to committee topic: %w", err)
	}
	s.mu.Lock()
	s.runners[key.ID] = r
	s.mu.Unlock()
	go r.run(s.ctx)
	r.logger.Info("🔑 dKMS key loaded", zap.Uint64s("operators", key.Committee), zap.Int("threshold", key.Threshold), zap.Uint64("height", r.height))
	return r.info(), nil
}

func (s *Service) runner(keyID [32]byte) *keyRunner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runners[keyID]
}

// KMSCommittee implements validation.KMSCommittees.
func (s *Service) KMSCommittee(keyID [32]byte) ([]spectypes.OperatorID, bool) {
	r := s.runner(keyID)
	if r == nil {
		return nil, false
	}
	return r.key.Committee, true
}

// Route implements network.MessageRouter: it hands validated dKMS messages to their key's runner.
func (s *Service) Route(ctx context.Context, m network.DecodedSSVMessage) {
	msg, ok := m.(*queue.SSVMessage)
	if !ok || msg.SignedSSVMessage == nil || msg.GetID().GetRoleType() != ssvtypes.RoleKMS {
		return
	}
	r := s.runner(kmstypes.KeyIDFromMessageID(msg.GetID()))
	if r == nil {
		return
	}
	r.do(func() { r.onMessage(s.ctx, msg) })
}

// Keys lists the keys this operator holds.
func (s *Service) Keys() []KeyInfo {
	s.mu.RLock()
	runners := make([]*keyRunner, 0, len(s.runners))
	for _, r := range s.runners {
		runners = append(runners, r)
	}
	s.mu.RUnlock()
	out := make([]KeyInfo, 0, len(runners))
	for _, r := range runners {
		info := make(chan KeyInfo, 1)
		if r.do(func() { info <- r.info() }) {
			select {
			case ki := <-info:
				out = append(out, ki)
			case <-time.After(5 * time.Second):
			}
		}
	}
	return out
}

// RejectedError is a request the policy or the request checks refuse.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return e.Reason }

// Submit checks a signing request against the current chain state and queues it for the key's
// committee. The same checks run again in consensus, pinned to the proposal's block.
func (s *Service) Submit(ctx context.Context, req *kmstypes.SignRequest) (string, error) {
	var keyID [32]byte
	if len(req.KeyID) != 32 {
		return "", &RejectedError{"key_id must be 32 bytes"}
	}
	copy(keyID[:], req.KeyID)
	r := s.runner(keyID)
	if r == nil {
		return "", &RejectedError{"unknown key"}
	}
	id, err := req.ID()
	if err != nil {
		return "", &RejectedError{err.Error()}
	}
	if res, ok := s.Result(id); ok && res.Status != StatusFailed {
		return id, nil
	}
	block, err := s.policyBlock(ctx)
	if err != nil {
		return "", fmt.Errorf("chain head: %w", err)
	}
	if err := s.checkRequest(ctx, r, req, block); err != nil {
		return "", err
	}
	s.putResult(&Result{ID: id, KeyID: r.key.IDHex, Status: StatusPending, Request: req})
	r.do(func() { r.enqueue(req) })
	r.send(&kmstypes.Message{Kind: kmstypes.KindRequest, Request: req})
	r.logger.Info("dKMS request accepted", zap.String("request", id), zap.String("requester", req.Requester.Hex()), zap.String("to", req.Tx.To.Hex()))
	return id, nil
}

// policyBlock is the block policy checks read at: the head minus the configured confirmations.
func (s *Service) policyBlock(ctx context.Context) (uint64, error) {
	head, err := s.chain.HeadNumber(ctx)
	if err != nil {
		return 0, err
	}
	if head < s.confs {
		return 0, nil
	}
	return head - s.confs, nil
}

// checkRequest is the policy gate, evaluated against the chain at block.
func (s *Service) checkRequest(ctx context.Context, r *keyRunner, req *kmstypes.SignRequest, block uint64) error {
	if len(req.KeyID) != 32 || [32]byte(req.KeyID) != r.key.ID {
		return &RejectedError{"request is for another key"}
	}
	if err := req.VerifyRequester(); err != nil {
		return &RejectedError{err.Error()}
	}
	if req.Tx.ChainID != s.chain.ChainID() {
		return &RejectedError{fmt.Sprintf("chain id %d is not supported (only %d)", req.Tx.ChainID, s.chain.ChainID())}
	}
	tx, err := req.Tx.Transaction()
	if err != nil {
		return &RejectedError{err.Error()}
	}
	if tx.Value().Cmp(maxUint256) > 0 {
		return &RejectedError{"value too large"}
	}
	digest, err := req.Tx.Digest()
	if err != nil {
		return &RejectedError{err.Error()}
	}
	nonce, err := s.chain.NonceAt(ctx, r.key.Address, block)
	if err != nil {
		return fmt.Errorf("nonce: %w", err)
	}
	if req.Tx.Nonce < nonce {
		return &RejectedError{fmt.Sprintf("nonce %d already used (account nonce %d)", req.Tx.Nonce, nonce)}
	}
	r.stateMu.RLock()
	other, ok := r.state.Nonces[req.Tx.Nonce]
	r.stateMu.RUnlock()
	if ok {
		if id, _ := req.ID(); other != id {
			return &RejectedError{fmt.Sprintf("nonce %d already signed for another request", req.Tx.Nonce)}
		}
	}
	ok, reason, err := s.chain.CheckPolicy(ctx, block, PolicyQuery{
		Key:       r.key.Address,
		Requester: req.Requester,
		To:        req.Tx.To,
		Value:     new(big.Int).Set(tx.Value()),
		Data:      req.Tx.Data,
		Digest:    [32]byte(digest),
	})
	if err != nil {
		return err
	}
	if !ok {
		return &RejectedError{"policy: " + reason}
	}
	return nil
}

func (s *Service) putResult(res *Result) {
	s.mu.Lock()
	s.results[res.ID] = res
	s.mu.Unlock()
	if res.Status != StatusPending {
		if err := s.store.saveResult(res); err != nil {
			s.logger.Warn("could not store result", zap.Error(err))
		}
	}
}

// Result returns a request's result.
func (s *Service) Result(id string) (*Result, bool) {
	s.mu.RLock()
	res, ok := s.results[id]
	s.mu.RUnlock()
	if ok {
		return res, true
	}
	res, ok, err := s.store.result(id)
	if err != nil || !ok {
		return nil, false
	}
	return res, true
}

// Wait waits for a request to reach a final status.
func (s *Service) Wait(ctx context.Context, id string) (*Result, error) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if res, ok := s.Result(id); ok && (res.Status == StatusSigned || res.Status == StatusFailed) {
			return res, nil
		}
		select {
		case <-ctx.Done():
			if res, ok := s.Result(id); ok {
				return res, nil
			}
			return nil, errors.New("timed out")
		case <-t.C:
		}
	}
}

// KeyAddress returns the address of a key, for clients building requests.
func (s *Service) KeyAddress(keyID [32]byte) (common.Address, bool) {
	r := s.runner(keyID)
	if r == nil {
		return common.Address{}, false
	}
	return r.key.Address, true
}
