package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/networkconfig"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxMessageSize             = maxConsensusMsgSize
	maxConsensusMsgSize        = 8388608
	maxPartialSignatureMsgSize = 1952
	allowedRoundsInFuture      = 1
	allowedRoundsInPast        = 2
	lateSlotAllowance          = 2
	signatureSize              = 96
	maxDutiesPerEpoch          = 2
)

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	// TODO: consider evicting old data to avoid excessive memory consumption
	Signers *hashmap.Map[spectypes.OperatorID, *SignerState]
}

func (cs *ConsensusState) GetSignerState(signer spectypes.OperatorID) *SignerState {
	signerState, ok := cs.Signers.Get(signer)
	if !ok {
		return nil
	}
	return signerState
}

func (cs *ConsensusState) CreateSignerState(signer spectypes.OperatorID) *SignerState {
	signerState := &SignerState{}
	cs.Signers.Set(signer, signerState)

	return signerState
}

type validatorGetterFunc = func(pk []byte) *ssvtypes.SSVShare

type MessageValidator struct {
	logger          *zap.Logger
	netCfg          networkconfig.NetworkConfig
	index           sync.Map
	shareStorage    registrystorage.Shares
	validatorGetter validatorGetterFunc
}

func NewMessageValidator(netCfg networkconfig.NetworkConfig, shareStorage registrystorage.Shares, opts ...Option) *MessageValidator {
	mv := &MessageValidator{
		logger:       zap.NewNop(),
		netCfg:       netCfg,
		shareStorage: shareStorage,
	}

	for _, opt := range opts {
		opt(mv)
	}

	return mv
}

type Option func(validator *MessageValidator)

func WithLogger(logger *zap.Logger) Option {
	return func(mv *MessageValidator) {
		mv.logger = logger
	}
}

func (mv *MessageValidator) SetValidatorGetter(f validatorGetterFunc) {
	mv.validatorGetter = f
}

func (mv *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage, receivedAt time.Time) (*queue.DecodedSSVMessage, error) {
	if len(ssvMessage.Data) == 0 {
		return nil, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxMessageSize
		return nil, err
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return nil, err
	}

	if !mv.validRole(ssvMessage.MsgID.GetRoleType()) {
		return nil, ErrInvalidRole
	}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(ssvMessage.MsgID.GetPubKey())
	if err != nil {
		return nil, fmt.Errorf("deserialize public key: %w", err)
	}

	share := mv.shareStorage.Get(nil, publicKey.Serialize())
	if share == nil {
		e := ErrUnknownValidator
		e.got = publicKey.SerializeToHexStr()
		return nil, e
	}

	nonCommittee := mv.validatorGetter(publicKey.Serialize()) == nil

	if share.Liquidated {
		return nil, ErrValidatorLiquidated
	}

	// TODO: check if need to return error if no metadata
	if share.BeaconMetadata != nil && !share.BeaconMetadata.IsAttesting() {
		err := ErrValidatorNotAttesting
		err.got = share.BeaconMetadata.Status.String()
		return nil, err
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		if errors.Is(err, queue.ErrUnknownMessageType) {
			e := ErrUnknownMessageType
			e.got = ssvMessage.GetType()
			return nil, e
		}

		e := ErrMalformedMessage
		e.innerErr = err
		return nil, e
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := mv.validateConsensusMessage(share, msg, nonCommittee, receivedAt); err != nil {
			return nil, err
		}
	case spectypes.SSVPartialSignatureMsgType:
		if err := mv.validatePartialSignatureMessage(share, msg); err != nil {
			return nil, err
		}
	case ssvmessage.SSVEventMsgType:
		if err := mv.validateEventMessage(msg); err != nil {
			return nil, err
		}
	case spectypes.DKGMsgType:
		// TODO: handle
	}

	return msg, nil
}

func (mv *MessageValidator) containsSignerFunc(signer spectypes.OperatorID) func(operator *spectypes.Operator) bool {
	return func(operator *spectypes.Operator) bool {
		return operator.OperatorID == signer
	}
}

func (mv *MessageValidator) validateSignatureFormat(signature []byte) error {
	if len(signature) != signatureSize {
		e := ErrWrongSignatureSize
		e.got = len(signature)
		return e
	}

	if [signatureSize]byte(signature) == [signatureSize]byte{} {
		return ErrZeroSignature
	}
	return nil
}

func (mv *MessageValidator) commonSignerValidation(signer spectypes.OperatorID, share *ssvtypes.SSVShare) error {
	if signer == 0 {
		return ErrZeroSigner
	}

	if !slices.ContainsFunc(share.Committee, mv.containsSignerFunc(signer)) {
		return ErrSignerNotInCommittee
	}

	return nil
}

func (mv *MessageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) error {
	if mv.earlyMessage(messageSlot, receivedAt) {
		return ErrEarlyMessage
	}

	if mv.lateMessage(messageSlot, role, receivedAt) {
		return ErrLateMessage
	}

	return nil
}

func (mv *MessageValidator) earlyMessage(slot phase0.Slot, receivedAt time.Time) bool {
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
}

func (mv *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) bool {
	var ttl phase0.Slot
	switch role {
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		ttl = 32 + lateSlotAllowance
	case spectypes.BNRoleValidatorRegistration:
		return false
	}

	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin).Add(clockErrorTolerance)

	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		After(deadline)
}

func (mv *MessageValidator) consensusState(id ConsensusID) *ConsensusState {
	if _, ok := mv.index.Load(id); !ok {
		cs := &ConsensusState{
			Signers: hashmap.New[spectypes.OperatorID, *SignerState](),
		}
		mv.index.Store(id, cs)
	}

	cs, _ := mv.index.Load(id)
	return cs.(*ConsensusState)
}
