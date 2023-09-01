package validation

import (
	"bytes"
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
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
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

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: v1.ValidatorStateActiveOngoing,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, share))

	netCfg := networkconfig.TestNetwork

	roleAttester := spectypes.BNRoleAttester

	t.Run("happy flow", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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

	t.Run("bad format", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{1}, 500),
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)

		expectedErr := ErrDataTooBig
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("no data", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		const tooBigMsgSize = maxMessageSize * 2

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{0x1}, tooBigMsgSize),
		}

		_, _, err := validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrDataTooBig
		expectedErr.got = tooBigMsgSize
		expectedErr.want = maxMessageSize
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("data size borderline / malformed message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    bytes.Repeat([]byte{0x1}, maxMessageSize),
		}

		_, _, err := validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrMalformedMessage
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("invalid SSV message type", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		message := &spectypes.SSVMessage{
			MsgType: math.MaxUint64,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    []byte{0x1},
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrUnknownSSVMessageType
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("malformed validator public key", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		message := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(netCfg.Domain, spectypes.ValidatorPK{}, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrDeserializePublicKey
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("unknown validator", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("wrong domain", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		require.ErrorAs(t, err, &expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, liquidatedShare.ValidatorPubKey))
	})

	t.Run("inactive validator", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		_, _, err = validator.validateSSVMessage(message, time.Now())
		expectedErr := ErrValidatorNotAttesting
		require.ErrorAs(t, err, &expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, inactiveShare.ValidatorPubKey))
	})

	t.Run("partial signer ID not in committee", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		expectedErr := ErrSignerNotInCommittee
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("partial zero signer ID", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		expectedErr := ErrSignerNotInCommittee
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("partial inconsistent signer ID", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("partial wrong signature size", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		expectedErr := ErrMalformedMessage
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("partial wrong signature", func(t *testing.T) {
		t.Skip() // TODO: enable when signature check is enabled

		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		expectedErr := ErrInvalidSignature
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("invalid QBFT message type", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		require.ErrorAs(t, err, &expectedErr)
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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

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

	t.Run("no signers", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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

		require.NoError(t, ns.Shares().Delete(nil, zeroSignerShare.ValidatorPubKey))
	})

	t.Run("non unique signer", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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

	t.Run("late message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot + phase0.Slot(netCfg.Beacon.SlotsPerEpoch()*3)).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt)
		expectedErr := ErrLateMessage
		require.ErrorAs(t, err, &expectedErr)
	})

	t.Run("early message", func(t *testing.T) {
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
		validator := NewMessageValidator(netCfg, WithShareStorage(ns.Shares()))

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
}
