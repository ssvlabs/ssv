package validation

import (
	"bytes"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

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

type observingSignatureVerifier struct {
	called chan struct{}
}

type validationLockTestEnv struct {
	validator           *messageValidator
	committeeID         spectypes.CommitteeID
	committeeIdentifier spectypes.MessageID
	netCfg              *networkconfig.Network
	ks                  *spectestingutils.TestKeySet
}

func (v *observingSignatureVerifier) VerifySignature(spectypes.OperatorID, *spectypes.SSVMessage, []byte) error {
	select {
	case v.called <- struct{}{}:
	default:
	}

	return nil
}

func newValidationLockTestEnv(t *testing.T) validationLockTestEnv {
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

	verifier := &observingSignatureVerifier{called: make(chan struct{}, 1)}

	validator := New(
		netCfg,
		validatorStore,
		operators,
		dutyStore,
		verifier,
	).(*messageValidator)

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID, spectypes.RoleCommittee)

	return validationLockTestEnv{
		validator:           validator,
		committeeID:         committeeID,
		committeeIdentifier: committeeIdentifier,
		netCfg:              netCfg,
		ks:                  ks,
	}
}

func TestConsensusSignatureVerificationOutsideValidationLock(t *testing.T) {
	env := newValidationLockTestEnv(t)

	slot := env.netCfg.FirstSlotAtEpoch(1)
	signedSSVMessage := generateSignedMessage(env.ks, env.committeeIdentifier, slot)
	topicID := commons.CommitteeTopicID(env.committeeID)[0]
	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	validationMu := env.validator.getValidationLock(signedSSVMessage.SSVMessage.GetID())
	validationMu.Lock()
	locked := true
	defer func() {
		if locked {
			validationMu.Unlock()
		}
	}()

	done := make(chan error, 1)
	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage, topicID, peerID, env.netCfg.SlotStartTime(slot))
		done <- err
	}()

	select {
	case <-env.validator.signatureVerifier.(*observingSignatureVerifier).called:
	case <-time.After(time.Second):
		t.Fatal("signature verification did not start while the validation lock was held")
	}

	select {
	case err := <-done:
		t.Fatalf("validation completed before the lock was released: %v", err)
	default:
	}

	validationMu.Unlock()
	locked = false

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("validation did not complete after the lock was released")
	}
}

func TestPartialSignatureVerificationOutsideValidationLock(t *testing.T) {
	env := newValidationLockTestEnv(t)

	slot := env.netCfg.FirstSlotAtEpoch(1)
	ssvMessage := spectestingutils.SSVMsgAggregator(nil, spectestingutils.PostConsensusAggregatorMsg(env.ks.Shares[1], 1, spec.DataVersionPhase0))
	ssvMessage.MsgID = env.committeeIdentifier
	signedSSVMessage := spectestingutils.SignPartialSigSSVMessage(env.ks, ssvMessage)
	topicID := commons.CommitteeTopicID(env.committeeID)[0]
	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	validationMu := env.validator.getValidationLock(signedSSVMessage.SSVMessage.GetID())
	validationMu.Lock()
	locked := true
	defer func() {
		if locked {
			validationMu.Unlock()
		}
	}()

	done := make(chan error, 1)
	go func() {
		_, err := env.validator.handleSignedSSVMessage(signedSSVMessage, topicID, peerID, env.netCfg.SlotStartTime(slot))
		done <- err
	}()

	select {
	case <-env.validator.signatureVerifier.(*observingSignatureVerifier).called:
	case <-time.After(time.Second):
		t.Fatal("partial signature verification did not start while the validation lock was held")
	}

	select {
	case err := <-done:
		t.Fatalf("partial validation completed before the lock was released: %v", err)
	default:
	}

	validationMu.Unlock()
	locked = false

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("partial validation did not complete after the lock was released")
	}
}
