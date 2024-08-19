package validation

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	alanspectypes "github.com/ssvlabs/ssv-spec/types"
	alanspectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/genesisqueue"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func Test_ValidateSSVMessage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	const validatorIndex = 123

	ks := alanspectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  validatorIndex,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, share))

	netCfg := networkconfig.TestNetwork

	roleAttester := spectypes.BNRoleAttester

	// Message validation happy flow, messages are not ignored or rejected and there are no errors
	t.Run("happy flow", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.NoError(t, err)
	})

	// Make sure messages are incremented and throw an ignore message if more than 1 for a commit
	t.Run("message counts", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)
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

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
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

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMsg,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		require.NoError(t, err)

		require.NotNil(t, state1)
		require.EqualValues(t, height, state1.Slot)
		require.EqualValues(t, 2, state1.Round)
		require.EqualValues(t, MessageCounts{Prepare: 1}, state1.MessageCounts)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedMsg = spectestingutils.TestingCommitMessageWithHeight(ks.Shares[1], 1, height+1)
		encodedMsg, err = signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg3 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMsg,
		}

		message3, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg3)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message3, receivedAt.Add(netCfg.Beacon.SlotDurationSec()), nil)
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1}, state1.MessageCounts)

		_, _, err = validator.validateSSVMessage(message3, receivedAt.Add(netCfg.Beacon.SlotDurationSec()), nil)
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedMsg = spectestingutils.TestingCommitMultiSignerMessageWithHeight([]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, height+1)
		encodedMsg, err = signedMsg.Encode()
		require.NoError(t, err)

		ssvMsg4 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMsg,
		}

		message4, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg4)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message4, receivedAt.Add(netCfg.Beacon.SlotDurationSec()), nil)
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1, Decided: 1}, state1.MessageCounts)
	})

	// Send a pubsub message with no data should cause an error
	t.Run("pubsub message has no data", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		pmsg := &pubsub.Message{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err := validator.validateP2PMessage(pmsg, receivedAt)

		require.ErrorIs(t, err, ErrPubSubMessageHasNoData)
	})

	// Send a pubsub message where there is too much data should cause an error
	t.Run("pubsub data too big", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, 10_000_000+commons.MessageOffset),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(pmsg, receivedAt)

		e := ErrPubSubDataTooBig
		e.got = 10_000_000
		require.ErrorIs(t, err, e)
	})

	// Send a malformed pubsub message (empty message) should return an error
	t.Run("empty pubsub message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, 1+commons.MessageOffset),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(pmsg, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
	})

	// Send a message with incorrect data (unable to decode incorrect message type)
	t.Run("bad data format", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    bytes.Repeat([]byte{1}, 500),
		}

		encodedMsg, err := ssvMsg.Encode()
		require.NoError(t, err)

		encodedSignedMsg := spectypes.EncodeSignedSSVMessage(encodedMsg, 1, make([]byte, 256))

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  encodedSignedMsg,
				Topic: &topic,
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(pmsg, receivedAt)

		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	// Send exact allowed data size amount but with invalid data (fails to decode)
	t.Run("data size borderline / malformed message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    bytes.Repeat([]byte{0x1}, maxMessageSize),
		}

		encodedMsg, err := ssvMsg.Encode()
		require.NoError(t, err)

		encodedSignedMsg := spectypes.EncodeSignedSSVMessage(encodedMsg, 1, make([]byte, 256))

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  encodedSignedMsg,
				Topic: &topic,
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(pmsg, receivedAt)
		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	// Send a message with no data should return an error
	t.Run("no data", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		message := &genesisqueue.GenesisSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    []byte{},
			},
		}

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		require.ErrorIs(t, err, ErrEmptyData)

		message.SSVMessage.Data = nil

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		require.ErrorIs(t, err, ErrEmptyData)
	})

	// Send a message where there is too much data should cause an error
	t.Run("data too big", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		const tooBigMsgSize = maxMessageSize * 2

		message := &genesisqueue.GenesisSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    bytes.Repeat([]byte{0x1}, tooBigMsgSize),
			},
		}

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		expectedErr := ErrSSVDataTooBig
		expectedErr.got = tooBigMsgSize
		expectedErr.want = maxMessageSize
		require.ErrorIs(t, err, expectedErr)
	})

	// Send an invalid SSV message type returns an error
	t.Run("invalid SSV message type", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: math.MaxUint64,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		encodedMsg, err := ssvMsg.Encode()
		require.NoError(t, err)

		encodedSignedMsg := spectypes.EncodeSignedSSVMessage(encodedMsg, 1, make([]byte, 256))

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  encodedSignedMsg,
				Topic: &topic,
			},
		}

		_, _, err = validator.validateP2PMessage(pmsg, time.Now())
		require.ErrorContains(t, err, ErrUnknownSSVMessageType.Error())
	})

	// Empty validator public key returns an error
	t.Run("empty validator public key", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), spectypes.ValidatorPK{}, roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		require.ErrorContains(t, err, ErrDeserializePublicKey.Error())
	})

	// Generate random validator and validate it is unknown to the network
	t.Run("unknown validator", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		sk, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), sk.PublicKey().Marshal(), roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		expectedErr := ErrUnknownValidator
		expectedErr.got = hex.EncodeToString(sk.PublicKey().Marshal())
		require.ErrorIs(t, err, expectedErr)
	})

	// Make sure messages are dropped if on the incorrect network
	t.Run("wrong domain", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		wrongDomain := spectypes.DomainType{math.MaxUint8, math.MaxUint8, math.MaxUint8, math.MaxUint8}
		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(wrongDomain, share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrWrongDomain
		expectedErr.got = hex.EncodeToString(wrongDomain[:])
		domain := netCfg.DomainType()
		expectedErr.want = hex.EncodeToString(domain[:])
		require.ErrorIs(t, err, expectedErr)
	})

	// Send message with a value that refers to a non-existent role
	t.Run("invalid role", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], math.MaxUint64),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrInvalidRole)
	})

	// Perform validator registration or voluntary exit with a consensus type message will give an error
	t.Run("unexpected consensus message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], spectypes.BNRoleValidatorRegistration),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorContains(t, err, ErrUnexpectedConsensusMessage.Error())

		ssvMsg = &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], spectypes.BNRoleVoluntaryExit),
			Data:    encodedValidSignedMessage,
		}

		message, err = genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorContains(t, err, ErrUnexpectedConsensusMessage.Error())
	})

	// Ignore messages related to a validator that is liquidated
	t.Run("liquidated validator", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		liquidatedSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		liquidatedShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				Liquidated: true,
			},
		}
		liquidatedShare.ValidatorPubKey = alanspectypes.ValidatorPK(liquidatedSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, liquidatedShare))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), liquidatedShare.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, time.Now(), nil)
		expectedErr := ErrValidatorLiquidated
		require.ErrorIs(t, err, expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, liquidatedShare.ValidatorPubKey[:]))
	})

	// Ignore messages related to a validator with unknown state
	t.Run("unknown state validator", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		inactiveSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		inactiveShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: eth2apiv1.ValidatorStateUnknown,
				},
				Liquidated: false,
			},
		}
		inactiveShare.ValidatorPubKey = alanspectypes.ValidatorPK(inactiveSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, inactiveShare))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), inactiveShare.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = eth2apiv1.ValidatorStateUnknown.String()
		require.ErrorIs(t, err, expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, inactiveShare.ValidatorPubKey[:]))
	})

	// Ignore messages related to a validator that in pending queued state
	t.Run("pending queued state validator", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		nonUpdatedMetadataSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		slot := netCfg.Beacon.EstimatedCurrentSlot()
		epoch := netCfg.Beacon.EstimatedEpochAtSlot(slot)
		height := specqbft.Height(slot)

		nonUpdatedMetadataShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status:          eth2apiv1.ValidatorStatePendingQueued,
					ActivationEpoch: epoch + 1,
				},
				Liquidated: false,
			},
		}
		nonUpdatedMetadataShare.ValidatorPubKey = alanspectypes.ValidatorPK(nonUpdatedMetadataSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataShare))

		leader := validator.roundRobinProposer(height, specqbft.FirstRound, nonUpdatedMetadataShare)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[leader], leader, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), nonUpdatedMetadataShare.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = eth2apiv1.ValidatorStatePendingQueued.String()
		require.ErrorIs(t, err, expectedErr)

		require.NoError(t, ns.Shares().Delete(nil, nonUpdatedMetadataShare.ValidatorPubKey[:]))
	})

	// Don't ignore messages related to a validator that in pending queued state (in case metadata is not updated),
	// but he is active (activation epoch <= current epoch)
	t.Run("active validator with pending queued state", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		nonUpdatedMetadataSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		slot := netCfg.Beacon.EstimatedCurrentSlot()
		epoch := netCfg.Beacon.EstimatedEpochAtSlot(slot)
		height := specqbft.Height(slot)

		nonUpdatedMetadataShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status:          eth2apiv1.ValidatorStatePendingQueued,
					ActivationEpoch: epoch,
				},
				Liquidated: false,
			},
		}
		nonUpdatedMetadataShare.ValidatorPubKey = alanspectypes.ValidatorPK(nonUpdatedMetadataSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataShare))

		leader := validator.roundRobinProposer(height, specqbft.FirstRound, nonUpdatedMetadataShare)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[leader], leader, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), nonUpdatedMetadataShare.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.NoError(t, err)

		require.NoError(t, ns.Shares().Delete(nil, nonUpdatedMetadataShare.ValidatorPubKey[:]))
	})

	// Unable to process a message with a validator that is not on the network
	t.Run("no share metadata", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		noMetadataSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		noMetadataShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: nil,
				Liquidated:     false,
			},
		}
		noMetadataShare.ValidatorPubKey = alanspectypes.ValidatorPK(noMetadataSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, noMetadataShare))

		validSignedMessage := spectestingutils.TestingProposalMessage(ks.Shares[1], 1)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), noMetadataShare.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrNoShareMetadata)

		require.NoError(t, ns.Shares().Delete(nil, noMetadataShare.ValidatorPubKey[:]))
	})

	// Receive error if more than 2 attestation duties in an epoch
	t.Run("too many duties", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)), nil)
		require.NoError(t, err)

		validSignedMessage = spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height+4)
		encodedValidSignedMessage, err = validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, netCfg.Beacon.GetSlotStartTime(slot+4).Add(validator.waitAfterSlotStart(roleAttester)), nil)
		require.NoError(t, err)

		validSignedMessage = spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height+8)
		encodedValidSignedMessage, err = validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg3 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message3, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg3)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message3, netCfg.Beacon.GetSlotStartTime(slot+8).Add(validator.waitAfterSlotStart(roleAttester)), nil)
		require.ErrorContains(t, err, ErrTooManyDutiesPerEpoch.Error())
	})

	// Throw error if getting a message for proposal and see there is no message from beacon
	t.Run("no proposal duties", func(t *testing.T) {
		const epoch = 1
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
		height := specqbft.Height(slot)

		dutyStore := dutystore.New()
		dutyStore.Proposer.Add(epoch, slot, validatorIndex+1, &eth2apiv1.ProposerDuty{}, true)
		validator := New(netCfg, WithNodeStorage(ns), WithDutyStore(dutyStore)).(*messageValidator)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], spectypes.BNRoleProposer),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(spectypes.BNRoleProposer)), nil)
		require.ErrorContains(t, err, ErrNoDuty.Error())

		dutyStore = dutystore.New()
		dutyStore.Proposer.Add(epoch, slot, validatorIndex, &eth2apiv1.ProposerDuty{}, true)
		validator = New(netCfg, WithNodeStorage(ns), WithDutyStore(dutyStore)).(*messageValidator)
		_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(spectypes.BNRoleProposer)), nil)
		require.NoError(t, err)
	})

	// Get error when receiving a message with over 13 partial signatures
	t.Run("partial message too big", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		for i := 0; i < 13; i++ {
			msg.Message.Messages = append(msg.Message.Messages, msg.Message.Messages[0])
		}

		_, err := msg.Encode()
		require.ErrorContains(t, err, "max expected 13 and 14 found")
	})

	// Get error when receiving message from operator who is not affiliated with the validator
	t.Run("signer ID not in committee", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 5, specqbft.Height(slot))

		encoded, err := msg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrSignerNotInCommittee)
	})

	// Get error when receiving message from operator who is non-existent (operator id 0)
	t.Run("partial zero signer ID", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 0, specqbft.Height(slot))

		encoded, err := msg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrZeroSigner)
	})

	// Get error when receiving partial signature message from operator who is the incorrect signer
	t.Run("partial inconsistent signer ID", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages[0].Signer = 2

		encoded, err := msg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrUnexpectedSigner
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive error when receiving a duplicated partial signature message
	t.Run("partial duplicated message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages = append(msg.Message.Messages, msg.Message.Messages[0])

		encoded, err := msg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrDuplicatedPartialSignatureMessage)
	})

	// Receive error when "partialSignatureMessages" does not contain any "partialSignatureMessage"
	t.Run("no partial signature messages", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Message.Messages = []*spectypes.PartialSignatureMessage{}

		encoded, err := msg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrNoPartialMessages)
	})

	// Receive error when the partial signature message is not enough bytes
	t.Run("partial wrong signature size", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		msg.Signature = []byte{1}

		encoded, err := msg.Encode()
		require.ErrorContains(t, err, "bytes array does not have the correct length")

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		encodedMsg, err := ssvMsg.Encode()
		require.NoError(t, err)

		topic := commons.GetTopicFullName(commons.ValidatorTopicID(share.ValidatorPubKey[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  encodedMsg,
				Topic: &topic,
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateP2PMessage(pmsg, receivedAt)
		require.ErrorContains(t, err, ErrMalformedMessage.Error())
	})

	// Run partial message type validation tests
	t.Run("partial message type validation", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(162304)

		// Check happy flow of a duty for each role
		t.Run("valid", func(t *testing.T) {
			tests := map[spectypes.BeaconRole][]spectypes.PartialSigMsgType{
				spectypes.BNRoleAttester:                  {spectypes.PostConsensusPartialSig},
				spectypes.BNRoleAggregator:                {spectypes.PostConsensusPartialSig, spectypes.SelectionProofPartialSig},
				spectypes.BNRoleProposer:                  {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig},
				spectypes.BNRoleSyncCommittee:             {spectypes.PostConsensusPartialSig},
				spectypes.BNRoleSyncCommitteeContribution: {spectypes.PostConsensusPartialSig, spectypes.ContributionProofs},
				spectypes.BNRoleValidatorRegistration:     {spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleVoluntaryExit:             {spectypes.VoluntaryExitPartialSig},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
					msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], role)

					innerSig, r, err := spectestingutils.NewTestingKeyManager().SignBeaconObject(spectypes.SSZUint64(spectestingutils.TestingDutyEpoch), phase0.Domain{}, ks.Shares[1].GetPublicKey().Serialize(), phase0.DomainType{})
					require.NoError(t, err)

					innerMsg := spectypes.PartialSignatureMessages{
						Type: msgType,
						Messages: []*spectypes.PartialSignatureMessage{
							{
								PartialSignature: innerSig,
								SigningRoot:      r,
								Signer:           1,
							},
						},
						Slot: slot,
					}

					sig, err := spectestingutils.NewTestingKeyManager().SignRoot(innerMsg, spectypes.PartialSignatureType, ks.Shares[1].GetPublicKey().Serialize())
					require.NoError(t, err)

					msg := &spectypes.SignedPartialSignatureMessage{
						Message:   innerMsg,
						Signature: sig,
						Signer:    1,
					}

					encoded, err := msg.Encode()
					require.NoError(t, err)

					ssvMsg := &spectypes.SSVMessage{
						MsgType: spectypes.SSVPartialSignatureMsgType,
						MsgID:   msgID,
						Data:    encoded,
					}

					message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
					require.NoError(t, err)

					receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
					_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
					require.NoError(t, err)
				}
			}
		})

		// Get error when receiving a message with an incorrect message type
		t.Run("invalid message type", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
			msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)

			msg := &spectypes.SignedPartialSignatureMessage{
				Message: spectypes.PartialSignatureMessages{
					Type: math.MaxUint64,
				},
				Signature: make([]byte, 96),
				Signer:    1,
			}

			encoded, err := msg.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   msgID,
				Data:    encoded,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.ErrorContains(t, err, ErrUnknownPartialMessageType.Error())
		})

		// Get error when sending an unexpected message type for the required duty (sending randao for attestor duty)
		t.Run("mismatch", func(t *testing.T) {
			tests := map[spectypes.BeaconRole][]spectypes.PartialSigMsgType{
				spectypes.BNRoleAttester:                  {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleAggregator:                {spectypes.RandaoPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleProposer:                  {spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleSyncCommittee:             {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleSyncCommitteeContribution: {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ValidatorRegistrationPartialSig},
				spectypes.BNRoleValidatorRegistration:     {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
				spectypes.BNRoleVoluntaryExit:             {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)
					msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], role)

					msg := &spectypes.SignedPartialSignatureMessage{
						Message: spectypes.PartialSignatureMessages{
							Type: msgType,
						},
						Signature: make([]byte, 96),
						Signer:    1,
					}

					encoded, err := msg.Encode()
					require.NoError(t, err)

					ssvMsg := &spectypes.SSVMessage{
						MsgType: spectypes.SSVPartialSignatureMsgType,
						MsgID:   msgID,
						Data:    encoded,
					}

					message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
					require.NoError(t, err)

					receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
					_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
					require.ErrorContains(t, err, ErrPartialSignatureTypeRoleMismatch.Error())
				}
			}
		})
	})

	// Get error when receiving QBFT message with an invalid type
	t.Run("invalid QBFT message type", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

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

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrUnknownQBFTMessageType
		require.ErrorIs(t, err, expectedErr)
	})

	// Get error when receiving an incorrect signature size (too small)
	t.Run("wrong signature size", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Signature = []byte{0x1}

		_, err := validSignedMessage.Encode()
		require.Error(t, err)
	})

	// Initialize signature tests
	t.Run("zero signature", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		// Get error when receiving a consensus message with a zero signature
		t.Run("consensus message", func(t *testing.T) {
			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
			zeroSignature := [signatureSize]byte{}
			validSignedMessage.Signature = zeroSignature[:]

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.ErrorIs(t, err, ErrZeroSignature)
		})

		// Get error when receiving a consensus message with a zero signature
		t.Run("partial signature message", func(t *testing.T) {
			partialSigMessage := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, height)
			zeroSignature := [signatureSize]byte{}
			partialSigMessage.Signature = zeroSignature[:]

			encoded, err := partialSigMessage.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.ErrorIs(t, err, ErrZeroSignature)
		})
	})

	// Get error when receiving a message with an empty list of signers
	t.Run("no signers", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Signers = []spectypes.OperatorID{}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrNoSigners)
	})

	// Initialize no signer tests
	t.Run("zero signer", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		inactiveSK, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		zeroSignerKS := alanspectestingutils.Testing7SharesSet()
		zeroSignerShare := &ssvtypes.SSVShare{
			Share: *alanspectestingutils.TestingShare(zeroSignerKS, alanspectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				Liquidated: false,
			},
		}
		zeroSignerShare.Committee[0].Signer = 0
		zeroSignerShare.ValidatorPubKey = alanspectypes.ValidatorPK(inactiveSK.PublicKey().Marshal())

		require.NoError(t, ns.Shares().Save(nil, zeroSignerShare))

		// Get error when receiving a consensus message with a zero signer
		t.Run("consensus message", func(t *testing.T) {
			validSignedMessage := spectestingutils.TestingProposalMessage(zeroSignerKS.Shares[1], 1)
			validSignedMessage.Signers = []spectypes.OperatorID{0}

			encodedValidSignedMessage, err := validSignedMessage.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), zeroSignerShare.ValidatorPubKey[:], roleAttester),
				Data:    encodedValidSignedMessage,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.ErrorIs(t, err, ErrZeroSigner)
		})

		// Get error when receiving a partial message with a zero signer
		t.Run("partial signature message", func(t *testing.T) {
			partialSignatureMessage := spectestingutils.PostConsensusAttestationMsg(zeroSignerKS.Shares[1], 1, specqbft.Height(slot))
			partialSignatureMessage.Signer = 0

			encoded, err := partialSignatureMessage.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), zeroSignerShare.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.ErrorIs(t, err, ErrZeroSigner)
		})

		require.NoError(t, ns.Shares().Delete(nil, zeroSignerShare.ValidatorPubKey[:]))
	})

	// Get error when receiving a message with duplicated signers
	t.Run("non unique signer", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{1, 2, 2}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrDuplicatedSigner)
	})

	// Get error when receiving a message with non-sorted signers
	t.Run("signers not sorted", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{3, 2, 1}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrSignersNotSorted)
	})

	// Get error when receiving message from non quorum size amount of signers
	t.Run("wrong signers length", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		validSignedMessage.Signers = []spectypes.OperatorID{1, 2}

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		expectedErr := ErrWrongSignersLength
		expectedErr.got = 2
		expectedErr.want = "between 3 and 4"
		require.ErrorIs(t, err, expectedErr)
	})

	// Get error when receiving a non decided message with multiple signers
	t.Run("non decided with multiple signers", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		validSignedMessage := spectestingutils.TestingMultiSignerProposalMessage(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})

		encoded, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		expectedErr := ErrNonDecidedWithMultipleSigners
		expectedErr.got = 3
		require.ErrorIs(t, err, expectedErr)
	})

	// Send late message for all roles and receive late message error
	t.Run("late message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		tests := map[spectypes.BeaconRole]time.Time{
			spectypes.BNRoleAttester:                  netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(spectypes.BNRoleAttester)),
			spectypes.BNRoleAggregator:                netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(spectypes.BNRoleAggregator)),
			spectypes.BNRoleProposer:                  netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleProposer)),
			spectypes.BNRoleSyncCommittee:             netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleSyncCommittee)),
			spectypes.BNRoleSyncCommitteeContribution: netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleSyncCommitteeContribution)),
		}

		for role, receivedAt := range tests {
			role, receivedAt := role, receivedAt
			t.Run(role.String(), func(t *testing.T) {
				msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], role)

				ssvMsg := &spectypes.SSVMessage{
					MsgType: spectypes.SSVConsensusMsgType,
					MsgID:   msgID,
					Data:    encodedValidSignedMessage,
				}

				message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
				require.NoError(t, err)

				_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
				require.ErrorContains(t, err, ErrLateMessage.Error())
			})
		}
	})

	// Send early message for all roles before the duty start and receive early message error
	t.Run("early message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot - 1)
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrEarlyMessage)
	})

	// Send message from non-leader acting as a leader should receive an error
	t.Run("not a leader", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[2], 2, height)
		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrSignerNotLeader
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	// Send wrong size of data (8 bytes) for a prepare justification message should receive an error
	t.Run("malformed prepare justification", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Message.PrepareJustification = [][]byte{{1}}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		require.ErrorContains(t, err, ErrMalformedPrepareJustifications.Error())
	})

	// Send prepare justification message without a proposal message should receive an error
	t.Run("non-proposal with prepare justification", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

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

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		expectedErr := ErrUnexpectedPrepareJustifications
		expectedErr.got = specqbft.PrepareMsgType
		require.ErrorIs(t, err, expectedErr)
	})

	// Send round change justification message without a proposal message should receive an error
	t.Run("non-proposal with round change justification", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

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

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		expectedErr := ErrUnexpectedRoundChangeJustifications
		expectedErr.got = specqbft.PrepareMsgType
		require.ErrorIs(t, err, expectedErr)
	})

	// Send round change justification message with a malformed message (1 byte) should receive an error
	t.Run("malformed round change justification", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.Message.RoundChangeJustification = [][]byte{{1}}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		require.ErrorContains(t, err, ErrMalformedRoundChangeJustifications.Error())
	})

	// Send message root hash that doesnt match the expected root hash should receive an error
	t.Run("wrong root hash", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
		validSignedMessage.FullData = []byte{1}

		encodedValidSignedMessage, err := validSignedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedValidSignedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)

		expectedErr := ErrInvalidHash
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive proposal from same operator twice with different messages (same round) should receive an error
	t.Run("double proposal with different data", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		ssvMsg1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned1,
		}

		message1, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg1)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt, nil)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
		signed2.FullData = []byte{1}
		signed2.Message.Root, err = specqbft.HashDataRoot(signed2.FullData)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned2,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		expectedErr := ErrDifferentProposalData
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive prepare from same operator twice with different messages (same round) should receive an error
	t.Run("double prepare", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		ssvMsg1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned1,
		}

		message1, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg1)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt, nil)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
		require.NoError(t, err)

		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned2,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "prepare, having pre-consensus: 0, proposal: 0, prepare: 1, commit: 0, decided: 0, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive commit from same operator twice with different messages (same round) should receive an error
	t.Run("double commit", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		ssvMsg1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned1,
		}

		message1, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg1)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt, nil)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned2,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 1, decided: 0, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive round change from same operator twice with different messages (same round) should receive an error
	t.Run("double round change", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signed1 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
		encodedSigned1, err := signed1.Encode()
		require.NoError(t, err)

		ssvMsg1 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned1,
		}

		message1, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg1)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message1, receivedAt, nil)
		require.NoError(t, err)

		signed2 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
		encodedSigned2, err := signed2.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
			Data:    encodedSigned2,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 0, round change: 1, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive too many decided messages should receive an error
	t.Run("too many decided", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)

		signed := spectestingutils.TestingCommitMultiSignerMessageWithRound(
			[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, 1)
		encodedSigned, err := signed.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedSigned,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))

		for i := 0; i < maxDecidedCount(len(share.Committee)); i++ {
			_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
			require.NoError(t, err)
		}

		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		expectedErr := ErrTooManySameTypeMessagesPerRound
		expectedErr.got = "decided, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 8, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive message from a round that is too high for that epoch should receive an error
	t.Run("round too high", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		tests := map[spectypes.BeaconRole]specqbft.Round{
			spectypes.BNRoleAttester:                  13,
			spectypes.BNRoleAggregator:                13,
			spectypes.BNRoleProposer:                  7,
			spectypes.BNRoleSyncCommittee:             7,
			spectypes.BNRoleSyncCommitteeContribution: 7,
		}

		for role, round := range tests {
			role, round := role, round
			t.Run(role.String(), func(t *testing.T) {
				msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], role)

				signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, round)
				encodedMessage, err := signedMessage.Encode()
				require.NoError(t, err)

				ssvMsg := &spectypes.SSVMessage{
					MsgType: spectypes.SSVConsensusMsgType,
					MsgID:   msgID,
					Data:    encodedMessage,
				}

				message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
				require.NoError(t, err)

				receivedAt := netCfg.Beacon.GetSlotStartTime(0).Add(validator.waitAfterSlotStart(role))
				_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
				require.ErrorContains(t, err, ErrRoundTooHigh.Error())
			})
		}
	})

	// Receive message from a round that is incorrect for current epoch should receive an error
	t.Run("round already advanced", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 2)
		encodedMessage, err := signedMessage.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMessage,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.NoError(t, err)

		signedMessage = spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 1)
		encodedMessage, err = signedMessage.Encode()
		require.NoError(t, err)

		ssvMsg2 := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedMessage,
		}

		message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
		require.NoError(t, err)

		_, _, err = validator.validateSSVMessage(message2, receivedAt, nil)
		require.ErrorContains(t, err, ErrRoundAlreadyAdvanced.Error())
	})

	// Initialize tests for testing when sending a message with a slot before the current one
	t.Run("slot already advanced", func(t *testing.T) {
		msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		// Send a consensus message with a slot before the current one should cause an error
		t.Run("consensus message", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			signedMessage := spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height+1)
			encodedMessage, err := signedMessage.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			_, _, err = validator.validateSSVMessage(message, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(roleAttester)), nil)
			require.NoError(t, err)

			signedMessage = spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height)
			encodedMessage, err = signedMessage.Encode()
			require.NoError(t, err)

			ssvMsg2 := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			message2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
			require.NoError(t, err)

			_, _, err = validator.validateSSVMessage(message2, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)), nil)
			require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
		})

		// Send a partial signature message with a slot before the current one should cause an error
		t.Run("partial signature message", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			message := spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height+1)
			message.Message.Slot = phase0.Slot(height) + 1
			sig, err := spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
			require.NoError(t, err)
			message.Signature = sig

			encodedMessage, err := message.Encode()
			require.NoError(t, err)

			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			decodedMsg, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
			require.NoError(t, err)

			_, _, err = validator.validateSSVMessage(decodedMsg, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(roleAttester)), nil)
			require.NoError(t, err)

			message = spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height)
			message.Message.Slot = phase0.Slot(height)
			sig, err = spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
			require.NoError(t, err)
			message.Signature = sig

			encodedMessage, err = message.Encode()
			require.NoError(t, err)

			ssvMsg2 := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   msgID,
				Data:    encodedMessage,
			}

			decodedMsg2, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg2)
			require.NoError(t, err)

			_, _, err = validator.validateSSVMessage(decodedMsg2, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester)), nil)
			require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
		})
	})

	// Receive an event message from an operator that is not myself should receive an error
	t.Run("event message", func(t *testing.T) {
		validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

		msgID := spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester)
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		eventMsg := &ssvtypes.EventMsg{}
		encoded, err := eventMsg.Encode()
		require.NoError(t, err)

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.MsgType(ssvmessage.SSVEventMsgType),
			MsgID:   msgID,
			Data:    encoded,
		}

		message, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		require.NoError(t, err)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
		_, _, err = validator.validateSSVMessage(message, receivedAt, nil)
		require.ErrorIs(t, err, ErrEventMessage)
	})

	// Get error when receiving an SSV message with an invalid signature.
	t.Run("signature verification", func(t *testing.T) {
		epoch := phase0.Epoch(123456789)

		t.Run("unsigned message", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[4], 4, specqbft.Height(epoch))

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			encodedMsg, err := commons.EncodeGenesisNetworkMsg(message)
			require.NoError(t, err)

			topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
			pMsg := &pubsub.Message{
				Message: &pspb.Message{
					Topic: &topicID[0],
					Data:  encodedMsg,
				},
			}

			slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateP2PMessage(pMsg, receivedAt)
			require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
		})

		t.Run("signed message", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			encodedMsg, err := commons.EncodeGenesisNetworkMsg(message)
			require.NoError(t, err)

			privKey, err := keys.GeneratePrivateKey()
			require.NoError(t, err)

			pubKey, err := privKey.Public().Base64()
			require.NoError(t, err)

			const operatorID = spectypes.OperatorID(1)

			od := &registrystorage.OperatorData{
				ID:           operatorID,
				PublicKey:    pubKey,
				OwnerAddress: common.Address{},
			}

			found, err := ns.SaveOperatorData(nil, od)
			require.NoError(t, err)
			require.False(t, found)

			signature, err := privKey.Sign(encodedMsg)
			require.NoError(t, err)

			signedSSVMsg := &spectypes.SignedSSVMessage{
				Signature:  signature,
				OperatorID: operatorID,
				Data:       encodedMsg,
			}
			encodedMsg, err = signedSSVMsg.Encode()
			require.NoError(t, err)

			topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
			pMsg := &pubsub.Message{
				Message: &pspb.Message{
					Topic: &topicID[0],
					Data:  encodedMsg,
				},
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateP2PMessage(pMsg, receivedAt)
			require.NoError(t, err)

			require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
		})

		t.Run("unexpected operator ID", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			encodedMsg, err := commons.EncodeGenesisNetworkMsg(message)
			require.NoError(t, err)

			privKey, err := keys.GeneratePrivateKey()
			require.NoError(t, err)

			pubKey, err := privKey.Public().Base64()
			require.NoError(t, err)

			const operatorID = spectypes.OperatorID(1)

			od := &registrystorage.OperatorData{
				ID:           operatorID,
				PublicKey:    pubKey,
				OwnerAddress: common.Address{},
			}

			found, err := ns.SaveOperatorData(nil, od)
			require.NoError(t, err)
			require.False(t, found)

			signature, err := privKey.Sign(encodedMsg)
			require.NoError(t, err)

			const unexpectedOperatorID = 2
			signedSSVMsg := &spectypes.SignedSSVMessage{
				Signature:  signature,
				OperatorID: unexpectedOperatorID,
				Data:       encodedMsg,
			}
			encodedMsg, err = signedSSVMsg.Encode()
			require.NoError(t, err)

			topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
			pMsg := &pubsub.Message{
				Message: &pspb.Message{
					Topic: &topicID[0],
					Data:  encodedMsg,
				},
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateP2PMessage(pMsg, receivedAt)
			require.ErrorContains(t, err, ErrOperatorNotFound.Error())

			require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
		})

		t.Run("malformed signature", func(t *testing.T) {
			validator := New(netCfg, WithNodeStorage(ns)).(*messageValidator)

			slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

			validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))

			encoded, err := validSignedMessage.Encode()
			require.NoError(t, err)

			message := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.DomainType(netCfg.DomainType()), share.ValidatorPubKey[:], roleAttester),
				Data:    encoded,
			}

			encodedMsg, err := commons.EncodeGenesisNetworkMsg(message)
			require.NoError(t, err)

			privKey, err := keys.GeneratePrivateKey()
			require.NoError(t, err)

			pubKey, err := privKey.Public().Base64()
			require.NoError(t, err)

			const operatorID = spectypes.OperatorID(1)

			od := &registrystorage.OperatorData{
				ID:           operatorID,
				PublicKey:    pubKey,
				OwnerAddress: common.Address{},
			}

			found, err := ns.SaveOperatorData(nil, od)
			require.NoError(t, err)
			require.False(t, found)

			signature := bytes.Repeat([]byte{1}, 256)
			signedSSVMsg := &spectypes.SignedSSVMessage{
				Signature:  signature,
				OperatorID: operatorID,
				Data:       encodedMsg,
			}
			encodedMsg, err = signedSSVMsg.Encode()
			require.NoError(t, err)

			topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
			pMsg := &pubsub.Message{
				Message: &pspb.Message{
					Topic: &topicID[0],
					Data:  encodedMsg,
				},
			}

			receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(roleAttester))
			_, _, err = validator.validateP2PMessage(pMsg, receivedAt)
			require.ErrorContains(t, err, ErrSignatureVerification.Error())

			require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
		})
	})
}
