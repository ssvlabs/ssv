package validation

import (
	"bytes"
	"context"
	"encoding/hex"
	"math"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

func Test_ValidateSSVMessage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	const validatorIndex = 123

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: v1.ValidatorStateActiveOngoing,
				Index:  validatorIndex,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, share))

	netCfg := networkconfig.TestNetwork

	roleAttester := spectypes.BNRoleAttester

	t.Run("happy flow", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.NoError(t, err)
	})

	t.Run("message counts", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)
		state := validator.consensusState(msgID)
		for i := spectypes.OperatorID(1); i <= 4; i++ {
			signerState := state.GetSignerState(i)
			require.Nil(t, signerState)
		}

		signedMsg := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedMsg, err := signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMsg,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt)
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		state1 := state.GetSignerState(1)
		require.NotNil(t, state1)
		require.EqualValues(t, height, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Proposal: 1}, state1.MessageCounts)
		for i := spectypes.OperatorID(2); i <= 4; i++ {
			signerState := state.GetSignerState(i)
			require.Nil(t, signerState)
		}

		signedMsg = spectestingutils.TestingPrepareMessageWithParams(ks.Shares[1], 1, 2, height, spectestingutils.TestingIdentifier, spectestingutils.TestingQBFTRootData)
		encodedMsg, err = signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg.Data = encodedMsg
		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt)
		require.NoError(t, err)

		require.NotNil(t, state1)
		require.EqualValues(t, height, state1.Slot)
		require.EqualValues(t, 2, state1.Round)
		require.EqualValues(t, MessageCounts{Prepare: 1}, state1.MessageCounts)

		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt)
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedMsg = spectestingutils.TestingCommitMessageWithHeight(ks.Shares[1], 1, height+1)
		encodedMsg, err = signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg.Data = encodedMsg
		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1}, state1.MessageCounts)

		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedMsg = spectestingutils.TestingCommitMultiSignerMessageWithHeight([]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, height+1)
		encodedMsg, err = signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg.Data = encodedMsg
		_, _, err = validator.validateSSVMessage(ssvMsg, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1, Decided: 1, lastDecidedSigners: 3}, state1.MessageCounts)
	})

	t.Run("pubsub message has no data", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		pmsg := &pubsub.Message{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err := validator.validateP2PMessage(context.TODO(), "", pmsg, receivedAt)

		require.ErrorIs(t, err, ErrPubSubMessageHasNoData)
	})

	t.Run("pubsub data too big", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey)[0])
		pmsg := &pubsub.Message{
			Message: &ps_pb.Message{
				Data:  bytes.Repeat([]byte{1}, 10_000_000),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(context.TODO(), "", pmsg, receivedAt)

		e := ErrPubSubDataTooBig
		e.got = 10_000_000
		require.ErrorIs(t, err, e)
	})

	t.Run("malformed network message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey)[0])
		pmsg := &pubsub.Message{
			Message: &ps_pb.Message{
				Data:  []byte{1},
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(context.TODO(), "", pmsg, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
	})

	t.Run("bad data format", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{1}, 500),
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	t.Run("no data", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    []byte{},
		}

		_, _, err := validator.validateSSVMessage(message, time.Now())
		require.ErrorIs(t, err, ErrEmptyData)

		message = &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    nil,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		require.ErrorIs(t, err, ErrEmptyData)
	})

	t.Run("data too big", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		const tooBigMsgSize = maxMessageSize * 2

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{0x1}, tooBigMsgSize),
		}

		_, _, err := validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrSSVDataTooBig
		expectedErr.got = tooBigMsgSize
		expectedErr.want = maxMessageSize
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("data size borderline / malformed message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{0x1}, maxMessageSize),
		}

		_, _, err := validator.validateSSVMessage(message, time.Now())
		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	t.Run("invalid SSV message type", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		message := &spectypes.SSVMessage{
			MsgType: math.MaxUint64,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    []byte{0x1},
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		require.ErrorContains(t, err, ErrUnknownSSVMessageType.Error())
	})

	t.Run("malformed validator public key", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, spectypes.ValidatorPK{}, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		require.ErrorContains(t, err, ErrDeserializePublicKey.Error())
	})

	t.Run("unknown validator", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		sk, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, sk.PublicKey().Marshal(), roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrUnknownValidator
		expectedErr.got = hex.EncodeToString(sk.PublicKey().Marshal())
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("wrong domain", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		wrongDomain := spectypes.DomainType{math.MaxUint8, math.MaxUint8, math.MaxUint8, math.MaxUint8}
		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(wrongDomain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrWrongDomain
		expectedErr.got = hex.EncodeToString(wrongDomain[:])
		expectedErr.want = hex.EncodeToString(netCfg.Domain[:])
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("invalid role", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, math.MaxUint64),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrInvalidRole)
	})

	t.Run("liquidated validator", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		liquidatedSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		liquidatedShare := &ssvtypes.SSVShare{
			Share: *spectestingutils.TestingShare(ks),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: v1.ValidatorStateActiveOngoing,
				},
				Liquidated: true,
			},
		}
		liquidatedShare.ValidatorPubKey = liquidatedSK.PublicKey().Marshal()

		require.NoError(t, ns.Shares().Save(nil, liquidatedShare))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, liquidatedShare.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrValidatorLiquidated
		require.ErrorIs(t, err, expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, liquidatedShare.ValidatorPubKey))
	})

	t.Run("inactive validator", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		inactiveSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		inactiveShare := &ssvtypes.SSVShare{
			Share: *spectestingutils.TestingShare(ks),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: v1.ValidatorStateUnknown,
				},
				Liquidated: false,
			},
		}
		inactiveShare.ValidatorPubKey = inactiveSK.PublicKey().Marshal()

		require.NoError(t, ns.Shares().Save(nil, inactiveShare))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, inactiveShare.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = v1.ValidatorStateUnknown.String()
		require.ErrorIs(t, err, expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, inactiveShare.ValidatorPubKey))
	})

	t.Run("too many duties", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)))
		require.NoError(t, err)

		validSignedMessage = spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height+4)
		encodedValidSignedMessage, err = validSignedMessage.Encode()
		require.NoError(t, err)

		message.Data = encodedValidSignedMessage
		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot+4).Add(validator.waitAfterSlotStart(roleAttester)))
		require.NoError(t, err)

		validSignedMessage = spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height+8)
		encodedValidSignedMessage, err = validSignedMessage.Encode()
		require.NoError(t, err)

		message.Data = encodedValidSignedMessage
		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot+8).Add(validator.waitAfterSlotStart(roleAttester)))
		require.ErrorContains(t, err, ErrTooManyDutiesPerEpoch.Error())
	})

	t.Run("no duties", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		fetcher := &mockDutyFetcher{
			slot:           slot,
			validatorIndex: validatorIndex + 1,
		}
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithDutyFetcher(fetcher), WithSignatureCheck(true))

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, spectypes.BNRoleProposer),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)))
		require.ErrorContains(t, err, ErrNoDuty.Error())

		f := &mockDutyFetcher{
			slot:           slot,
			validatorIndex: validatorIndex,
		}
		validator = NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithDutyFetcher(f), WithSignatureCheck(true))
		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)))
		require.NoError(t, err)
	})

	t.Run("partial message too big", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		for i := 0; i < 13; i++ {
			msg.Message.Messages = append(msg.Message.Messages, msg.Message.Messages[0])
		}

		_, err := msg.Encode()
		require.ErrorContains(t, err, "max expected 13 and 14 found")
	})

	t.Run("signer ID not in committee", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 5, specqbft.Height(slot))

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrSignerNotInCommittee)
	})

	t.Run("partial zero signer ID", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 0, specqbft.Height(slot))

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrZeroSigner)
	})

	t.Run("partial inconsistent signer ID", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages[0].Signer = 2

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrUnexpectedSigner
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("partial duplicated message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages = append(msg.Message.Messages, msg.Message.Messages[0])

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrDuplicatedPartialSignatureMessage)
	})

	t.Run("no partial messages", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages = []*spectypes.PartialSignatureMessage{}

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrNoPartialMessages)
	})

	t.Run("partial wrong signature size", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Signature = []byte{1}

		encoded, err := msg.Encode()
		require.ErrorContains(t, err, "bytes array does not have the correct length")

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	t.Run("partial wrong signature", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Signature = bytes.Repeat([]byte{1}, 96)

		encoded, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorContains(t, err, ErrInvalidSignature.Error())
	})

	t.Run("partial message type", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(162304)

		t.Run("valid", func(t *testing.T) {
			tests := map[spectypes.BeaconRole][]spectypes.PartialSigMsgType{
				spectypes.BNRoleAttester:                  {spectypes.PostConsensusPartialSig},
				spectypes.BNRoleAggregator:                {spectypes.PostConsensusPartialSig, spectypes.SelectionProofPartialSig},
				spectypes.BNRoleProposer:                  {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig},
				spectypes.BNRoleSyncCommittee:             {spectypes.PostConsensusPartialSig},
				spectypes.BNRoleSyncCommitteeContribution: {spectypes.PostConsensusPartialSig, spectypes.ContributionProofs},
				spectypes.BNRoleValidatorRegistration:     {spectypes.ValidatorRegistrationPartialSig},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))
					msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, role)

					msg := &spectypes.SignedPartialSignatureMessage{
						Message: spectypes.PartialSignatureMessages{
							Type: msgType,
						},
						Signature: make([]byte, 96),
						Signer:    1,
					}

					encoded, err := msg.Encode()
					require.NoError(t, err)

					message := &spectypes.SSVMessage{
						MsgType: spectypes.SSVPartialSignatureMsgType,
						MsgID:   msgID,
						Data:    encoded,
					}

					receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
					_, _, err = validator.validateSSVMessage(message, receivedAt)
					require.ErrorContains(t, err, ErrNoPartialMessages.Error())
				}
			}
		})

		t.Run("invalid", func(t *testing.T) {
			validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))
			msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)

			msg := &spectypes.SignedPartialSignatureMessage{
				Message: spectypes.PartialSignatureMessages{
					Type: math.MaxUint64,
				},
				Signature: make([]byte, 96),
				Signer:    1,
			}

			encoded, err := msg.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   msgID,
				Data:    encoded,
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
			_, _, err = validator.validateSSVMessage(message, receivedAt)
			require.ErrorContains(t, err, ErrUnknownPartialMessageType.Error())
		})

		t.Run("mismatch", func(t *testing.T) {
			tests := map[spectypes.BeaconRole][]spectypes.PartialSigMsgType{
				spectypes.BNRoleAttester:                  {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleAggregator:                {spectypes.RandaoPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleProposer:                  {spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleSyncCommittee:             {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleSyncCommitteeContribution: {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleValidatorRegistration:     {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))
					msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, role)

					msg := &spectypes.SignedPartialSignatureMessage{
						Message: spectypes.PartialSignatureMessages{
							Type: msgType,
						},
						Signature: make([]byte, 96),
						Signer:    1,
					}

					encoded, err := msg.Encode()
					require.NoError(t, err)

					message := &spectypes.SSVMessage{
						MsgType: spectypes.SSVPartialSignatureMsgType,
						MsgID:   msgID,
						Data:    encoded,
					}

					receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
					_, _, err = validator.validateSSVMessage(message, receivedAt)
					require.ErrorContains(t, err, ErrPartialSignatureTypeRoleMismatch.Error())
				}
			}
		})
	})

	t.Run("invalid QBFT message type", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		msg := &specqbft.Message{
			MsgType:    math.MaxUint64,
			Height:     height,
			Round:      specqbft.FirstRound,
			Identifier: spectestingutils.TestingIdentifier,
			Root:       spectestingutils.TestingQBFTRootData,
		}
		signedMsg := spectestingutils.SignQBFTMsg(ks.Shares[1], 1, msg)

		encodedValidSignedMessage, err := signedMsg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrUnknownQBFTMessageType
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("wrong signature size", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Signature = []byte{0x1}

		_, err := validSignedMessage.Encode()
		require.Error(t, err)
	})

	t.Run("zero signature", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		t.Run("consensus message", func(t *testing.T) {
			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
			zeroSignature := [signatureSize]byte{}
			validSignedMessage.Signature = zeroSignature[:]

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
				Data:    encoded,
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt)
			require.ErrorIs(t, err, ErrZeroSignature)
		})

		t.Run("partial signature message", func(t *testing.T) {
			partialSigMessage := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, height)
			zeroSignature := [signatureSize]byte{}
			partialSigMessage.Signature = zeroSignature[:]

			encoded, err := partialSigMessage.Encode()
			require.NoError(t, err)

			ssvMessage := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
				Data:    encoded,
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(ssvMessage, receivedAt)
			require.ErrorIs(t, err, ErrZeroSignature)
		})
	})

	t.Run("no signers", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Signers = []spectypes.OperatorID{}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrNoSigners)
	})

	t.Run("zero signer", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		inactiveSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		zeroSignerKS := spectestingutils.Testing7SharesSet()
		zeroSignerShare := &ssvtypes.SSVShare{
			Share: *spectestingutils.TestingShare(zeroSignerKS),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: v1.ValidatorStateActiveOngoing,
				},
				Liquidated: false,
			},
		}
		zeroSignerShare.Committee[0].OperatorID = 0
		zeroSignerShare.ValidatorPubKey = inactiveSK.PublicKey().Marshal()

		require.NoError(t, ns.Shares().Save(nil, zeroSignerShare))

		t.Run("consensus message", func(t *testing.T) {
			validSignedMessage := spectestingutils.TestingProposalMessage(zeroSignerKS.Shares[1], 1)
			validSignedMessage.Signers = []spectypes.OperatorID{0}

			encodedValidSignedMessage, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(netCfg.Domain, zeroSignerShare.ValidatorPubKey, roleAttester),
				Data:    encodedValidSignedMessage,
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt)
			require.ErrorIs(t, err, ErrZeroSigner)
		})

		t.Run("partial signature message", func(t *testing.T) {
			partialSignatureMessage := spectestingutils.PostConsensusAttestationMsg(zeroSignerKS.Shares[1], 1, specqbft.Height(slot))
			partialSignatureMessage.Signer = 0

			encoded, err := partialSignatureMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   spectypes.NewMsgID(netCfg.Domain, zeroSignerShare.ValidatorPubKey, roleAttester),
				Data:    encoded,
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt)
			require.ErrorIs(t, err, ErrZeroSigner)
		})

		require.NoError(t, ns.Shares().Delete(nil, zeroSignerShare.ValidatorPubKey))
	})

	t.Run("non unique signer", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{1, 2, 2}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrDuplicatedSigner)
	})

	t.Run("signers not sorted", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{3, 2, 1}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrSignersNotSorted)
	})

	t.Run("wrong signers length", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{1, 2}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrWrongSignersLength
		expectedErr.got = 2
		expectedErr.want = "between 3 and 4"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("non decided with multiple signers", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingMultiSignerProposalMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrNonDecidedWithMultipleSigners
		expectedErr.got = 3
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("wrong signed signature", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		validSignedMessage.Signature = bytes.Repeat([]byte{1}, 96)

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		require.ErrorContains(t, err, ErrInvalidSignature.Error())
	})

	t.Run("late message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		tests := map[spectypes.BeaconRole]time.Time{
			spectypes.BNRoleAttester:                  netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(roleAttester)),
			spectypes.BNRoleAggregator:                netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(roleAttester)),
			spectypes.BNRoleProposer:                  netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(roleAttester)),
			spectypes.BNRoleSyncCommittee:             netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(roleAttester)),
			spectypes.BNRoleSyncCommitteeContribution: netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(roleAttester)),
		}

		for role, receivedAt := range tests {
			t.Run(role.String(), func(t *testing.T) {
				msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, role)

				message := &spectypes.SSVMessage{
					MsgType: spectypes.SSVConsensusMsgType,
					MsgID:   msgID,
					Data:    encodedValidSignedMessage,
				}

				_, _, err = validator.validateSSVMessage(message, receivedAt)
				require.ErrorContains(t, err, ErrLateMessage.Error())
			})
		}
	})

	t.Run("early message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot - 1)
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		require.ErrorIs(t, err, ErrEarlyMessage)
	})

	t.Run("not leader", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[2], 2, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrSignerNotLeader
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("malformed prepare justification", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Message.PrepareJustification = [][]byte{{1}}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPrepareJustifications.Error())
	})

	t.Run("non-proposal with prepare justification", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.TestingProposalMessageWithParams(
			ks.Shares[1], spectypes.OperatorID(1), specqbft.FirstRound, specqbft.FirstHeight, spectestingutils.TestingQBFTRootData,
			nil,
			spectestingutils.MarshalJustifications([]*specqbft.SignedMessage{
				spectestingutils.TestingRoundChangeMessage(ks.Shares[1], spectypes.OperatorID(1)),
			}))
		msg.Message.MsgType = specqbft.PrepareMsgType

		encodedValidSignedMessage, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrUnexpectedPrepareJustifications
		expectedErr.got = specqbft.PrepareMsgType
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("non-proposal with round change justification", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.TestingProposalMessageWithParams(
			ks.Shares[1], spectypes.OperatorID(1), specqbft.FirstRound, specqbft.FirstHeight, spectestingutils.TestingQBFTRootData,
			spectestingutils.MarshalJustifications([]*specqbft.SignedMessage{
				spectestingutils.TestingPrepareMessage(ks.Shares[1], spectypes.OperatorID(1)),
			}),
			nil,
		)
		msg.Message.MsgType = specqbft.PrepareMsgType

		encodedValidSignedMessage, err := msg.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrUnexpectedRoundChangeJustifications
		expectedErr.got = specqbft.PrepareMsgType
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("malformed round change justification", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Message.RoundChangeJustification = [][]byte{{1}}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		require.ErrorContains(t, err, ErrMalformedRoundChangeJustifications.Error())
	})

	t.Run("wrong root hash", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.FullData = []byte{1}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrInvalidHash
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("double proposal with different data", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		message1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned1,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
		signed2.FullData = []byte{1}
		signed2.Message.Root, err = specqbft.HashDataRoot(signed2.FullData)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		message2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned2,
		}

		_, _, err = validator.validateSSVMessage(message2, receivedAt)
		expectedErr := ErrDuplicatedProposalWithDifferentData
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("double prepare", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		message1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned1,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		message2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned2,
		}

		_, _, err = validator.validateSSVMessage(message2, receivedAt)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "prepare, having pre-consensus: 0, proposal: 0, prepare: 1, commit: 0, decided: 0, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("double commit", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		message1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned1,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		message2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned2,
		}

		_, _, err = validator.validateSSVMessage(message2, receivedAt)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 1, decided: 0, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("double round change", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		message1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned1,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		message2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned2,
		}

		_, _, err = validator.validateSSVMessage(message2, receivedAt)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 0, round change: 1, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("too many decided", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)

		signed := spectestingutils.TestingCommitMultiSignerMessageWithRound(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, 1)
		encodedSigned, err := signed.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedSigned,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		state := validator.consensusState(msgID)
		for i := 0; i < maxDecidedCount(len(share.Committee)); i++ {
			_, _, err = validator.validateSSVMessage(message, receivedAt)
			require.NoError(t, err)

			for i := spectypes.OperatorID(1); i <= 3; i++ {
				state.GetSignerState(i).MessageCounts.lastDecidedSigners = 0
			}
		}

		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "decided, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 8, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("decided not increasing signer count", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingCommitMultiSignerMessageWithRound(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		message1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned1,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingCommitMultiSignerMessageWithRound(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, 1)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		message2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedSigned2,
		}

		_, _, err = validator.validateSSVMessage(message2, receivedAt)
		expectedErr := ErrDecidedSignersSequence
		expectedErr.got = 3
		expectedErr.want = "more than 3"
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("round too high", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		tests := map[spectypes.BeaconRole]specqbft.Round{
			spectypes.BNRoleAttester:                  13,
			spectypes.BNRoleAggregator:                13,
			spectypes.BNRoleProposer:                  7,
			spectypes.BNRoleSyncCommittee:             7,
			spectypes.BNRoleSyncCommitteeContribution: 7,
			spectypes.BNRoleValidatorRegistration:     1,
		}

		for role, round := range tests {
			t.Run(role.String(), func(t *testing.T) {
				msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, role)

				signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, round)
				encodedMessage, err := signedMessage.Encode()
				require.NoError(t, err)

				ssvMessage := &spectypes.SSVMessage{
					MsgType: spectypes.SSVConsensusMsgType,
					MsgID:   msgID,
					Data:    encodedMessage,
				}

				receivedAt := netCfg.Beacon.GetSlotStartTime(0).Add(validator.waitAfterSlotStart(role))
				_, _, err = validator.validateSSVMessage(ssvMessage, receivedAt)
				require.ErrorContains(t, err, ErrRoundTooHigh.Error())
			})
		}
	})

	t.Run("round already advanced", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 2)
		encodedMessage, err := signedMessage.Encode()
		require.NoError(t, err)

		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMessage,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(ssvMessage, receivedAt)
		require.NoError(t, err)

		signedMessage = spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 1)
		encodedMessage, err = signedMessage.Encode()
		require.NoError(t, err)

		ssvMessage.Data = encodedMessage
		_, _, err = validator.validateSSVMessage(ssvMessage, receivedAt)
		require.ErrorContains(t, err, ErrRoundAlreadyAdvanced.Error())
	})

	t.Run("slot already advanced", func(t *testing.T) {
		msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		t.Run("consensus message", func(t *testing.T) {
			validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

			signedMessage := spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height+1)
			encodedMessage, err := signedMessage.Encode()
			require.NoError(t, err)

			ssvMessage := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			_, _, err = validator.validateSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(roleAttester)))
			require.NoError(t, err)

			signedMessage = spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height)
			encodedMessage, err = signedMessage.Encode()
			require.NoError(t, err)

			ssvMessage.Data = encodedMessage
			_, _, err = validator.validateSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)))
			require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
		})

		t.Run("partial signature message", func(t *testing.T) {
			validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

			message := spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height+1)
			message.Message.Slot = phase0.Slot(height) + 1
			sig, err := spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
			require.NoError(t, err)
			message.Signature = sig

			encodedMessage, err := message.Encode()
			require.NoError(t, err)

			ssvMessage := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			_, _, err = validator.validateSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(roleAttester)))
			require.NoError(t, err)

			message = spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height)
			message.Message.Slot = phase0.Slot(height)
			sig, err = spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
			require.NoError(t, err)
			message.Signature = sig

			encodedMessage, err = message.Encode()
			require.NoError(t, err)

			ssvMessage.Data = encodedMessage
			_, _, err = validator.validateSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)))
			require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
		})
	})

	t.Run("event message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()), WithSignatureCheck(true))

		msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		eventMsg := &ssvtypes.EventMsg{}
		encoded, err := eventMsg.Encode()
		require.NoError(t, err)

		ssvMessage := &spectypes.SSVMessage{
			MsgType: ssvmessage.SSVEventMsgType,
			MsgID:   msgID,
			Data:    encoded,
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(ssvMessage, receivedAt)
		require.ErrorIs(t, err, ErrEventMessage)
	})
}

type mockDutyFetcher struct {
	slot           phase0.Slot
	validatorIndex phase0.ValidatorIndex
}

func (m mockDutyFetcher) ProposerDuty(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) *v1.ProposerDuty {
	if slot == m.slot && validatorIndex == m.validatorIndex {
		return &v1.ProposerDuty{}
	}

	return nil
}

func (m mockDutyFetcher) SyncCommitteeDuty(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) *v1.SyncCommitteeDuty {
	if slot == m.slot && validatorIndex == m.validatorIndex {
		return &v1.SyncCommitteeDuty{}
	}

	return nil
}
