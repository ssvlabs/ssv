package validation

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type blockingSignatureVerifier struct {
	mu      sync.Mutex
	started chan int
	release chan struct{}
	calls   int
}

type validationActorTestEnv struct {
	validator           *messageValidator
	committeeID         spectypes.CommitteeID
	committeeIdentifier spectypes.MessageID
	netCfg              *networkconfig.Network
	ks                  *spectestingutils.TestKeySet
}

func (v *blockingSignatureVerifier) VerifySignature(spectypes.OperatorID, *spectypes.SSVMessage, []byte) error {
	v.mu.Lock()
	v.calls++
	call := v.calls
	v.mu.Unlock()

	v.started <- call
	<-v.release
	return nil
}

func newValidationActorTestEnv(t *testing.T, verifier interface {
	VerifySignature(spectypes.OperatorID, *spectypes.SSVMessage, []byte) error
}) validationActorTestEnv {
	ctrl := gomock.NewController(t)

	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
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
		if id != committeeID {
			return nil, false
		}

		share1 := cloneSSVShare(t, shares.active)
		share2 := cloneSSVShare(t, share1)
		share2.ValidatorIndex = share1.ValidatorIndex + 1
		share3 := cloneSSVShare(t, share2)
		share3.ValidatorIndex = share2.ValidatorIndex + 1

		return &registrystorage.Committee{
			ID:        id,
			Operators: committee,
			Shares: []*ssvtypes.SSVShare{
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
	}).AnyTimes()

	for _, id := range []spectypes.OperatorID{1, 2, 3, 4, 5} {
		operators.EXPECT().
			OperatorsExist(gomock.Any(), []spectypes.OperatorID{id}).
			Return(true, nil).
			AnyTimes()
	}

	validator := New(
		netCfg,
		validatorStore,
		operators,
		dutyStore,
		verifier,
	).(*messageValidator)

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID, spectypes.RoleCommittee)

	return validationActorTestEnv{
		validator:           validator,
		committeeID:         committeeID,
		committeeIdentifier: committeeIdentifier,
		netCfg:              netCfg,
		ks:                  ks,
	}
}

func TestConsensusActorVerifiesSameKeyMessagesConcurrently(t *testing.T) {
	verifier := &blockingSignatureVerifier{
		started: make(chan int, 2),
		release: make(chan struct{}, 2),
	}
	env := newValidationActorTestEnv(t, verifier)

	slot := env.netCfg.FirstSlotAtEpoch(1)
	newSignedMessage := func() *spectypes.SignedSSVMessage {
		signedSSVMessage := generateSignedMessage(env.ks, env.committeeIdentifier, slot, func(message *specqbft.Message) {
			message.MsgType = specqbft.CommitMsgType
		})
		signedSSVMessage.FullData = nil
		return signedSSVMessage
	}
	signedSSVMessage1 := newSignedMessage()
	signedSSVMessage2 := newSignedMessage()

	topicID := commons.CommitteeTopicID(env.committeeID)[0]
	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	receivedAt := env.netCfg.SlotStartTime(slot)

	done1 := make(chan error, 1)
	done2 := make(chan error, 1)

	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage1, topicID, peerID, receivedAt)
		done1 <- err
	}()

	select {
	case <-verifier.started:
	case <-time.After(time.Second):
		t.Fatal("first consensus signature verification did not start")
	}

	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage2, topicID, peerID, receivedAt)
		done2 <- err
	}()

	select {
	case <-verifier.started:
	case <-time.After(time.Second):
		t.Fatal("second consensus signature verification did not start while the first was still verifying")
	}

	verifier.release <- struct{}{}
	verifier.release <- struct{}{}

	err1 := <-done1
	err2 := <-done2

	require.True(t, (err1 == nil) != (err2 == nil), "expected exactly one consensus message to commit")

	dupErr := ErrDuplicatedMessage
	if err1 != nil {
		require.ErrorIs(t, err1, dupErr)
	} else {
		require.ErrorIs(t, err2, dupErr)
	}
}

func TestPartialActorVerifiesSameKeyMessagesConcurrently(t *testing.T) {
	verifier := &blockingSignatureVerifier{
		started: make(chan int, 2),
		release: make(chan struct{}, 2),
	}
	env := newValidationActorTestEnv(t, verifier)

	newSignedMessage := func() (*spectypes.SignedSSVMessage, phase0.Slot) {
		partialSignatureMessages := spectestingutils.PostConsensusAggregatorMsg(env.ks.Shares[1], 1, spec.DataVersionPhase0)
		ssvMessage := spectestingutils.SSVMsgAggregator(nil, partialSignatureMessages)
		ssvMessage.MsgID = env.committeeIdentifier
		return spectestingutils.SignPartialSigSSVMessage(env.ks, ssvMessage), partialSignatureMessages.Slot
	}
	signedSSVMessage1, slot := newSignedMessage()
	signedSSVMessage2, _ := newSignedMessage()

	topicID := commons.CommitteeTopicID(env.committeeID)[0]
	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	receivedAt := env.netCfg.SlotStartTime(slot)

	done1 := make(chan error, 1)
	done2 := make(chan error, 1)

	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage1, topicID, peerID, receivedAt)
		done1 <- err
	}()

	select {
	case <-verifier.started:
	case <-time.After(time.Second):
		t.Fatal("first partial signature verification did not start")
	}

	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage2, topicID, peerID, receivedAt)
		done2 <- err
	}()

	select {
	case <-verifier.started:
	case <-time.After(time.Second):
		t.Fatal("second partial signature verification did not start while the first was still verifying")
	}

	verifier.release <- struct{}{}
	verifier.release <- struct{}{}

	err1 := <-done1
	err2 := <-done2

	require.True(t, (err1 == nil) != (err2 == nil), "expected exactly one partial signature message to commit")

	if err1 != nil {
		require.ErrorIs(t, err1, ErrTooManyPartialSigMessage)
	} else {
		require.ErrorIs(t, err2, ErrTooManyPartialSigMessage)
	}
}

func TestValidationActorClosedIsIgnored(t *testing.T) {
	verifier := &blockingSignatureVerifier{
		started: make(chan int, 1),
		release: make(chan struct{}, 1),
	}
	env := newValidationActorTestEnv(t, verifier)

	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	result := env.validator.handleValidationError(context.Background(), peerID, nil, errValidationActorClosed)
	require.Equal(t, pubsub.ValidationIgnore, result)
}
