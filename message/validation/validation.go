package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
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
	allowedRoundsInFuture      = 2
	allowedRoundsInPast        = 1
	lateSlotAllowance          = 2
	signatureSize              = 96
)

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	Signers *hashmap.Map[spectypes.OperatorID, *SignerState]
}

func (cs *ConsensusState) SignerState(signer spectypes.OperatorID) *SignerState {
	signerState, ok := cs.Signers.Get(signer)
	if !ok {
		signerState = &SignerState{}
		cs.Signers.Set(signer, signerState)
	}

	return signerState
}

type MessageValidator struct {
	mu           sync.Mutex
	netCfg       networkconfig.NetworkConfig
	index        map[ConsensusID]*ConsensusState
	shareStorage registrystorage.Shares
}

func NewMessageValidator(netCfg networkconfig.NetworkConfig, shareStorage registrystorage.Shares) *MessageValidator {
	return &MessageValidator{
		netCfg:       netCfg,
		index:        make(map[ConsensusID]*ConsensusState),
		shareStorage: shareStorage,
	}
}

func (mv *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage, receivedAt time.Time) (*queue.DecodedSSVMessage, error) {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	if len(ssvMessage.Data) == 0 {
		return nil, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrDataTooBig
		err.got = len(ssvMessage.Data)
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

	share := mv.shareStorage.Get(nil, ssvMessage.MsgID.GetPubKey())
	if share == nil {
		return nil, ErrUnknownValidator
	}

	if share.Liquidated {
		return nil, ErrValidatorLiquidated
	}

	// TODO: return error if no metadata?
	if share.BeaconMetadata != nil && !share.BeaconMetadata.IsAttesting() {
		return nil, ErrValidatorNotAttesting
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := mv.validateConsensusMessage(share, msg, receivedAt); err != nil {
			return nil, err
		}
	case spectypes.SSVPartialSignatureMsgType:
	// TODO: uncomment
	//if err := mv.validatePartialSignatureMessage(share, msg); err != nil {
	//	return nil, err
	//}
	case ssvmessage.SSVEventMsgType:
		if err := mv.validateEventMessage(msg); err != nil {
			return nil, err
		}
	case spectypes.DKGMsgType: // TODO: handle
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
		return fmt.Errorf("wrong signature size: %d", len(signature))
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

func (mv *MessageValidator) validateSlotState(signerState *SignerState, msgSlot phase0.Slot) error {
	if signerState.Slot > msgSlot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = msgSlot
		return err
	}

	// Advance slot & round, if needed.
	if signerState.Slot < msgSlot {
		signerState.Reset(msgSlot, specqbft.FirstRound)
	}

	return nil
}

func (mv *MessageValidator) validateRoundState(signerState *SignerState, msgRound specqbft.Round) error {
	if signerState.Round > msgRound {
		// Signers aren't allowed to decrease their round.
		// If they've sent a future message due to clock error,
		// they'd have to wait for the next slot/round to be accepted.
		err := ErrRoundAlreadyAdvanced
		err.want = signerState.Round
		err.got = msgRound
		return err
	}

	if msgRound-signerState.Round > allowedRoundsInFuture {
		err := ErrRoundTooFarInTheFuture
		err.want = signerState.Round
		err.got = msgRound
		return err
	}

	// Advance slot & round, if needed.
	if signerState.Round < msgRound {
		signerState.Reset(signerState.Slot, msgRound)
	}

	return nil
}

func (mv *MessageValidator) validateSlotAndRoundState(signerState *SignerState, msgSlot phase0.Slot, msgRound specqbft.Round) error {
	// TODO: make sure it's correct
	//if signerState.Slot < msgSlot && signerState.Round >= msgRound || signerState.Round < msgRound && signerState.Slot >= msgSlot {
	//return ErrFutureSlotRoundMismatch
	//}

	if err := mv.validateSlotState(signerState, msgSlot); err != nil {
		return err
	}

	if err := mv.validateRoundState(signerState, msgRound); err != nil {
		return err
	}

	return nil
}

func (mv *MessageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) error {
	if mv.earlyMessage(messageSlot, receivedAt) {
		return ErrEarlyMessage
	}

	if mv.lateMessage(messageSlot, role, receivedAt) {
		// TODO: make sure check is correct
		//return ErrLateMessage
	}

	return nil
}

func (mv *MessageValidator) earlyMessage(slot phase0.Slot, receivedAt time.Time) bool {
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
}

func (mv *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) bool {
	// Note: this is just an example, should be according to ssv-spec.
	var ttl phase0.Slot
	switch role {
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		ttl = 32 + lateSlotAllowance
	case spectypes.BNRoleValidatorRegistration:
		ttl = 1
	}
	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin).Add(clockErrorTolerance)
	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		After(deadline)
}

func (mv *MessageValidator) consensusState(id ConsensusID) *ConsensusState {
	if _, ok := mv.index[id]; !ok {
		mv.index[id] = &ConsensusState{
			Signers: hashmap.New[spectypes.OperatorID, *SignerState](),
		}
	}
	return mv.index[id]
}
