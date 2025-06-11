package validation

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils"
)

func Test_ValidateSSVMessage(t *testing.T) {
	ctrl := gomock.NewController(t)

	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	require.NoError(t, err)

	netCfg := networkconfig.TestNetwork

	ks := spectestingutils.Testing4SharesSet()
	shares := generateShares(t, ks, ns, netCfg)

	dutyStore := dutystore.New()
	validatorStore := mocks.NewMockValidatorStore(ctrl)
	operators := mocks.NewMockOperators(ctrl)

	committee := slices.Collect(maps.Keys(ks.Shares))
	slices.Sort(committee)

	committeeID := shares.active.CommitteeID()

	validatorStore.EXPECT().Committee(gomock.Any()).DoAndReturn(func(id spectypes.CommitteeID) (*registrystorage.Committee, bool) {
		if id == committeeID {

			share1 := cloneSSVShare(t, shares.active)
			share2 := cloneSSVShare(t, share1)
			share2.ValidatorIndex = share1.ValidatorIndex + 1
			share3 := cloneSSVShare(t, share2)
			share3.ValidatorIndex = share2.ValidatorIndex + 1

			return &registrystorage.Committee{
				ID:        id,
				Operators: committee,
				Validators: []*ssvtypes.SSVShare{
					share1,
					share2,
					share3,
				},
				Indices: []phase0.ValidatorIndex{
					share1.ValidatorIndex,
					share2.ValidatorIndex,
					share3.ValidatorIndex,
				},
			}, true
		}

		return nil, false
	}).AnyTimes()

	validatorStore.EXPECT().Validator(gomock.Any()).DoAndReturn(func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
		for _, share := range []*ssvtypes.SSVShare{
			shares.active,
			shares.liquidated,
			shares.inactive,
			shares.nonUpdatedMetadata,
			shares.nonUpdatedMetadataNextEpoch,
			shares.noMetadata,
		} {
			if bytes.Equal(share.ValidatorPubKey[:], pubKey) {
				return share, true
			}
		}
		return nil, false
	}).AnyTimes()

	for _, id := range []spectypes.OperatorID{1, 2, 3, 4, 5} {
		operators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{id}).
			Return(true, nil).
			AnyTimes()
	}

	signatureVerifier := signatureverifier.NewMockSignatureVerifier(ctrl)
	signatureVerifier.EXPECT().VerifySignature(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	wrongSignatureVerifier := signatureverifier.NewMockSignatureVerifier(ctrl)
	wrongSignatureVerifier.EXPECT().VerifySignature(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("test")).AnyTimes()

	committeeRole := spectypes.RoleCommittee
	nonCommitteeRole := spectypes.RoleAggregator

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID, committeeRole)
	nonCommitteeIdentifier := spectypes.NewMsgID(netCfg.DomainType, ks.ValidatorPK.Serialize(), nonCommitteeRole)

	// Message validation happy flow, messages are not ignored or rejected and there are no errors
	t.Run("happy flow", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)
	})

	// Make sure messages are incremented and throw an ignore message if more than 1 for a commit
	t.Run("message counts", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		msgID := committeeIdentifier
		state := validator.consensusState(msgID)
		for i := spectypes.OperatorID(1); i <= 4; i++ {
			signerState := state.GetOrCreate(i)
			require.NotNil(t, signerState)
		}

		signedSSVMessage := generateSignedMessage(ks, msgID, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrDuplicatedMessage.Error())

		stateBySlot := state.GetOrCreate(1)
		require.NotNil(t, stateBySlot)

		storedState := stateBySlot.Get(slot)
		require.NotNil(t, storedState)
		require.EqualValues(t, height, storedState.Slot)
		require.EqualValues(t, 1, storedState.Round)
		require.EqualValues(t, MessageCounts{Proposal: 1}, storedState.MessageCounts)
		for i := spectypes.OperatorID(2); i <= 4; i++ {
			require.NotNil(t, state.GetOrCreate(i))
		}

		signedSSVMessage = generateSignedMessage(ks, msgID, slot, func(message *specqbft.Message) {
			message.Round = 2
			message.MsgType = specqbft.PrepareMsgType
		})
		signedSSVMessage.FullData = nil

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		storedState = stateBySlot.Get(slot)
		require.NotNil(t, storedState)
		require.EqualValues(t, height, storedState.Slot)
		require.EqualValues(t, 2, storedState.Round)
		require.EqualValues(t, MessageCounts{Prepare: 1}, storedState.MessageCounts)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrDuplicatedMessage.Error())

		signedSSVMessage = generateSignedMessage(ks, msgID, slot+1, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		signedSSVMessage.FullData = nil
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)

		storedState = stateBySlot.Get(phase0.Slot(height) + 1)
		require.NotNil(t, storedState)
		require.EqualValues(t, 1, storedState.Round)
		require.EqualValues(t, MessageCounts{Commit: 1}, storedState.MessageCounts)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.ErrorContains(t, err, ErrDuplicatedMessage.Error())

		signedSSVMessage = generateMultiSignedMessage(ks, msgID, slot+1)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)
		require.NotNil(t, stateBySlot)
		require.EqualValues(t, 1, storedState.Round)
		require.EqualValues(t, MessageCounts{Commit: 1}, storedState.MessageCounts)
	})

	// Send a pubsub message with no data should cause an error
	t.Run("pubsub message has no data", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		pmsg := &pubsub.Message{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err := validator.handlePubsubMessage(pmsg, receivedAt)

		require.ErrorIs(t, err, ErrPubSubMessageHasNoData)
	})

	// Send a pubsub message where there is too much data should cause an error
	t.Run("pubsub data too big", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.CommitteeTopicID(committeeID)[0])
		msgSize := maxSignedMsgSize*2 + MessageOffset

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, msgSize),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handlePubsubMessage(pmsg, receivedAt)

		e := ErrPubSubDataTooBig
		e.got = msgSize
		require.ErrorIs(t, err, e)
	})

	// Send a malformed pubsub message (empty message) should return an error
	t.Run("empty pubsub message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.CommitteeTopicID(committeeID)[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, 1+MessageOffset),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handlePubsubMessage(pmsg, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
	})

	// Send a message with incorrect data (unable to decode incorrect message type)
	t.Run("bad data format", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, 500)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUndecodableMessageData.Error())
	})

	// Send a message with no data should return an error
	t.Run("no data", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = []byte{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrEmptyData)

		signedSSVMessage.SSVMessage.Data = nil
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrEmptyData)
	})

	// Send a message where there is too much data should cause an error
	t.Run("data too big", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		tooBigMsgSize := maxPayloadDataSize * 2
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, tooBigMsgSize)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		expectedErr := ErrSSVDataTooBig
		expectedErr.got = tooBigMsgSize
		expectedErr.want = maxPayloadDataSize
		require.ErrorIs(t, err, expectedErr)
	})

	// Send exact allowed data size amount but with invalid data (fails to decode)
	t.Run("data size borderline / malformed message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, maxPayloadDataSize)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUndecodableMessageData.Error())
	})

	// Send an invalid SSV message type returns an error
	t.Run("invalid SSV message type", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.MsgType = math.MaxUint64

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		require.ErrorContains(t, err, ErrUnknownSSVMessageType.Error())
	})

	// Generate random validator and validate it is unknown to the network
	t.Run("unknown validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		sk, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		unknown := spectypes.NewMsgID(netCfg.DomainType, sk.PublicKey().Marshal(), nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, unknown, slot)

		_, exists := validatorStore.Validator(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID())
		require.False(t, exists)

		topicID := commons.CommitteeTopicID(shares.active.CommitteeID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		expectedErr := ErrUnknownValidator
		expectedErr.got = hex.EncodeToString(sk.PublicKey().Marshal())
		require.ErrorIs(t, err, expectedErr)
	})

	// Generate random committee ID and validate it is unknown to the network
	t.Run("unknown committee ID", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		unknownCommitteeID := bytes.Repeat([]byte{1}, 48)
		unknownIdentifier := spectypes.NewMsgID(netCfg.DomainType, unknownCommitteeID, committeeRole)
		signedSSVMessage := generateSignedMessage(ks, unknownIdentifier, slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		expectedErr := ErrNonExistentCommitteeID
		expectedErr.got = hex.EncodeToString(unknownCommitteeID[16:])
		require.ErrorIs(t, err, expectedErr)
	})

	// Make sure messages are dropped if on the incorrect network
	t.Run("wrong domain", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		wrongDomain := spectypes.DomainType{math.MaxUint8, math.MaxUint8, math.MaxUint8, math.MaxUint8}
		badIdentifier := spectypes.NewMsgID(wrongDomain, encodedCommitteeID, committeeRole)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrWrongDomain
		expectedErr.got = hex.EncodeToString(wrongDomain[:])
		domain := netCfg.DomainType
		expectedErr.want = hex.EncodeToString(domain[:])
		require.ErrorIs(t, err, expectedErr)
	})

	// Send message with a value that refers to a non-existent role
	t.Run("invalid role", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		badIdentifier := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID, math.MaxInt32)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrInvalidRole)
	})

	// Perform validator registration or voluntary exit with a consensus type message will give an error
	t.Run("unexpected consensus message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		badIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.active.ValidatorPubKey[:], spectypes.RoleValidatorRegistration)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.CommitteeTopicID(committeeID)[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrUnexpectedConsensusMessage
		expectedErr.got = spectypes.RoleValidatorRegistration
		require.ErrorIs(t, err, expectedErr)

		badIdentifier = spectypes.NewMsgID(netCfg.DomainType, shares.active.ValidatorPubKey[:], spectypes.RoleVoluntaryExit)
		signedSSVMessage = generateSignedMessage(ks, badIdentifier, slot)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr.got = spectypes.RoleVoluntaryExit
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator that is liquidated
	t.Run("liquidated validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		liquidatedIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.liquidated.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, liquidatedIdentifier, slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrValidatorLiquidated
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator with unknown state
	t.Run("unknown state validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		inactiveIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.inactive.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, inactiveIdentifier, slot)

		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrNoShareMetadata
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator that in pending queued state
	t.Run("pending queued state validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		nonUpdatedMetadataNextEpochIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.nonUpdatedMetadataNextEpoch.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, nonUpdatedMetadataNextEpochIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = eth2apiv1.ValidatorStatePendingQueued.String()
		require.ErrorIs(t, err, expectedErr)
	})

	// Don't ignore messages related to a validator that in pending queued state (in case metadata is not updated),
	// but it is active (activation epoch <= current epoch)
	t.Run("active validator with pending queued state", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.EstimatedCurrentSlot()

		nonUpdatedMetadataIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.nonUpdatedMetadata.ValidatorPubKey[:], nonCommitteeRole)
		qbftMessage := &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     specqbft.Height(slot),
			Round:      specqbft.FirstRound,
			Identifier: nonUpdatedMetadataIdentifier[:],
			Root:       sha256.Sum256(spectestingutils.TestingQBFTFullData),

			RoundChangeJustification: [][]byte{},
			PrepareJustification:     [][]byte{},
		}

		leader := validator.roundRobinProposer(specqbft.Height(slot), specqbft.FirstRound, []spectypes.OperatorID{1, 2, 3, 4})
		signedSSVMessage := spectestingutils.SignQBFTMsg(ks.OperatorKeys[leader], leader, qbftMessage)
		signedSSVMessage.FullData = spectestingutils.TestingQBFTFullData

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)
	})

	// Unable to process a message with a validator that is not on the network
	t.Run("no share metadata", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		noMetadataIdentifier := spectypes.NewMsgID(netCfg.DomainType, shares.noMetadata.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, noMetadataIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoShareMetadata)
	})

	// Receive error if more than 2 attestation duties in an epoch
	t.Run("too many duties", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		epoch := phase0.Epoch(1)
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		dutyStore.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: slot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
			{Slot: slot + 4, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
			{Slot: slot + 8, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
		})

		role := spectypes.RoleAggregator
		identifier := spectypes.NewMsgID(netCfg.DomainType, ks.ValidatorPK.Serialize(), role)
		signedSSVMessage := generateSignedMessage(ks, identifier, slot)

		// First duty.
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot))
		require.NoError(t, err)

		// Second duty.
		signedSSVMessage = generateSignedMessage(ks, identifier, slot+4)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+4))
		require.NoError(t, err)

		// Second duty (another message).
		signedSSVMessage = generateSignedMessage(ks, identifier, slot+4, func(qbftMessage *specqbft.Message) {
			qbftMessage.MsgType = specqbft.RoundChangeMsgType
		})
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+4))
		require.NoError(t, err)

		// Third duty.
		// TODO: this should fail, see https://github.com/ssvlabs/ssv/pull/1758
		signedSSVMessage = generateSignedMessage(ks, identifier, slot+8)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+8))
		require.NoError(t, err)

		// Third duty (another message).
		signedSSVMessage = generateSignedMessage(ks, identifier, slot+8, func(qbftMessage *specqbft.Message) {
			qbftMessage.MsgType = specqbft.RoundChangeMsgType
		})
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+8))
		require.ErrorContains(t, err, ErrTooManyDutiesPerEpoch.Error())
	})

	// Throw error if getting a message for proposal and see there is no message from beacon
	t.Run("no proposal duties", func(t *testing.T) {
		const epoch = 1
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		ds := dutystore.New()
		ds.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: slot, ValidatorIndex: shares.active.ValidatorIndex + 1, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
		})
		validator := New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		identifier := spectypes.NewMsgID(netCfg.DomainType, ks.ValidatorPK.Serialize(), spectypes.RoleProposer)
		signedSSVMessage := generateSignedMessage(ks, identifier, slot)

		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot))
		require.ErrorContains(t, err, ErrNoDuty.Error())

		ds = dutystore.New()
		ds.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: slot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
		})
		validator = New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot))
		require.NoError(t, err)
	})

	t.Run("accept pre-consensus randao message when epoch duties are not set", func(t *testing.T) {
		currentSlot := &utils.SlotValue{}
		mockNetworkConfig := networkconfig.NetworkConfig{}
		mockNetworkConfig.Beacon = utils.SetupMockBeaconNetwork(t, currentSlot)

		const epoch = 1
		currentSlot.SetSlot(netCfg.Beacon.FirstSlotAtEpoch(epoch))

		ds := dutystore.New()

		validator := New(mockNetworkConfig, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		messages := generateRandaoMsg(ks.Shares[1], 1, epoch, currentSlot.GetSlot())
		encodedMessages, err := messages.Encode()
		require.NoError(t, err)

		dutyExecutorID := shares.active.ValidatorPubKey[:]
		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(mockNetworkConfig.DomainType, dutyExecutorID, spectypes.RoleProposer),
			Data:    encodedMessages,
		}

		signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

		receivedAt := mockNetworkConfig.Beacon.GetSlotStartTime(currentSlot.GetSlot())
		topicID := commons.CommitteeTopicID(committeeID)[0]

		require.False(t, ds.Proposer.IsEpochSet(epoch))

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)
	})

	t.Run("reject pre-consensus randao message when epoch duties are set", func(t *testing.T) {
		currentSlot := &utils.SlotValue{}
		mockNetworkConfig := networkconfig.NetworkConfig{}
		mockNetworkConfig.Beacon = utils.SetupMockBeaconNetwork(t, currentSlot)

		const epoch = 1
		currentSlot.SetSlot(mockNetworkConfig.Beacon.FirstSlotAtEpoch(epoch))

		ds := dutystore.New()
		ds.Proposer.Set(epoch, make([]dutystore.StoreDuty[eth2apiv1.ProposerDuty], 0))

		validator := New(mockNetworkConfig, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		messages := generateRandaoMsg(ks.Shares[1], 1, epoch, currentSlot.GetSlot())
		encodedMessages, err := messages.Encode()
		require.NoError(t, err)

		dutyExecutorID := shares.active.ValidatorPubKey[:]
		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(mockNetworkConfig.DomainType, dutyExecutorID, spectypes.RoleProposer),
			Data:    encodedMessages,
		}

		signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

		receivedAt := mockNetworkConfig.Beacon.GetSlotStartTime(currentSlot.GetSlot())
		topicID := commons.CommitteeTopicID(committeeID)[0]

		require.True(t, ds.Proposer.IsEpochSet(epoch))

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrNoDuty.Error())
	})

	//// Get error when receiving a message with over 13 partial signatures
	t.Run("partial message too big", func(t *testing.T) {
		// slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
		for i := 0; i < 1512; i++ {
			msg.Messages = append(msg.Messages, msg.Messages[0])
		}

		_, err := msg.Encode()
		require.ErrorContains(t, err, "max expected 1512 and 1513 found")
	})

	// Get error when receiving message from operator who is not affiliated with the validator
	t.Run("signer ID not in committee", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.EstimatedCurrentSlot()

		qbftMessage := &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     specqbft.Height(slot),
			Round:      specqbft.FirstRound,
			Identifier: committeeIdentifier[:],
			Root:       sha256.Sum256(spectestingutils.TestingQBFTFullData),

			RoundChangeJustification: [][]byte{},
			PrepareJustification:     [][]byte{},
		}

		signedSSVMessage := spectestingutils.SignQBFTMsg(ks.OperatorKeys[1], 5, qbftMessage)
		signedSSVMessage.FullData = spectestingutils.TestingQBFTFullData

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSignerNotInCommittee.Error())
	})

	// Get error when receiving message from operator who is non-existent (operator id 0)
	t.Run("partial zero signer ID", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.SignPartialSigSSVMessage(ks, spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)))
		msg.OperatorIDs = []spectypes.OperatorID{0}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(msg.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(msg, topicID, receivedAt)
		require.ErrorIs(t, err, ErrZeroSigner)
	})

	// Get error when receiving partial signature message from operator who is the incorrect signer
	t.Run("partial inconsistent signer ID", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		ssvMessage := spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0))
		ssvMessage.MsgID = committeeIdentifier
		partialSigSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)
		partialSigSSVMessage.OperatorIDs = []spectypes.OperatorID{2}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(partialSigSSVMessage, topicID, receivedAt)
		expectedErr := ErrInconsistentSigners
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive error when "partialSignatureMessages" does not contain any "partialSignatureMessage"
	t.Run("no partial signature messages", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
		messages.Messages = nil
		ssvMessage := spectestingutils.SSVMsgAggregator(nil, messages)
		ssvMessage.MsgID = committeeIdentifier
		signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoPartialSignatureMessages)
	})

	// Receive error when the partial RSA signature message is not enough bytes
	t.Run("partial wrong RSA signature size", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		partialSigSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)))
		partialSigSSVMessage.Signatures = [][]byte{{1}}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(partialSigSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(partialSigSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrWrongRSASignatureSize.Error())
	})

	// Run partial message type validation tests
	t.Run("partial message type validation", func(t *testing.T) {
		// Check happy flow of a duty for each committeeRole
		t.Run("valid", func(t *testing.T) {
			tests := map[spectypes.RunnerRole][]spectypes.PartialSigMsgType{
				spectypes.RoleCommittee:                 {spectypes.PostConsensusPartialSig},
				spectypes.RoleAggregator:                {spectypes.PostConsensusPartialSig, spectypes.SelectionProofPartialSig},
				spectypes.RoleProposer:                  {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig},
				spectypes.RoleSyncCommitteeContribution: {spectypes.PostConsensusPartialSig, spectypes.ContributionProofs},
				spectypes.RoleValidatorRegistration:     {spectypes.ValidatorRegistrationPartialSig},
				spectypes.RoleVoluntaryExit:             {spectypes.VoluntaryExitPartialSig},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					role, msgType := role, msgType
					subtestName := fmt.Sprintf("%v/%v", message.RunnerRoleToString(role), message.PartialMsgTypeToString(msgType))
					t.Run(subtestName, func(t *testing.T) {
						ds := dutystore.New()
						ds.Proposer.Set(spectestingutils.TestingDutyEpoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
							{Slot: spectestingutils.TestingDutySlot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
						})
						ds.SyncCommittee.Set(0, []dutystore.StoreSyncCommitteeDuty{
							{ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.SyncCommitteeDuty{}, InCommittee: true},
						})
						ds.VoluntaryExit.AddDuty(spectestingutils.TestingDutySlot, phase0.BLSPubKey(shares.active.ValidatorPubKey))

						validator := New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

						messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
						messages.Type = msgType

						encodedMessages, err := messages.Encode()
						require.NoError(t, err)

						dutyExecutorID := shares.active.ValidatorPubKey[:]
						if validator.committeeRole(role) {
							dutyExecutorID = encodedCommitteeID
						}
						ssvMessage := &spectypes.SSVMessage{
							MsgType: spectypes.SSVPartialSignatureMsgType,
							MsgID:   spectypes.NewMsgID(netCfg.DomainType, dutyExecutorID, role),
							Data:    encodedMessages,
						}

						signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

						receivedAt := netCfg.Beacon.GetSlotStartTime(spectestingutils.TestingDutySlot)

						topicID := commons.CommitteeTopicID(committeeID)[0]

						_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
						require.NoError(t, err)
					})
				}
			}
		})

		// Get error when receiving a message with an incorrect message type
		t.Run("invalid message type", func(t *testing.T) {
			validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

			messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
			messages.Type = math.MaxUint64

			encodedMessages, err := messages.Encode()
			require.NoError(t, err)

			ssvMessage := &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   committeeIdentifier,
				Data:    encodedMessages,
			}

			signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

			receivedAt := netCfg.Beacon.GetSlotStartTime(spectestingutils.TestingDutySlot)
			topicID := commons.CommitteeTopicID(committeeID)[0]
			_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
			require.ErrorContains(t, err, ErrInvalidPartialSignatureType.Error())
		})

		// Get error when sending an unexpected message type for the required duty (sending randao for attestor duty)
		t.Run("mismatch", func(t *testing.T) {
			tests := map[spectypes.RunnerRole][]spectypes.PartialSigMsgType{
				spectypes.RoleCommittee:                 {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.RoleAggregator:                {spectypes.RandaoPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.RoleProposer:                  {spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
				spectypes.RoleSyncCommitteeContribution: {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ValidatorRegistrationPartialSig},
				spectypes.RoleValidatorRegistration:     {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
				spectypes.RoleVoluntaryExit:             {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
			}

			for role, msgTypes := range tests {
				for _, msgType := range msgTypes {
					role, msgType := role, msgType
					subtestName := fmt.Sprintf("%v/%v", message.RunnerRoleToString(role), message.PartialMsgTypeToString(msgType))
					t.Run(subtestName, func(t *testing.T) {
						ds := dutystore.New()
						ds.Proposer.Set(spectestingutils.TestingDutyEpoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
							{Slot: spectestingutils.TestingDutySlot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
						})
						ds.SyncCommittee.Set(0, []dutystore.StoreSyncCommitteeDuty{
							{ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.SyncCommitteeDuty{}, InCommittee: true},
						})

						validator := New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

						messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
						messages.Type = msgType

						encodedMessages, err := messages.Encode()
						require.NoError(t, err)

						dutyExecutorID := shares.active.ValidatorPubKey[:]
						if validator.committeeRole(role) {
							dutyExecutorID = encodedCommitteeID
						}
						ssvMessage := &spectypes.SSVMessage{
							MsgType: spectypes.SSVPartialSignatureMsgType,
							MsgID:   spectypes.NewMsgID(netCfg.DomainType, dutyExecutorID, role),
							Data:    encodedMessages,
						}

						signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

						receivedAt := netCfg.Beacon.GetSlotStartTime(spectestingutils.TestingDutySlot)
						topicID := commons.CommitteeTopicID(committeeID)[0]
						t.Log(signedSSVMessage.SSVMessage.MsgID.GetDomain())
						_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
						require.ErrorContains(t, err, ErrPartialSignatureTypeRoleMismatch.Error())
					})
				}
			}
		})
	})

	// Get error when receiving QBFT message with an invalid type
	t.Run("invalid QBFT message type", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = math.MaxUint64
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrUnknownQBFTMessageType
		require.ErrorIs(t, err, expectedErr)
	})

	// Get error when receiving an incorrect signature size (too small)
	t.Run("wrong signature size", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.Signatures = [][]byte{{0x1}}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrWrongRSASignatureSize.Error())
	})

	// Get error when receiving a message with an empty list of signers
	t.Run("no signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoSigners)
	})

	// Get error when receiving a message with more signers than committee size.
	// It tests ErrMoreSignersThanCommitteeSize from knowledge base.
	t.Run("more signers than committee size", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{1, 2, 3, 4, 5}
		signedSSVMessage.Signatures = [][]byte{
			signedSSVMessage.Signatures[0],
			signedSSVMessage.Signatures[0],
			signedSSVMessage.Signatures[0],
			signedSSVMessage.Signatures[0],
			signedSSVMessage.Signatures[0],
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSignerNotInCommittee.Error())
	})

	// Get error when receiving a consensus message with zero signer
	t.Run("consensus zero signer", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{0}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrZeroSigner)
	})

	// Get error when receiving a message with duplicated signers
	t.Run("non unique signer", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{1, 2, 2}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrDuplicatedSigner)
	})

	// Get error when receiving a message with an operator that does not exist and has not been removed
	t.Run("operator exists and not removed", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		err := validator.validateSignerIsKnown(1)
		require.NoError(t, err)
	})

	// Get error when receiving a message with an operator that does not exist but has been removed
	t.Run("operator exists but removed", func(t *testing.T) {

		// Configure mock to return false for operator 999, simulating a removed operator
		operators.EXPECT().OperatorsExist(gomock.Any(), []spectypes.OperatorID{999}).Return(false, nil)

		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)
		err := validator.validateSignerIsKnown(999)
		expectedErr := ErrUnknownOperator
		expectedErr.got = spectypes.OperatorID(999)

		require.ErrorIs(t, err, expectedErr)
	})

	// Get error when receiving a message with an operator and there is an error during operator validation
	t.Run("error during operator validation", func(t *testing.T) {
		// Configure mock to return an error when checking if operator 6 exists (1-5 are already in the store)
		// This simulates a storage or network error during validation
		operators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{6}).
			Return(false, fmt.Errorf("validation error"))

		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)
		err := validator.validateSignerIsKnown(6)
		expectedErr := ErrOperatorValidation
		expectedErr.got = spectypes.OperatorID(6)

		require.ErrorIs(t, err, expectedErr)
	})

	// Test that validateSignedSSVMessage returns the error from validateSignerIsKnown
	// when the error is not ErrUnknownOperator
	t.Run("signer exists error propagation", func(t *testing.T) {
		localCtrl := gomock.NewController(t)
		localMockOperators := mocks.NewMockOperators(localCtrl)

		localMockOperators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{1}).
			Return(true, nil).
			AnyTimes()
		localMockOperators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{2}).
			Return(true, nil).
			AnyTimes()

		// For operator 3, return an error other than ErrUnknownOperator
		// This simulates a database error or other validation failure
		customErr := fmt.Errorf("custom validation error")

		localMockOperators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{3}).
			Return(false, customErr).
			AnyTimes()

		localValidator := New(netCfg, validatorStore, localMockOperators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		testMsg := &spectypes.SignedSSVMessage{
			OperatorIDs: []spectypes.OperatorID{1, 2, 3},
			Signatures: [][]byte{
				bytes.Repeat([]byte{1}, rsaSignatureSize),
				bytes.Repeat([]byte{2}, rsaSignatureSize),
				bytes.Repeat([]byte{3}, rsaSignatureSize),
			},
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   committeeIdentifier,
				Data:    []byte{1, 2, 3},
			},
		}

		// Validate the message - should return an error since operator 3 returns a non-ErrUnknownOperator error
		err := localValidator.validateSignedSSVMessage(testMsg)

		require.Error(t, err)

		// Verify that the error is an ErrOperatorValidation error and has operator ID 3
		var valErr Error
		require.True(t, errors.As(err, &valErr))
		require.Equal(t, valErr.got, spectypes.OperatorID(3))
		require.Equal(t, valErr.text, ErrOperatorValidation.text)
	})

	// Get error when receiving a message with non-sorted signers
	t.Run("signers not sorted", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{3, 2, 1}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrSignersNotSorted)
	})

	// Get error when receiving message with different amount of signers and signatures
	t.Run("wrong signers/signatures length", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{1, 2, 3, 4}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrSignersAndSignaturesWithDifferentLength.Error())
	})

	// Get error when receiving message from less than quorum size amount of signers
	t.Run("decided too few signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{1, 2}
		signedSSVMessage.Signatures = signedSSVMessage.Signatures[:2]

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrDecidedNotEnoughSigners.Error())
	})

	// Get error when receiving a non decided message with multiple signers
	t.Run("non decided with multiple signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.ProposalMsgType
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		expectedErr := ErrNonDecidedWithMultipleSigners
		expectedErr.got = 3
		require.ErrorIs(t, err, expectedErr)
	})

	// Send late message for all roles and receive late message error
	t.Run("late message", func(t *testing.T) {
		const epoch = 1
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		ds := dutystore.New()
		ds.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: slot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
		})
		ds.SyncCommittee.Set(0, []dutystore.StoreSyncCommitteeDuty{
			{ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.SyncCommitteeDuty{}, InCommittee: true},
		})

		validator := New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		tests := map[spectypes.RunnerRole]time.Time{
			spectypes.RoleCommittee:                 netCfg.Beacon.GetSlotStartTime(slot + 35),
			spectypes.RoleAggregator:                netCfg.Beacon.GetSlotStartTime(slot + 35),
			spectypes.RoleProposer:                  netCfg.Beacon.GetSlotStartTime(slot + 4),
			spectypes.RoleSyncCommitteeContribution: netCfg.Beacon.GetSlotStartTime(slot + 4),
		}

		for role, receivedAt := range tests {
			role, receivedAt := role, receivedAt
			t.Run(message.RunnerRoleToString(role), func(t *testing.T) {
				dutyExecutorID := shares.active.ValidatorPubKey[:]
				if validator.committeeRole(role) {
					dutyExecutorID = encodedCommitteeID
				}

				msgID := spectypes.NewMsgID(netCfg.DomainType, dutyExecutorID, role)
				signedSSVMessage := generateSignedMessage(ks, msgID, slot)

				topicID := commons.CommitteeTopicID(committeeID)[0]
				_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
				require.ErrorContains(t, err, ErrLateSlotMessage.Error())
			})
		}
	})

	// Send early message for all roles before the duty start and receive early message error
	t.Run("early message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot - 1)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrEarlySlotMessage.Error())
	})

	// Send message from non-leader acting as a leader should receive an error
	t.Run("not a leader", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{2}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSignerNotLeader.Error())
	})

	// Send wrong size of data (8 bytes) for a prepare justification message should receive an error
	t.Run("malformed prepare justification", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.PrepareJustification = [][]byte{{1}}
		})
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{2}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPrepareJustifications.Error())
	})

	// Send prepare justification message without a proposal message should receive an error
	t.Run("non-proposal with prepare justification", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.PrepareJustification = spectestingutils.MarshalJustifications([]*spectypes.SignedSSVMessage{
				generateSignedMessage(ks, committeeIdentifier, slot, func(justMsg *specqbft.Message) {
					justMsg.MsgType = specqbft.RoundChangeMsgType
				}),
			})
			message.MsgType = specqbft.PrepareMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUnexpectedPrepareJustifications.Error())
	})

	// Send round change justification message without a proposal message should receive an error
	t.Run("non-proposal with round change justification", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.RoundChangeJustification = spectestingutils.MarshalJustifications([]*spectypes.SignedSSVMessage{
				generateSignedMessage(ks, committeeIdentifier, slot, func(justMsg *specqbft.Message) {
					justMsg.MsgType = specqbft.PrepareMsgType
				}),
			})
			message.MsgType = specqbft.PrepareMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUnexpectedRoundChangeJustifications.Error())
	})

	// Send round change justification message with a malformed message (1 byte) should receive an error
	t.Run("malformed round change justification", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.RoundChangeJustification = [][]byte{{1}}
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrMalformedRoundChangeJustifications.Error())
	})

	// Send message root hash that doesn't match the expected root hash should receive an error
	t.Run("wrong root hash", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.FullData = []byte{1}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		expectedErr := ErrInvalidHash
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive proposal from same operator twice with different messages (same round) should receive an error
	t.Run("double proposal with different data", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		anotherFullData := []byte{1}
		signedSSVMessage = generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.Root, err = specqbft.HashDataRoot(anotherFullData)
			require.NoError(t, err)
		})
		signedSSVMessage.FullData = anotherFullData

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrDifferentProposalData
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive prepare from same operator twice with different messages (same round) should receive an error
	t.Run("double prepare", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		identifier := spectypes.NewMsgID(netCfg.DomainType, ks.ValidatorPK.Serialize(), spectypes.RoleProposer)
		signedSSVMessage := generateSignedMessage(ks, identifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.PrepareMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrDuplicatedMessage
		expectedErr.got = "prepare, having pre-consensus: 0, proposal: 0, prepare: 1, commit: 0, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive commit from same operator twice with different messages (same round) should receive an error
	t.Run("double commit", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrDuplicatedMessage
		expectedErr.got = "commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 1, round change: 0, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive round change from same operator twice with different messages (same round) should receive an error
	t.Run("double round change", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.RoundChangeMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrDuplicatedMessage
		expectedErr.got = "round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, round change: 1, post-consensus: 0"
		require.ErrorIs(t, err, expectedErr)
	})

	// Decided with same signers should receive an error
	t.Run("decided with same signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateMultiSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		signedSSVMessage.FullData = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrDecidedWithSameSigners)
	})

	// Send message with a slot lower than in the previous message
	t.Run("slot already advanced", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, nonCommitteeIdentifier, slot, func(message *specqbft.Message) {
			message.Height = 8
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		signedSSVMessage = generateSignedMessage(ks, nonCommitteeIdentifier, slot, func(message *specqbft.Message) {
			message.Height = 4
		})

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
	})

	// Send message with a round lower than in the previous message
	t.Run("round already advanced", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.Round = 5
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(5 * roundtimer.QuickTimeout)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		signedSSVMessage = generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.Round = 1
		})

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrRoundAlreadyAdvanced.Error())
	})

	// Receive message from a round that is too high for that epoch should receive an error
	t.Run("round too high", func(t *testing.T) {
		const epoch = 1
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		ds := dutystore.New()
		ds.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: slot, ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
		})
		ds.SyncCommittee.Set(0, []dutystore.StoreSyncCommitteeDuty{
			{ValidatorIndex: shares.active.ValidatorIndex, Duty: &eth2apiv1.SyncCommitteeDuty{}, InCommittee: true},
		})

		validator := New(netCfg, validatorStore, operators, ds, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		tests := map[spectypes.RunnerRole]specqbft.Round{
			spectypes.RoleCommittee:                 13,
			spectypes.RoleAggregator:                13,
			spectypes.RoleProposer:                  7,
			spectypes.RoleSyncCommitteeContribution: 7,
		}

		for role, round := range tests {
			role, round := role, round
			t.Run(message.RunnerRoleToString(role), func(t *testing.T) {
				dutyExecutorID := shares.active.ValidatorPubKey[:]
				if validator.committeeRole(role) {
					dutyExecutorID = encodedCommitteeID
				}

				msgID := spectypes.NewMsgID(netCfg.DomainType, dutyExecutorID, role)
				signedSSVMessage := generateSignedMessage(ks, msgID, slot, func(message *specqbft.Message) {
					message.MsgType = specqbft.PrepareMsgType
					message.Round = round
				})
				signedSSVMessage.FullData = nil

				topicID := commons.CommitteeTopicID(committeeID)[0]

				sinceSlotStart := time.Duration(0)
				for {
					currentRound, err := validator.currentEstimatedRound(sinceSlotStart)
					require.NoError(t, err)
					if currentRound == round {
						break
					}
					sinceSlotStart += roundtimer.QuickTimeout
				}

				receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(sinceSlotStart)
				_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
				require.ErrorContains(t, err, ErrRoundTooHigh.Error())
			})
		}
	})

	// Receive an event message from an operator that is not myself should receive an error
	t.Run("event message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.MsgType = message.SSVEventMsgType

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrEventMessage)
	})

	// Receive a unknown message type from an operator that is not myself should receive an error
	t.Run("unknown type message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		unknownType := spectypes.MsgType(12345)
		signedSSVMessage.SSVMessage.MsgType = unknownType

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, fmt.Sprintf("%s, got %d", ErrUnknownSSVMessageType.Error(), unknownType))
	})

	// Receive a message with a wrong signature
	t.Run("wrong signature", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, wrongSignatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSignatureVerification.Error())
	})

	// Receive a message with an incorrect topic
	t.Run("incorrect topic", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := "incorrect"

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrIncorrectTopic.Error())
	})

	// Receive nil signed ssv message
	t.Run("nil signed ssv message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)

		_, err = validator.handleSignedSSVMessage(nil, "", receivedAt)
		require.ErrorContains(t, err, ErrNilSignedSSVMessage.Error())
	})

	// Receive nil ssv message
	t.Run("nil ssv message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, "", receivedAt)
		require.ErrorContains(t, err, ErrNilSSVMessage.Error())
	})

	// Receive zero round
	t.Run("zero round", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.Round = specqbft.NoRound
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrZeroRound.Error())
	})

	// Receive a message with no signatures
	t.Run("no signatures", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.Signatures = [][]byte{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrNoSignatures.Error())
	})

	// Receive a message with mismatched identifier
	t.Run("mismatched identifier", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			wrongID := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID[:], nonCommitteeRole)
			message.Identifier = wrongID[:]
		})
		signedSSVMessage.SSVMessage.MsgID = committeeIdentifier

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrMismatchedIdentifier.Error())
	})

	// Receive a prepare/commit message with FullData
	t.Run("prepare/commit with FullData", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.PrepareMsgType
		})

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrPrepareOrCommitWithFullData.Error())

		signedSSVMessage = generateSignedMessage(ks, committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrPrepareOrCommitWithFullData.Error())
	})

	// Receive a non-consensus message with FullData
	t.Run("non-consensus with FullData", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		ssvMessage := spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0))
		ssvMessage.MsgID = committeeIdentifier
		signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)
		signedSSVMessage.FullData = []byte{1}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrFullDataNotInConsensusMessage)
	})

	// Receive a partial signature message with multiple signers
	t.Run("partial signature with multiple signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		ssvMessage := spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0))
		ssvMessage.MsgID = committeeIdentifier
		signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)
		signedSSVMessage.OperatorIDs = []spectypes.OperatorID{1, 2}
		signedSSVMessage.Signatures = append(signedSSVMessage.Signatures, signedSSVMessage.Signatures[0])

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrPartialSigOneSigner)
	})

	// Receive a partial signature message with too many signers
	t.Run("partial signature with too many messages", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
		for i := 0; i < 12; i++ {
			messages.Messages = append(messages.Messages, messages.Messages[0])
		}

		data, err := messages.Encode()
		require.NoError(t, err)

		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   committeeIdentifier,
			Data:    data,
		}

		signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrTooManyPartialSignatureMessages.Error())
	})

	// Receive a partial signature message with triple validator index
	t.Run("partial signature with triple validator index", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
		for i := 0; i < 3; i++ {
			messages.Messages = append(messages.Messages, messages.Messages[0])
		}

		data, err := messages.Encode()
		require.NoError(t, err)

		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   committeeIdentifier,
			Data:    data,
		}

		signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(spectypes.CommitteeID(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrTripleValidatorIndexInPartialSignatures.Error())
	})

	// Receive a partial signature message with validator index mismatch
	t.Run("partial signature with validator index mismatch", func(t *testing.T) {
		validator := New(netCfg, validatorStore, operators, dutyStore, signatureVerifier, phase0.Epoch(0)).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1, spec.DataVersionPhase0)
		messages.Messages[0].ValidatorIndex = math.MaxUint64

		data, err := messages.Encode()
		require.NoError(t, err)

		ssvMessage := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   nonCommitteeIdentifier,
			Data:    data,
		}

		signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(ks, ssvMessage)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		topicID := commons.CommitteeTopicID(committeeID)[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrValidatorIndexMismatch.Error())
	})
}

