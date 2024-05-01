package msgvalidation

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/golang/mock/gomock"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/storage/mocks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/signatureverifier"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

func Test_ValidateSSVMessage(t *testing.T) {
	ctrl := gomock.NewController(t)

	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	netCfg := networkconfig.TestNetwork

	ks := spectestingutils.Testing4SharesSet()
	shares := generateShares(t, ks, ns, netCfg)

	dutyStore := dutystore.New()
	validatorStore := mocks.NewMockValidatorStore(ctrl)

	committee := maps.Keys(ks.Shares)
	slices.Sort(committee)

	committeeID := shares.active.CommitteeID()

	validatorStore.EXPECT().Committee(gomock.Any()).DoAndReturn(func(id ssvtypes.CommitteeID) *storage.Committee {
		if id == committeeID {
			return &storage.Committee{
				ID:        id,
				Operators: committee,
				Validators: []*ssvtypes.SSVShare{
					shares.active,
				},
			}
		}

		return nil
	}).AnyTimes()

	validatorStore.EXPECT().Validator(gomock.Any()).DoAndReturn(func(pubKey []byte) *ssvtypes.SSVShare {
		for _, share := range []*ssvtypes.SSVShare{
			shares.active,
			shares.liquidated,
			shares.inactive,
			shares.nonUpdatedMetadata,
			shares.nonUpdatedMetadataNextEpoch,
			shares.noMetadata,
		} {
			if bytes.Equal(share.ValidatorPubKey[:], pubKey) {
				return share
			}
		}
		return nil
	}).AnyTimes()

	signatureVerifier := signatureverifier.NewMockSignatureVerifier(ctrl)
	signatureVerifier.EXPECT().VerifySignature(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	committeeRole := spectypes.RoleCommittee
	nonCommitteeRole := spectypes.RoleAggregator

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.Domain, encodedCommitteeID, committeeRole)
	//nonCommitteeIdentifier := spectypes.NewMsgID(netCfg.Domain, ks.ValidatorPK.Serialize(), committeeRole)

	// Message validation happy flow, messages are not ignored or rejected and there are no errors
	t.Run("happy flow", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)
	})

	// Make sure messages are incremented and throw an ignore message if more than 1 for a commit
	t.Run("message counts", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		height := specqbft.Height(slot)

		msgID := committeeIdentifier
		state := validator.consensusState(msgID)
		for i := spectypes.OperatorID(1); i <= 4; i++ {
			signerState := state.GetSignerState(i)
			require.Nil(t, signerState)
		}

		signedSSVMessage := generateSignedMessage(ks, msgID, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
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

		signedSSVMessage = generateSignedMessage(ks, msgID, slot, func(message *specqbft.Message) {
			message.Round = 2
			message.MsgType = specqbft.PrepareMsgType
		})
		signedSSVMessage.FullData = nil

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)

		require.NotNil(t, state1)
		require.EqualValues(t, height, state1.Slot)
		require.EqualValues(t, 2, state1.Round)
		require.EqualValues(t, MessageCounts{Prepare: 1}, state1.MessageCounts)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedSSVMessage = generateSignedMessage(ks, msgID, slot+1, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		signedSSVMessage.FullData = nil
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1}, state1.MessageCounts)

		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.ErrorContains(t, err, ErrTooManySameTypeMessagesPerRound.Error())

		signedSSVMessage = generateMultiSignedMessage(ks, msgID, slot+1)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt.Add(netCfg.Beacon.SlotDurationSec()))
		require.NoError(t, err)
		require.NotNil(t, state1)
		require.EqualValues(t, height+1, state1.Slot)
		require.EqualValues(t, 1, state1.Round)
		require.EqualValues(t, MessageCounts{Commit: 1, Decided: 1}, state1.MessageCounts)
	})

	// Send a pubsub message with no data should cause an error
	t.Run("pubsub message has no data", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		pmsg := &pubsub.Message{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err := validator.handlePubsubMessage(pmsg, receivedAt)

		require.ErrorIs(t, err, ErrPubSubMessageHasNoData)
	})

	// Send a pubsub message where there is too much data should cause an error
	t.Run("pubsub data too big", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.CommitteeTopicID(committeeIdentifier[:])[0])
		msgSize := 10_000_000 + commons.MessageOffset

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, msgSize),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err = validator.handlePubsubMessage(pmsg, receivedAt)

		e := ErrPubSubDataTooBig
		e.got = msgSize
		require.ErrorIs(t, err, e)
	})

	// Send a malformed pubsub message (empty message) should return an error
	t.Run("empty pubsub message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		topic := commons.GetTopicFullName(commons.CommitteeTopicID(committeeIdentifier[:])[0])
		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Data:  bytes.Repeat([]byte{1}, 1+commons.MessageOffset),
				Topic: &topic,
				From:  []byte("16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r"),
			},
		}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err = validator.handlePubsubMessage(pmsg, receivedAt)

		require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
	})

	// Send a message with incorrect data (unable to decode incorrect message type)
	t.Run("bad data format", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, 500)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUndecodableMessageData.Error())
	})

	// Send a message with no data should return an error
	t.Run("no data", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = []byte{}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrEmptyData)

		signedSSVMessage.SSVMessage.Data = nil
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrEmptyData)
	})

	// Send a message where there is too much data should cause an error
	t.Run("data too big", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)

		const tooBigMsgSize = maxPayloadSize * 2
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, tooBigMsgSize)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		expectedErr := ErrSSVDataTooBig
		expectedErr.got = tooBigMsgSize
		expectedErr.want = maxPayloadSize
		require.ErrorIs(t, err, expectedErr)
	})

	// Send exact allowed data size amount but with invalid data (fails to decode)
	t.Run("data size borderline / malformed message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.Data = bytes.Repeat([]byte{1}, maxPayloadSize)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)

		require.ErrorContains(t, err, ErrUndecodableMessageData.Error())
	})

	// Send an invalid SSV message type returns an error
	t.Run("invalid SSV message type", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.SSVMessage.MsgType = math.MaxUint64

		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		require.ErrorContains(t, err, ErrUnknownSSVMessageType.Error())
	})

	// Empty validator public key returns an error
	t.Run("empty validator public key", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		badPK := spectypes.ValidatorPK{}
		badIdentifier := spectypes.NewMsgID(netCfg.Domain, badPK[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		require.ErrorContains(t, err, ErrDeserializePublicKey.Error())
	})

	// Generate random validator and validate it is unknown to the network
	t.Run("unknown validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		sk, err := eth2types.GenerateBLSPrivateKey()
		require.NoError(t, err)

		badIdentifier := spectypes.NewMsgID(netCfg.Domain, sk.PublicKey().Marshal(), nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, time.Now())
		expectedErr := ErrUnknownValidator
		expectedErr.got = hex.EncodeToString(sk.PublicKey().Marshal())
		require.ErrorIs(t, err, expectedErr)
	})

	// Make sure messages are dropped if on the incorrect network
	t.Run("wrong domain", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		wrongDomain := spectypes.DomainType{math.MaxUint8, math.MaxUint8, math.MaxUint8, math.MaxUint8}
		badIdentifier := spectypes.NewMsgID(wrongDomain, encodedCommitteeID, committeeRole)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrWrongDomain
		expectedErr.got = hex.EncodeToString(wrongDomain[:])
		expectedErr.want = hex.EncodeToString(netCfg.Domain[:])
		require.ErrorIs(t, err, expectedErr)
	})

	// Send message with a value that refers to a non-existent role
	t.Run("invalid role", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		badIdentifier := spectypes.NewMsgID(netCfg.Domain, encodedCommitteeID, math.MaxInt32)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrInvalidRole)
	})

	// Perform validator registration or voluntary exit with a consensus type message will give an error
	t.Run("unexpected consensus message", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		badIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.active.ValidatorPubKey[:], spectypes.RoleValidatorRegistration)
		signedSSVMessage := generateSignedMessage(ks, badIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrUnexpectedConsensusMessage
		expectedErr.got = spectypes.RoleValidatorRegistration
		require.ErrorIs(t, err, expectedErr)

		badIdentifier = spectypes.NewMsgID(netCfg.Domain, shares.active.ValidatorPubKey[:], spectypes.RoleVoluntaryExit)
		signedSSVMessage = generateSignedMessage(ks, badIdentifier, slot)

		topicID = commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr.got = spectypes.RoleVoluntaryExit
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator that is liquidated
	t.Run("liquidated validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		liquidatedIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.liquidated.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, liquidatedIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(nonCommitteeRole))
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrValidatorLiquidated
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator with unknown state
	t.Run("unknown state validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		inactiveIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.inactive.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, inactiveIdentifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(nonCommitteeRole))
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = eth2apiv1.ValidatorStateUnknown.String()
		require.ErrorIs(t, err, expectedErr)
	})

	// Ignore messages related to a validator that in pending queued state
	t.Run("pending queued state validator", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		nonUpdatedMetadataNextEpochIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.nonUpdatedMetadataNextEpoch.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, nonUpdatedMetadataNextEpochIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		expectedErr := ErrValidatorNotAttesting
		expectedErr.got = eth2apiv1.ValidatorStatePendingQueued.String()
		require.ErrorIs(t, err, expectedErr)
	})

	// Don't ignore messages related to a validator that in pending queued state (in case metadata is not updated),
	// but it is active (activation epoch <= current epoch)
	t.Run("active validator with pending queued state", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.EstimatedCurrentSlot()

		nonUpdatedMetadataIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.nonUpdatedMetadata.ValidatorPubKey[:], nonCommitteeRole)
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

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(nonCommitteeRole))
		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.NoError(t, err)
	})

	// Unable to process a message with a validator that is not on the network
	t.Run("no share metadata", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		noMetadataIdentifier := spectypes.NewMsgID(netCfg.Domain, shares.noMetadata.ValidatorPubKey[:], nonCommitteeRole)
		signedSSVMessage := generateSignedMessage(ks, noMetadataIdentifier, slot)

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoShareMetadata)
	})

	// Receive error if more than 2 attestation duties in an epoch
	t.Run("too many duties", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		epoch := phase0.Epoch(1)
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		dutyStore.Proposer.Add(epoch, slot, shares.active.ValidatorIndex, &eth2apiv1.ProposerDuty{}, true)
		dutyStore.Proposer.Add(epoch, slot+4, shares.active.ValidatorIndex, &eth2apiv1.ProposerDuty{}, true)
		dutyStore.Proposer.Add(epoch, slot+8, shares.active.ValidatorIndex, &eth2apiv1.ProposerDuty{}, true)

		role := spectypes.RoleAggregator
		identifier := spectypes.NewMsgID(netCfg.Domain, ks.ValidatorPK.Serialize(), role)
		signedSSVMessage := generateSignedMessage(ks, identifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(role)))
		require.NoError(t, err)

		signedSSVMessage = generateSignedMessage(ks, identifier, slot+4)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+4).Add(validator.waitAfterSlotStart(role)))
		require.NoError(t, err)

		signedSSVMessage = generateSignedMessage(ks, identifier, slot+8)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot+8).Add(validator.waitAfterSlotStart(role)))
		require.ErrorContains(t, err, ErrTooManyDutiesPerEpoch.Error())
	})

	// Throw error if getting a message for proposal and see there is no message from beacon
	t.Run("no proposal duties", func(t *testing.T) {
		const epoch = 1
		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)

		ds := dutystore.New()
		ds.Proposer.Add(epoch, slot, shares.active.ValidatorIndex+1, &eth2apiv1.ProposerDuty{}, true)
		validator := New(netCfg, validatorStore, ds, signatureVerifier).(*messageValidator)

		identifier := spectypes.NewMsgID(netCfg.Domain, ks.ValidatorPK.Serialize(), spectypes.RoleProposer)
		signedSSVMessage := generateSignedMessage(ks, identifier, slot)

		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(spectypes.RoleProposer)))
		require.ErrorContains(t, err, ErrNoDuty.Error())

		ds = dutystore.New()
		ds.Proposer.Add(epoch, slot, shares.active.ValidatorIndex, &eth2apiv1.ProposerDuty{}, true)
		validator = New(netCfg, validatorStore, ds, signatureVerifier).(*messageValidator)
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(spectypes.RoleProposer)))
		require.NoError(t, err)
	})

	//// Get error when receiving a message with over 13 partial signatures
	t.Run("partial message too big", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		msg := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, specqbft.Height(slot))
		for i := 0; i < 13; i++ {
			msg.Messages = append(msg.Messages, msg.Messages[0])
		}

		_, err := msg.Encode()
		require.ErrorContains(t, err, "max expected 13 and 14 found")
	})

	// Get error when receiving message from operator who is not affiliated with the validator
	t.Run("signer ID not in committee", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

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

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(nonCommitteeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorContains(t, err, ErrSignerNotInCommittee.Error())
	})

	// Get error when receiving message from operator who is non-existent (operator id 0)
	t.Run("partial zero signer ID", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		message := spectestingutils.SignPartialSigSSVMessage(ks, spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1)))
		message.OperatorIDs = []spectypes.OperatorID{0}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(message.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(message, topicID, receivedAt)
		require.ErrorIs(t, err, ErrZeroSigner)
	})

	// Get error when receiving partial signature message from operator who is the incorrect signer
	t.Run("partial inconsistent signer ID", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		message := spectestingutils.SignPartialSigSSVMessage(ks, spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1)))
		message.OperatorIDs = []spectypes.OperatorID{2}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(message.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(message, topicID, receivedAt)
		expectedErr := ErrInconsistentSigners
		expectedErr.got = spectypes.OperatorID(2)
		expectedErr.want = spectypes.OperatorID(1)
		require.ErrorIs(t, err, expectedErr)
	})

	// Receive error when "partialSignatureMessages" does not contain any "partialSignatureMessage"
	t.Run("no partial signature messages", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1)
		messages.Messages = nil
		signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], spectestingutils.SSVMsgAggregator(nil, messages))

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoPartialSignatureMessages)
	})

	// Receive error when the partial signature message is not enough bytes
	t.Run("partial wrong signature size", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

		message := spectestingutils.SignPartialSigSSVMessage(ks, spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1)))
		message.Signatures = [][]byte{{1}}

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.ValidatorTopicID(message.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(message, topicID, receivedAt)
		require.ErrorContains(t, err, ErrWrongRSASignatureSize.Error())
	})

	// Run partial message type validation tests
	t.Run("partial message type validation", func(t *testing.T) {
		slot := netCfg.Beacon.FirstSlotAtEpoch(1)

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
					validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

					messages := spectestingutils.PostConsensusAggregatorMsg(ks.Shares[1], 1)
					messages.Type = msgType

					encodedMessages, err := messages.Encode()
					require.NoError(t, err)

					ssvMessage := &spectypes.SSVMessage{
						MsgType: spectypes.SSVPartialSignatureMsgType,
						MsgID:   spectypes.NewMsgID(spectestingutils.TestingSSVDomainType, shares.active.ValidatorPubKey[:], role),
						Data:    encodedMessages,
					}

					signedSSVMessage := spectestingutils.SignedSSVMessageWithSigner(1, ks.OperatorKeys[1], ssvMessage)

					receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(role))
					topicID := commons.ValidatorTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
					if role == spectypes.RoleCommittee {
						topicID = commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
					}
					_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
					require.NoError(t, err)
				}
			}
		})

		//// Get error when receiving a message with an incorrect message type
		//t.Run("invalid message type", func(t *testing.T) {
		//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
		//	msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
		//
		//	msg := &spectypes.SignedPartialSignatureMessage{
		//		Message: spectypes.PartialSignatureMessages{
		//			Type: math.MaxUint64,
		//		},
		//		Signature: make([]byte, 96),
		//		Signer:    1,
		//	}
		//
		//	encoded, err := msg.Encode()
		//	require.NoError(t, err)
		//
		//	message := &spectypes.SSVMessage{
		//		MsgType: spectypes.SSVPartialSignatureMsgType,
		//		MsgID:   msgID,
		//		Data:    encoded,
		//	}
		//
		//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
		//	require.ErrorContains(t, err, ErrInvalidPartialSignatureType.Error())
		//})
		//
		//// Get error when sending an unexpected message type for the required duty (sending randao for attestor duty)
		//t.Run("mismatch", func(t *testing.T) {
		//	tests := map[spectypes.BeaconRole][]spectypes.PartialSigMsgType{
		//		spectypes.BNRoleAttester:                  {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
		//		spectypes.BNRoleAggregator:                {spectypes.RandaoPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
		//		spectypes.BNRoleProposer:                  {spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
		//		spectypes.BNRoleSyncCommittee:             {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig},
		//		spectypes.BNRoleSyncCommitteeContribution: {spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ValidatorRegistrationPartialSig},
		//		spectypes.BNRoleValidatorRegistration:     {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
		//		spectypes.BNRoleVoluntaryExit:             {spectypes.PostConsensusPartialSig, spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs},
		//	}
		//
		//	for committeeRole, msgTypes := range tests {
		//		for _, msgType := range msgTypes {
		//			validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
		//			msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
		//
		//			msg := &spectypes.SignedPartialSignatureMessage{
		//				Message: spectypes.PartialSignatureMessages{
		//					Type: msgType,
		//				},
		//				Signature: make([]byte, 96),
		//				Signer:    1,
		//			}
		//
		//			encoded, err := msg.Encode()
		//			require.NoError(t, err)
		//
		//			message := &spectypes.SSVMessage{
		//				MsgType: spectypes.SSVPartialSignatureMsgType,
		//				MsgID:   msgID,
		//				Data:    encoded,
		//			}
		//
		//			receivedAt := netCfg.Beacon.GetSlotStartTime(slot)
		//			_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
		//			require.ErrorContains(t, err, ErrPartialSignatureTypeRoleMismatch.Error())
		//		}
		//	}
		//})
	})

	//// Get error when receiving QBFT message with an invalid type
	//t.Run("invalid QBFT message type", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	msg := &specqbft.Message{
	//		MsgType:    math.MaxUint64,
	//		Height:     height,
	//		Round:      specqbft.FirstRound,
	//		Identifier: spectestingutils.TestingIdentifier,
	//		Root:       spectestingutils.TestingQBFTRootData,
	//	}
	//	signedMsg := spectestingutils.SignQBFTMsg(ks.Shares[1], 1, msg)
	//
	//	encodedValidSignedMessage, err := signedMsg.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	expectedErr := ErrUnknownQBFTMessageType
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Get error when receiving an incorrect signature size (too small)
	//t.Run("wrong signature size", func(t *testing.T) {
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	validSignedMessage.Signature = []byte{0x1}
	//
	//	_, err := validSignedMessage.Encode()
	//	require.Error(t, err)
	//})
	//
	//// Initialize signature tests
	//t.Run("zero signature", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	// Get error when receiving a consensus message with a zero signature
	//	t.Run("consensus message", func(t *testing.T) {
	//		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//		zeroSignature := [rsaSignatureSize]byte{}
	//		validSignedMessage.Signature = zeroSignature[:]
	//
	//		encoded, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//		require.ErrorIs(t, err, ErrEmptySignature)
	//	})
	//
	//	// Get error when receiving a consensus message with a zero signature
	//	t.Run("partial signature message", func(t *testing.T) {
	//		partialSigMessage := spectestingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, height)
	//		zeroSignature := [rsaSignatureSize]byte{}
	//		partialSigMessage.Signature = zeroSignature[:]
	//
	//		encoded, err := partialSigMessage.Encode()
	//		require.NoError(t, err)
	//
	//		ssvMessage := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVPartialSignatureMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handleSignedSSVMessage(ssvMessage, receivedAt, nil)
	//		require.ErrorIs(t, err, ErrEmptySignature)
	//	})
	//})
	//
	// Get error when receiving a message with an empty list of signers
	t.Run("no signers", func(t *testing.T) {
		validator := New(netCfg, validatorStore, dutyStore, signatureVerifier).(*messageValidator)

		slot := netCfg.Beacon.FirstSlotAtEpoch(1)
		signedSSVMessage := generateSignedMessage(ks, committeeIdentifier, slot)
		signedSSVMessage.OperatorIDs = nil

		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
		topicID := commons.CommitteeTopicID(signedSSVMessage.GetSSVMessage().GetID().GetSenderID())[0]
		_, err = validator.handleSignedSSVMessage(signedSSVMessage, topicID, receivedAt)
		require.ErrorIs(t, err, ErrNoSigners)
	})
	//
	//// Initialize no signer tests
	//t.Run("zero signer", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	inactiveSK, err := eth2types.GenerateBLSPrivateKey()
	//	require.NoError(t, err)
	//
	//	zeroSignerKS := spectestingutils.Testing7SharesSet()
	//	zeroSignerShare := &ssvtypes.SSVShare{
	//		Share: *spectestingutils.TestingShare(zeroSignerKS),
	//		Metadata: ssvtypes.Metadata{
	//			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
	//				Status: eth2apiv1.ValidatorStateActiveOngoing,
	//			},
	//			Liquidated: false,
	//		},
	//	}
	//	zeroSignerShare.Committee[0].OperatorID = 0
	//	zeroSignerShare.ValidatorPubKey = inactiveSK.PublicKey().Marshal()
	//
	//	require.NoError(t, ns.Shares().Save(nil, zeroSignerShare))
	//
	//	// Get error when receiving a consensus message with a zero signer
	//	t.Run("consensus message", func(t *testing.T) {
	//		validSignedMessage := spectestingutils.TestingProposalMessage(zeroSignerKS.Shares[1], 1)
	//		validSignedMessage.Signers = []spectypes.OperatorID{0}
	//
	//		encodedValidSignedMessage, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, zeroSignerShare.ValidatorPubKey, committeeRole),
	//			Data:    encodedValidSignedMessage,
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//		require.ErrorIs(t, err, ErrZeroSigner)
	//	})
	//
	//	// Get error when receiving a partial message with a zero signer
	//	t.Run("partial signature message", func(t *testing.T) {
	//		partialSignatureMessage := spectestingutils.PostConsensusAttestationMsg(zeroSignerKS.Shares[1], 1, specqbft.Height(slot))
	//		partialSignatureMessage.Signer = 0
	//
	//		encoded, err := partialSignatureMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVPartialSignatureMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, zeroSignerShare.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//		require.ErrorIs(t, err, ErrZeroSigner)
	//	})
	//
	//	require.NoError(t, ns.Shares().Delete(nil, zeroSignerShare.ValidatorPubKey))
	//})
	//
	//// Get error when receiving a message with duplicated signers
	//t.Run("non unique signer", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
	//		[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})
	//
	//	validSignedMessage.Signers = []spectypes.OperatorID{1, 2, 2}
	//
	//	encoded, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encoded,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	require.ErrorIs(t, err, ErrDuplicatedSigner)
	//})
	//
	//// Get error when receiving a message with non-sorted signers
	//t.Run("signers not sorted", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
	//		[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})
	//
	//	validSignedMessage.Signers = []spectypes.OperatorID{3, 2, 1}
	//
	//	encoded, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encoded,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	require.ErrorIs(t, err, ErrSignersNotSorted)
	//})
	//
	//// Get error when receiving message from non quorum size amount of signers
	//t.Run("wrong signers length", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	validSignedMessage := spectestingutils.TestingCommitMultiSignerMessage(
	//		[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})
	//
	//	validSignedMessage.Signers = []spectypes.OperatorID{1, 2}
	//
	//	encoded, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encoded,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	expectedErr := ErrTooManySigners
	//	expectedErr.got = 2
	//	expectedErr.want = "between 3 and 4"
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Get error when receiving a non decided message with multiple signers
	//t.Run("non decided with multiple signers", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	validSignedMessage := spectestingutils.TestingMultiSignerProposalMessage(
	//		[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3})
	//
	//	encoded, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encoded,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	expectedErr := ErrNonDecidedWithMultipleSigners
	//	expectedErr.got = 3
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Send late message for all roles and receive late message error
	//t.Run("late message", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	tests := map[spectypes.BeaconRole]time.Time{
	//		spectypes.BNRoleAttester:                  netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(spectypes.BNRoleAttester)),
	//		spectypes.BNRoleAggregator:                netCfg.Beacon.GetSlotStartTime(slot + 35).Add(validator.waitAfterSlotStart(spectypes.BNRoleAggregator)),
	//		spectypes.BNRoleProposer:                  netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleProposer)),
	//		spectypes.BNRoleSyncCommittee:             netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleSyncCommittee)),
	//		spectypes.BNRoleSyncCommitteeContribution: netCfg.Beacon.GetSlotStartTime(slot + 4).Add(validator.waitAfterSlotStart(spectypes.BNRoleSyncCommitteeContribution)),
	//	}
	//
	//	for committeeRole, receivedAt := range tests {
	//		committeeRole, receivedAt := committeeRole, receivedAt
	//		t.Run(committeeRole.String(), func(t *testing.T) {
	//			msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//
	//			message := &spectypes.SSVMessage{
	//				MsgType: spectypes.SSVConsensusMsgType,
	//				MsgID:   msgID,
	//				Data:    encodedValidSignedMessage,
	//			}
	//
	//			_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//			require.ErrorContains(t, err, ErrLateMessage.Error())
	//		})
	//	}
	//})
	//
	//// Send early message for all roles before the duty start and receive early message error
	//t.Run("early message", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot - 1)
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	require.ErrorIs(t, err, ErrEarlyMessage)
	//})
	//
	//// Send message from non-leader acting as a leader should receive an error
	//t.Run("not a leader", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[2], 2, height)
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	expectedErr := ErrSignerNotLeader
	//	expectedErr.got = spectypes.OperatorID(2)
	//	expectedErr.want = spectypes.OperatorID(1)
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Send wrong size of data (8 bytes) for a prepare justification message should receive an error
	//t.Run("malformed prepare justification", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	validSignedMessage.Message.PrepareJustification = [][]byte{{1}}
	//
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	require.ErrorContains(t, err, ErrMalformedPrepareJustifications.Error())
	//})
	//
	//// Send prepare justification message without a proposal message should receive an error
	//t.Run("non-proposal with prepare justification", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	msg := spectestingutils.TestingProposalMessageWithParams(
	//		ks.Shares[1], spectypes.OperatorID(1), specqbft.FirstRound, specqbft.FirstHeight, spectestingutils.TestingQBFTRootData,
	//		nil,
	//		spectestingutils.MarshalJustifications([]*specqbft.SignedMessage{
	//			spectestingutils.TestingRoundChangeMessage(ks.Shares[1], spectypes.OperatorID(1)),
	//		}))
	//	msg.Message.MsgType = specqbft.PrepareMsgType
	//
	//	encodedValidSignedMessage, err := msg.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	expectedErr := ErrUnexpectedPrepareJustifications
	//	expectedErr.got = specqbft.PrepareMsgType
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Send round change justification message without a proposal message should receive an error
	//t.Run("non-proposal with round change justification", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	msg := spectestingutils.TestingProposalMessageWithParams(
	//		ks.Shares[1], spectypes.OperatorID(1), specqbft.FirstRound, specqbft.FirstHeight, spectestingutils.TestingQBFTRootData,
	//		spectestingutils.MarshalJustifications([]*specqbft.SignedMessage{
	//			spectestingutils.TestingPrepareMessage(ks.Shares[1], spectypes.OperatorID(1)),
	//		}),
	//		nil,
	//	)
	//	msg.Message.MsgType = specqbft.PrepareMsgType
	//
	//	encodedValidSignedMessage, err := msg.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	expectedErr := ErrUnexpectedRoundChangeJustifications
	//	expectedErr.got = specqbft.PrepareMsgType
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Send round change justification message with a malformed message (1 byte) should receive an error
	//t.Run("malformed round change justification", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	validSignedMessage.Message.RoundChangeJustification = [][]byte{{1}}
	//
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	require.ErrorContains(t, err, ErrMalformedRoundChangeJustifications.Error())
	//})
	//
	//// Send message root hash that doesnt match the expected root hash should receive an error
	//t.Run("wrong root hash", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, height)
	//	validSignedMessage.FullData = []byte{1}
	//
	//	encodedValidSignedMessage, err := validSignedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedValidSignedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//
	//	expectedErr := ErrInvalidHash
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive proposal from same operator twice with different messages (same round) should receive an error
	//t.Run("double proposal with different data", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	signed1 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
	//	encodedSigned1, err := signed1.Encode()
	//	require.NoError(t, err)
	//
	//	message1 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned1,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message1, receivedAt, nil)
	//	require.NoError(t, err)
	//
	//	signed2 := spectestingutils.TestingProposalMessageWithRound(ks.Shares[1], 1, 1)
	//	signed2.FullData = []byte{1}
	//	signed2.Message.Root, err = specqbft.HashDataRoot(signed2.FullData)
	//	require.NoError(t, err)
	//
	//	encodedSigned2, err := signed2.Encode()
	//	require.NoError(t, err)
	//
	//	message2 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned2,
	//	}
	//
	//	_, _, err = validator.handleSignedSSVMessage(message2, receivedAt, nil)
	//	expectedErr := ErrDuplicatedProposalWithDifferentData
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive prepare from same operator twice with different messages (same round) should receive an error
	//t.Run("double prepare", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	signed1 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
	//	encodedSigned1, err := signed1.Encode()
	//	require.NoError(t, err)
	//
	//	message1 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned1,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message1, receivedAt, nil)
	//	require.NoError(t, err)
	//
	//	signed2 := spectestingutils.TestingPrepareMessage(ks.Shares[1], 1)
	//	require.NoError(t, err)
	//
	//	encodedSigned2, err := signed2.Encode()
	//	require.NoError(t, err)
	//
	//	message2 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned2,
	//	}
	//
	//	_, _, err = validator.handleSignedSSVMessage(message2, receivedAt, nil)
	//	expectedErr := ErrTooManySameTypeMessagesPerRound
	//	expectedErr.got = "prepare, having pre-consensus: 0, proposal: 0, prepare: 1, commit: 0, decided: 0, round change: 0, post-consensus: 0"
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive commit from same operator twice with different messages (same round) should receive an error
	//t.Run("double commit", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	signed1 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
	//	encodedSigned1, err := signed1.Encode()
	//	require.NoError(t, err)
	//
	//	message1 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned1,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message1, receivedAt, nil)
	//	require.NoError(t, err)
	//
	//	signed2 := spectestingutils.TestingCommitMessage(ks.Shares[1], 1)
	//	encodedSigned2, err := signed2.Encode()
	//	require.NoError(t, err)
	//
	//	message2 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned2,
	//	}
	//
	//	_, _, err = validator.handleSignedSSVMessage(message2, receivedAt, nil)
	//	expectedErr := ErrTooManySameTypeMessagesPerRound
	//	expectedErr.got = "commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 1, decided: 0, round change: 0, post-consensus: 0"
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive round change from same operator twice with different messages (same round) should receive an error
	//t.Run("double round change", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	signed1 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
	//	encodedSigned1, err := signed1.Encode()
	//	require.NoError(t, err)
	//
	//	message1 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned1,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(message1, receivedAt, nil)
	//	require.NoError(t, err)
	//
	//	signed2 := spectestingutils.TestingRoundChangeMessage(ks.Shares[1], 1)
	//	encodedSigned2, err := signed2.Encode()
	//	require.NoError(t, err)
	//
	//	message2 := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//		Data:    encodedSigned2,
	//	}
	//
	//	_, _, err = validator.handleSignedSSVMessage(message2, receivedAt, nil)
	//	expectedErr := ErrTooManySameTypeMessagesPerRound
	//	expectedErr.got = "round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 0, round change: 1, post-consensus: 0"
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive too many decided messages should receive an error
	//t.Run("too many decided", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//
	//	signed := spectestingutils.TestingCommitMultiSignerMessageWithRound(
	//		[]*bls.SecretKey{ks.Shares[1], ks.Shares[2], ks.Shares[3]}, []spectypes.OperatorID{1, 2, 3}, 1)
	//	encodedSigned, err := signed.Encode()
	//	require.NoError(t, err)
	//
	//	message := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   msgID,
	//		Data:    encodedSigned,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//
	//	for i := 0; i < maxDecidedCount(len(share.Committee)); i++ {
	//		_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//		require.NoError(t, err)
	//	}
	//
	//	_, _, err = validator.handleSignedSSVMessage(message, receivedAt, nil)
	//	expectedErr := ErrTooManySameTypeMessagesPerRound
	//	expectedErr.got = "decided, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 8, round change: 0, post-consensus: 0"
	//	require.ErrorIs(t, err, expectedErr)
	//})
	//
	//// Receive message from a round that is too high for that epoch should receive an error
	//t.Run("round too high", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	tests := map[spectypes.BeaconRole]specqbft.Round{
	//		spectypes.BNRoleAttester:                  13,
	//		spectypes.BNRoleAggregator:                13,
	//		spectypes.BNRoleProposer:                  7,
	//		spectypes.BNRoleSyncCommittee:             7,
	//		spectypes.BNRoleSyncCommitteeContribution: 7,
	//	}
	//
	//	for committeeRole, round := range tests {
	//		committeeRole, round := committeeRole, round
	//		t.Run(committeeRole.String(), func(t *testing.T) {
	//			msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//
	//			signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, round)
	//			encodedMessage, err := signedMessage.Encode()
	//			require.NoError(t, err)
	//
	//			ssvMessage := &spectypes.SSVMessage{
	//				MsgType: spectypes.SSVConsensusMsgType,
	//				MsgID:   msgID,
	//				Data:    encodedMessage,
	//			}
	//
	//			receivedAt := netCfg.Beacon.GetSlotStartTime(0).Add(validator.waitAfterSlotStart(committeeRole))
	//			_, _, err = validator.handleSignedSSVMessage(ssvMessage, receivedAt, nil)
	//			require.ErrorContains(t, err, ErrRoundTooHigh.Error())
	//		})
	//	}
	//})
	//
	//// Receive message from a round that is incorrect for current epoch should receive an error
	//t.Run("round already advanced", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	signedMessage := spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 2)
	//	encodedMessage, err := signedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	ssvMessage := &spectypes.SSVMessage{
	//		MsgType: spectypes.SSVConsensusMsgType,
	//		MsgID:   msgID,
	//		Data:    encodedMessage,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(ssvMessage, receivedAt, nil)
	//	require.NoError(t, err)
	//
	//	signedMessage = spectestingutils.TestingPrepareMessageWithRound(ks.Shares[1], 1, 1)
	//	encodedMessage, err = signedMessage.Encode()
	//	require.NoError(t, err)
	//
	//	ssvMessage.Data = encodedMessage
	//	_, _, err = validator.handleSignedSSVMessage(ssvMessage, receivedAt, nil)
	//	require.ErrorContains(t, err, ErrRoundAlreadyAdvanced.Error())
	//})
	//
	//// Initialize tests for testing when sending a message with a slot before the current one
	//t.Run("slot already advanced", func(t *testing.T) {
	//	msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//	height := specqbft.Height(slot)
	//
	//	// Send a consensus message with a slot before the current one should cause an error
	//	t.Run("consensus message", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		signedMessage := spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height+1)
	//		encodedMessage, err := signedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		ssvMessage := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   msgID,
	//			Data:    encodedMessage,
	//		}
	//
	//		_, _, err = validator.handleSignedSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(committeeRole)), nil)
	//		require.NoError(t, err)
	//
	//		signedMessage = spectestingutils.TestingPrepareMessageWithHeight(ks.Shares[1], 1, height)
	//		encodedMessage, err = signedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		ssvMessage.Data = encodedMessage
	//		_, _, err = validator.handleSignedSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole)), nil)
	//		require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
	//	})
	//
	//	// Send a partial signature message with a slot before the current one should cause an error
	//	t.Run("partial signature message", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		message := spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height+1)
	//		message.Message.Slot = phase0.Slot(height) + 1
	//		sig, err := spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
	//		require.NoError(t, err)
	//		message.Signature = sig
	//
	//		encodedMessage, err := message.Encode()
	//		require.NoError(t, err)
	//
	//		ssvMessage := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVPartialSignatureMsgType,
	//			MsgID:   msgID,
	//			Data:    encodedMessage,
	//		}
	//
	//		_, _, err = validator.handleSignedSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot+1).Add(validator.waitAfterSlotStart(committeeRole)), nil)
	//		require.NoError(t, err)
	//
	//		message = spectestingutils.PostConsensusAttestationMsg(ks.Shares[2], 2, height)
	//		message.Message.Slot = phase0.Slot(height)
	//		sig, err = spectestingutils.NewTestingKeyManager().SignRoot(message.Message, spectypes.PartialSignatureType, ks.Shares[2].GetPublicKey().Serialize())
	//		require.NoError(t, err)
	//		message.Signature = sig
	//
	//		encodedMessage, err = message.Encode()
	//		require.NoError(t, err)
	//
	//		ssvMessage.Data = encodedMessage
	//		_, _, err = validator.handleSignedSSVMessage(ssvMessage, netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole)), nil)
	//		require.ErrorContains(t, err, ErrSlotAlreadyAdvanced.Error())
	//	})
	//})
	//
	//// Receive an event message from an operator that is not myself should receive an error
	//t.Run("event message", func(t *testing.T) {
	//	validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//	msgID := spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole)
	//	slot := netCfg.Beacon.FirstSlotAtEpoch(1)
	//
	//	eventMsg := &ssvtypes.EventMsg{}
	//	encoded, err := eventMsg.Encode()
	//	require.NoError(t, err)
	//
	//	ssvMessage := &spectypes.SSVMessage{
	//		MsgType: ssvmessage.SSVEventMsgType,
	//		MsgID:   msgID,
	//		Data:    encoded,
	//	}
	//
	//	receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//	_, _, err = validator.handleSignedSSVMessage(ssvMessage, receivedAt, nil)
	//	require.ErrorIs(t, err, ErrEventMessage)
	//})
	//
	//// Get error when receiving an SSV message with an invalid signature.
	//t.Run("signature verification", func(t *testing.T) {
	//	epoch := phase0.Epoch(123456789)
	//
	//	t.Run("unsigned message", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[4], 4, specqbft.Height(epoch))
	//
	//		encoded, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		encodedMsg, err := commons.EncodeNetworkMsg(message)
	//		require.NoError(t, err)
	//
	//		topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	//		pMsg := &pubsub.Message{
	//			Message: &pspb.Message{
	//				Topic: &topicID[0],
	//				Data:  encodedMsg,
	//			},
	//		}
	//
	//		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handlePubsubMessage(pMsg, receivedAt)
	//		require.ErrorContains(t, err, ErrMalformedPubSubMessage.Error())
	//	})
	//
	//	t.Run("signed message", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
	//
	//		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))
	//
	//		encoded, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		encodedMsg, err := commons.EncodeNetworkMsg(message)
	//		require.NoError(t, err)
	//
	//		privKey, err := keys.GeneratePrivateKey()
	//		require.NoError(t, err)
	//
	//		pubKey, err := privKey.Public().Base64()
	//		require.NoError(t, err)
	//
	//		const operatorID = spectypes.OperatorID(1)
	//
	//		od := &registrystorage.OperatorData{
	//			ID:           operatorID,
	//			PublicKey:    pubKey,
	//			OwnerAddress: common.Address{},
	//		}
	//
	//		found, err := ns.SaveOperatorData(nil, od)
	//		require.NoError(t, err)
	//		require.False(t, found)
	//
	//		signature, err := privKey.Sign(encodedMsg)
	//		require.NoError(t, err)
	//
	//		encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, operatorID, signature)
	//
	//		topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	//		pMsg := &pubsub.Message{
	//			Message: &pspb.Message{
	//				Topic: &topicID[0],
	//				Data:  encodedMsg,
	//			},
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handlePubsubMessage(pMsg, receivedAt)
	//		require.NoError(t, err)
	//
	//		require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
	//	})
	//
	//	t.Run("unexpected operator ID", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
	//
	//		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))
	//
	//		encoded, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		encodedMsg, err := commons.EncodeNetworkMsg(message)
	//		require.NoError(t, err)
	//
	//		privKey, err := keys.GeneratePrivateKey()
	//		require.NoError(t, err)
	//
	//		pubKey, err := privKey.Public().Base64()
	//		require.NoError(t, err)
	//
	//		const operatorID = spectypes.OperatorID(1)
	//
	//		od := &registrystorage.OperatorData{
	//			ID:           operatorID,
	//			PublicKey:    pubKey,
	//			OwnerAddress: common.Address{},
	//		}
	//
	//		found, err := ns.SaveOperatorData(nil, od)
	//		require.NoError(t, err)
	//		require.False(t, found)
	//
	//		signature, err := privKey.Sign(encodedMsg)
	//		require.NoError(t, err)
	//
	//		const unexpectedOperatorID = 2
	//		encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, unexpectedOperatorID, signature)
	//
	//		topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	//		pMsg := &pubsub.Message{
	//			Message: &pspb.Message{
	//				Topic: &topicID[0],
	//				Data:  encodedMsg,
	//			},
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handlePubsubMessage(pMsg, receivedAt)
	//		require.ErrorContains(t, err, ErrOperatorNotFound.Error())
	//
	//		require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
	//	})
	//
	//	t.Run("malformed signature", func(t *testing.T) {
	//		validator := New(netCfg, WithValidatorStore(ns)).(*messageValidator)
	//
	//		slot := netCfg.Beacon.FirstSlotAtEpoch(epoch)
	//
	//		validSignedMessage := spectestingutils.TestingProposalMessageWithHeight(ks.Shares[1], 1, specqbft.Height(slot))
	//
	//		encoded, err := validSignedMessage.Encode()
	//		require.NoError(t, err)
	//
	//		message := &spectypes.SSVMessage{
	//			MsgType: spectypes.SSVConsensusMsgType,
	//			MsgID:   spectypes.NewMsgID(netCfg.Domain, share.ValidatorPubKey, committeeRole),
	//			Data:    encoded,
	//		}
	//
	//		encodedMsg, err := commons.EncodeNetworkMsg(message)
	//		require.NoError(t, err)
	//
	//		privKey, err := keys.GeneratePrivateKey()
	//		require.NoError(t, err)
	//
	//		pubKey, err := privKey.Public().Base64()
	//		require.NoError(t, err)
	//
	//		const operatorID = spectypes.OperatorID(1)
	//
	//		od := &registrystorage.OperatorData{
	//			ID:           operatorID,
	//			PublicKey:    pubKey,
	//			OwnerAddress: common.Address{},
	//		}
	//
	//		found, err := ns.SaveOperatorData(nil, od)
	//		require.NoError(t, err)
	//		require.False(t, found)
	//
	//		encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, operatorID, bytes.Repeat([]byte{1}, 256))
	//
	//		topicID := commons.ValidatorTopicID(message.GetID().GetPubKey())
	//		pMsg := &pubsub.Message{
	//			Message: &pspb.Message{
	//				Topic: &topicID[0],
	//				Data:  encodedMsg,
	//			},
	//		}
	//
	//		receivedAt := netCfg.Beacon.GetSlotStartTime(slot).Add(validator.waitAfterSlotStart(committeeRole))
	//		_, _, err = validator.handlePubsubMessage(pMsg, receivedAt)
	//		require.ErrorContains(t, err, ErrSignatureVerification.Error())
	//
	//		require.NoError(t, ns.DeleteOperatorData(nil, operatorID))
	//	})
	//})
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
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  spectestingutils.TestingShare(ks).ValidatorIndex,
			},
			Liquidated: false,
		},
	}

	require.NoError(t, ns.Shares().Save(nil, activeShare))

	liquidatedShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  spectestingutils.TestingShare(ks).ValidatorIndex,
			},
			Liquidated: true,
		},
	}

	liquidatedSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(liquidatedShare.ValidatorPubKey[:], liquidatedSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, liquidatedShare))

	inactiveShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateUnknown,
			},
			Liquidated: false,
		},
	}

	inactiveSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(inactiveShare.ValidatorPubKey[:], inactiveSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, inactiveShare))

	slot := netCfg.Beacon.EstimatedCurrentSlot()
	epoch := netCfg.Beacon.EstimatedEpochAtSlot(slot)

	nonUpdatedMetadataShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: epoch,
			},
			Liquidated: false,
		},
	}

	nonUpdatedMetadataSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataShare.ValidatorPubKey[:], nonUpdatedMetadataSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataShare))

	nonUpdatedMetadataNextEpochShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: epoch + 1,
			},
			Liquidated: false,
		},
	}

	nonUpdatedMetadataNextEpochSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataNextEpochShare.ValidatorPubKey[:], nonUpdatedMetadataNextEpochSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataNextEpochShare))

	noMetadataShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: nil,
			Liquidated:     false,
		},
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