// Deep copy helper function for testing purposes only
func cloneSSVShare(t *testing.T, original *ssvtypes.SSVShare) *ssvtypes.SSVShare {
	// json encode original
	originalJSON, err := json.Marshal(original)
	require.NoError(t, err)

	// json decode original
	cloned := new(ssvtypes.SSVShare)
	require.NoError(t, json.Unmarshal(originalJSON, cloned))

	return cloned
}

type shareSet struct {
	active                      *ssvtypes.SSVShare
	liquidated                  *ssvtypes.SSVShare
	inactive                    *ssvtypes.SSVShare
	nonUpdatedMetadata          *ssvtypes.SSVShare
	nonUpdatedMetadataNextEpoch *ssvtypes.SSVShare
	noMetadata                  *ssvtypes.SSVShare
}

func generateShares(t *testing.T, ks *spectestingutils.TestKeySet, ns storage.Storage, netCfg networkconfig.NetworkConfig) shareSet {
	activeShare := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:     eth2apiv1.ValidatorStateActiveOngoing,
		Liquidated: false,
	}

	require.NoError(t, ns.Shares().Save(nil, activeShare))

	liquidatedShare := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:     eth2apiv1.ValidatorStateActiveOngoing,
		Liquidated: true,
	}

	liquidatedSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(liquidatedShare.ValidatorPubKey[:], liquidatedSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, liquidatedShare))

	inactiveShare := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:     eth2apiv1.ValidatorStateUnknown,
		Liquidated: false,
	}

	inactiveSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(inactiveShare.ValidatorPubKey[:], inactiveSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, inactiveShare))

	slot := netCfg.Beacon.EstimatedCurrentSlot()
	activationEpoch := netCfg.Beacon.EstimatedEpochAtSlot(slot)
	exitEpoch := goclient.FarFutureEpoch

	nonUpdatedMetadataShare := &ssvtypes.SSVShare{
		Share:           *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:          eth2apiv1.ValidatorStatePendingQueued,
		ActivationEpoch: activationEpoch,
		ExitEpoch:       exitEpoch,
		Liquidated:      false,
	}

	nonUpdatedMetadataSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataShare.ValidatorPubKey[:], nonUpdatedMetadataSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataShare))

	nonUpdatedMetadataNextEpochShare := &ssvtypes.SSVShare{
		Share:           *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:          eth2apiv1.ValidatorStatePendingQueued,
		ActivationEpoch: activationEpoch + 1,
		ExitEpoch:       exitEpoch,
		Liquidated:      false,
	}

	nonUpdatedMetadataNextEpochSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataNextEpochShare.ValidatorPubKey[:], nonUpdatedMetadataNextEpochSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataNextEpochShare))

	noMetadataShare := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Liquidated: false,
	}

	noMetadataShareSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(noMetadataShare.ValidatorPubKey[:], noMetadataShareSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, noMetadataShare))

	return shareSet{
		active:                      activeShare,
		liquidated:                  liquidatedShare,
		inactive:                    inactiveShare,
		nonUpdatedMetadata:          nonUpdatedMetadataShare,
		nonUpdatedMetadataNextEpoch: nonUpdatedMetadataNextEpochShare,
		noMetadata:                  noMetadataShare,
	}
}

func generateSignedMessage(
	ks *spectestingutils.TestKeySet,
	identifier spectypes.MessageID,
	slot phase0.Slot,
	opts ...func(message *specqbft.Message),
) *spectypes.SignedSSVMessage {
	fullData := spectestingutils.TestingQBFTFullData
	height := specqbft.Height(slot)

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     height,
		Round:      specqbft.FirstRound,
		Identifier: identifier[:],
		Root:       sha256.Sum256(fullData),

		RoundChangeJustification: [][]byte{},
		PrepareJustification:     [][]byte{},
	}

	for _, opt := range opts {
		opt(qbftMessage)
	}

	signedSSVMessage := spectestingutils.SignQBFTMsg(ks.OperatorKeys[1], 1, qbftMessage)
	signedSSVMessage.FullData = fullData

	return signedSSVMessage
}

func generateMultiSignedMessage(
	ks *spectestingutils.TestKeySet,
	identifier spectypes.MessageID,
	slot phase0.Slot,
	opts ...func(message *specqbft.Message),
) *spectypes.SignedSSVMessage {
	fullData := spectestingutils.TestingQBFTFullData
	height := specqbft.Height(slot)

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     height,
		Round:      specqbft.FirstRound,
		Identifier: identifier[:],
		Root:       sha256.Sum256(fullData),

		RoundChangeJustification: [][]byte{},
		PrepareJustification:     [][]byte{},
	}

	for _, opt := range opts {
		opt(qbftMessage)
	}

	signedSSVMessage := spectestingutils.MultiSignQBFTMsg(
		[]*rsa.PrivateKey{ks.OperatorKeys[1], ks.OperatorKeys[2], ks.OperatorKeys[3]},
		[]spectypes.OperatorID{1, 2, 3},
		qbftMessage,
	)
	signedSSVMessage.FullData = fullData

	return signedSSVMessage
}

var generateRandaoMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	epoch phase0.Epoch,
	slot phase0.Slot,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(epoch), d, sk.GetPublicKey().Serialize(), spectypes.DomainRandao)

	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	msgs.Messages = append(msgs.Messages, &spectypes.PartialSignatureMessage{
		PartialSignature: signed[:],
		SigningRoot:      root,
		Signer:           id,
		ValidatorIndex:   spectestingutils.TestingValidatorIndex,
	})

	return &msgs
}
