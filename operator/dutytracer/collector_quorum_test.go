package validator

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

// TestCollector_QuorumAfterFlush tests the critical scenario where signatures arrive
// BEFORE the proposal, and quorum should be detected immediately when proposal arrives.
func TestCollector_QuorumAfterFlush(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4}, // 4 operators = need 3 for quorum
	}

	var validatorPK1 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	attestationRoot := phase0.Root{1, 2, 3, 4, 5}
	syncCommitteeRoot := phase0.Root{6, 7, 8, 9, 10}
	fakeSig := [96]byte{}

	t.Run("signatures before proposal reach quorum after flush", func(t *testing.T) {
		listener.Reset()

		// Step 1: Send signatures from 3 operators BEFORE proposal arrives
		// These should be buffered in pendingByRoot
		operators := []spectypes.OperatorID{operator1, operator2, operator3}
		for _, op := range operators {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex1, Signer: op, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Verify no quorum detected yet (signatures are pending)
		calls := listener.GetCalls()
		require.Len(t, calls, 0, "no quorum should be detected before proposal arrives")

		// Step 2: Proposal arrives with attestation and sync committee roots
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)

		trace.Lock()
		trace.attestationRoot = attestationRoot
		trace.syncCommitteeRoot = syncCommitteeRoot
		trace.roleRootsReady = true
		trace.flushPending()
		// This is the critical call that our fix added!
		collector.checkQuorumAfterFlush(logger, committeeID, slot, trace)
		trace.Unlock()

		// Verify quorum detected IMMEDIATELY after flush
		calls = listener.GetCalls()
		require.Len(t, calls, 1, "quorum should be detected immediately after proposal arrives and flushes pending")

		call := calls[0]
		assert.Equal(t, vIndex1, call.Index)
		assert.Equal(t, slot, call.Slot)
		assert.Equal(t, spectypes.BNRoleAttester, call.Role)
		assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, call.Signers)
	})
}

// TestCollector_RoleSpecificQuorum tests that attestation signatures don't trigger
// sync committee quorum and vice versa (prevents false positives).
func TestCollector_RoleSpecificQuorum(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		vIndex2   = phase0.ValidatorIndex(20)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4},
	}

	var validatorPK1, validatorPK2 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))
	copy(validatorPK2[:], []byte("validator_pk_2_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1
	share2 := &ssvtypes.SSVShare{}
	share2.ValidatorPubKey = validatorPK2

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex2).Return(share2, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	attestationRoot := phase0.Root{1, 2, 3, 4, 5}
	syncCommitteeRoot := phase0.Root{6, 7, 8, 9, 10}
	fakeSig := [96]byte{}

	t.Run("attestation quorum doesn't trigger sync committee quorum", func(t *testing.T) {
		listener.Reset()

		// Set up roots
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)
		trace.attestationRoot = attestationRoot
		trace.syncCommitteeRoot = syncCommitteeRoot
		trace.roleRootsReady = true

		// Send attestation signatures from 3 operators for V1 (reaches quorum)
		operators := []spectypes.OperatorID{operator1, operator2, operator3}
		for _, op := range operators {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex1, Signer: op, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Send sync committee signatures from only 2 operators for V2 (below quorum)
		for _, op := range []spectypes.OperatorID{operator1, operator2} {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex2, Signer: op, PartialSignature: fakeSig[:], SigningRoot: syncCommitteeRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Verify only attestation quorum published, NOT sync committee
		calls := listener.GetCalls()
		require.Len(t, calls, 1, "only attestation quorum should be published")

		call := calls[0]
		assert.Equal(t, vIndex1, call.Index)
		assert.Equal(t, spectypes.BNRoleAttester, call.Role)
		assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, call.Signers)
	})
}

// TestCollector_MixedTimingQuorum tests the scenario where some signatures arrive
// before the proposal and some after (mixed timing).
func TestCollector_MixedTimingQuorum(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4},
	}

	var validatorPK1 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	attestationRoot := phase0.Root{1, 2, 3, 4, 5}
	syncCommitteeRoot := phase0.Root{6, 7, 8, 9, 10}
	fakeSig := [96]byte{}

	t.Run("mixed timing: 2 before + 1 after proposal = quorum", func(t *testing.T) {
		listener.Reset()

		// Step 1: Send 2 signatures BEFORE proposal (not enough for quorum)
		for _, op := range []spectypes.OperatorID{operator1, operator2} {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex1, Signer: op, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Verify no quorum yet
		calls := listener.GetCalls()
		require.Len(t, calls, 0)

		// Step 2: Proposal arrives
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)

		trace.Lock()
		trace.attestationRoot = attestationRoot
		trace.syncCommitteeRoot = syncCommitteeRoot
		trace.roleRootsReady = true
		trace.flushPending()
		collector.checkQuorumAfterFlush(logger, committeeID, slot, trace)
		trace.Unlock()

		// Still no quorum (only 2/3)
		calls = listener.GetCalls()
		require.Len(t, calls, 0)

		// Step 3: Third signature arrives AFTER proposal
		partSigMessages := &spectypes.PartialSignatureMessages{
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{
				{ValidatorIndex: vIndex1, Signer: operator3, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
			},
		}

		partSigMessagesData, err := partSigMessages.Encode()
		require.NoError(t, err)

		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
		require.NoError(t, err)

		// NOW quorum should be detected
		calls = listener.GetCalls()
		require.Len(t, calls, 1)

		call := calls[0]
		assert.Equal(t, vIndex1, call.Index)
		assert.Equal(t, spectypes.BNRoleAttester, call.Role)
		assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, call.Signers)
	})
}

// TestCollector_UnknownRootQuorum tests that signatures with unknown roots
// don't contribute to quorum.
func TestCollector_UnknownRootQuorum(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4},
	}

	var validatorPK1 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	attestationRoot := phase0.Root{1, 2, 3, 4, 5}
	syncCommitteeRoot := phase0.Root{6, 7, 8, 9, 10}
	unknownRoot := phase0.Root{99, 99, 99, 99, 99}
	fakeSig := [96]byte{}

	t.Run("unknown roots don't contribute to quorum", func(t *testing.T) {
		listener.Reset()

		// Set up roots
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)
		trace.attestationRoot = attestationRoot
		trace.syncCommitteeRoot = syncCommitteeRoot
		trace.roleRootsReady = true

		// Send 2 signatures with correct attestation root
		for _, op := range []spectypes.OperatorID{operator1, operator2} {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex1, Signer: op, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Send 1 signature with UNKNOWN root (should not count)
		partSigMessages := &spectypes.PartialSignatureMessages{
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{
				{ValidatorIndex: vIndex1, Signer: operator3, PartialSignature: fakeSig[:], SigningRoot: unknownRoot},
			},
		}

		partSigMessagesData, err := partSigMessages.Encode()
		require.NoError(t, err)

		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
		require.NoError(t, err)

		// Verify NO quorum (only 2 valid signatures, unknown root doesn't count)
		calls := listener.GetCalls()
		require.Len(t, calls, 0, "unknown root signatures should not contribute to quorum")

		// Verify unknown root is in pending
		trace, _, err = collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)
		trace.Lock()
		assert.Contains(t, trace.pendingByRoot, unknownRoot, "unknown root should be in pending")
		trace.Unlock()
	})
}

// TestCollector_MultipleValidatorsAndRoles tests that multiple validators
// with different roles can reach quorum independently.
func TestCollector_MultipleValidatorsAndRoles(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		vIndex2   = phase0.ValidatorIndex(20)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4},
	}

	var validatorPK1, validatorPK2 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))
	copy(validatorPK2[:], []byte("validator_pk_2_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1
	share2 := &ssvtypes.SSVShare{}
	share2.ValidatorPubKey = validatorPK2

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex2).Return(share2, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	attestationRoot := phase0.Root{1, 2, 3, 4, 5}
	syncCommitteeRoot := phase0.Root{6, 7, 8, 9, 10}
	fakeSig := [96]byte{}

	t.Run("V1 attester + V2 sync committee both reach quorum", func(t *testing.T) {
		listener.Reset()

		// Set up roots
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)
		trace.attestationRoot = attestationRoot
		trace.syncCommitteeRoot = syncCommitteeRoot
		trace.roleRootsReady = true

		// Send combined messages: each operator signs both V1 attestation and V2 sync committee
		operators := []spectypes.OperatorID{operator1, operator2, operator3}
		for _, op := range operators {
			partSigMessages := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{ValidatorIndex: vIndex1, Signer: op, PartialSignature: fakeSig[:], SigningRoot: attestationRoot},
					{ValidatorIndex: vIndex2, Signer: op, PartialSignature: fakeSig[:], SigningRoot: syncCommitteeRoot},
				},
			}

			partSigMessagesData, err := partSigMessages.Encode()
			require.NoError(t, err)

			partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
			err = collector.Collect(context.TODO(), partSigMsg, dummyVerify)
			require.NoError(t, err)
		}

		// Verify TWO publications: one for V1/Attester, one for V2/SyncCommittee
		calls := listener.GetCalls()
		require.Len(t, calls, 2, "both validators should reach quorum independently")

		// Sort by validator index for predictable order
		if calls[0].Index > calls[1].Index {
			calls[0], calls[1] = calls[1], calls[0]
		}

		// Verify V1 attester quorum
		assert.Equal(t, vIndex1, calls[0].Index)
		assert.Equal(t, spectypes.BNRoleAttester, calls[0].Role)
		assert.ElementsMatch(t, operators, calls[0].Signers)

		// Verify V2 sync committee quorum
		assert.Equal(t, vIndex2, calls[1].Index)
		assert.Equal(t, spectypes.BNRoleSyncCommittee, calls[1].Role)
		assert.ElementsMatch(t, operators, calls[1].Signers)
	})
}

// TestCollector_checkAndPublishQuorumForRoleByIndex specifically tests the new
// helper method added for quorum detection after flush.
func TestCollector_checkAndPublishQuorumForRoleByIndex(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot      = phase0.Slot(100)
		vIndex1   = phase0.ValidatorIndex(10)
		operator1 = spectypes.OperatorID(1)
		operator2 = spectypes.OperatorID(2)
		operator3 = spectypes.OperatorID(3)
		operator4 = spectypes.OperatorID(4)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{operator1, operator2, operator3, operator4},
	}

	var validatorPK1 spectypes.ValidatorPK
	copy(validatorPK1[:], []byte("validator_pk_1_padded_to_48_bytes_exactly_here"))

	share1 := &ssvtypes.SSVShare{}
	share1.ValidatorPubKey = validatorPK1

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	validators.EXPECT().ValidatorByIndex(vIndex1).Return(share1, true).AnyTimes()

	listener := &mockDecidedListener{}
	dutyStore := new(mockDutyTraceStore)
	collector := New(logger, validators, nil, dutyStore, networkconfig.TestNetwork.Beacon, listener.OnDecided, nil)

	t.Run("checkAndPublishQuorumForRoleByIndex works correctly", func(t *testing.T) {
		listener.Reset()

		// Manually populate trace with signer data
		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)

		trace.Lock()
		defer trace.Unlock()

		// Add 3 signers to Attester bucket (simulating what flushPending does)
		trace.Attester = []*exporter.SignerData{
			{Signer: operator1, ValidatorIdx: []phase0.ValidatorIndex{vIndex1}, ReceivedTime: 1000},
			{Signer: operator2, ValidatorIdx: []phase0.ValidatorIndex{vIndex1}, ReceivedTime: 1001},
			{Signer: operator3, ValidatorIdx: []phase0.ValidatorIndex{vIndex1}, ReceivedTime: 1002},
		}

		threshold := uint64(3) // 4 operators, need 3 for quorum

		// Call the method directly
		collector.checkAndPublishQuorumForRoleByIndex(logger, trace, spectypes.BNRoleAttester, slot, vIndex1, threshold)

		// Verify quorum was published
		calls := listener.GetCalls()
		require.Len(t, calls, 1)

		call := calls[0]
		assert.Equal(t, vIndex1, call.Index)
		assert.Equal(t, slot, call.Slot)
		assert.Equal(t, spectypes.BNRoleAttester, call.Role)
		assert.ElementsMatch(t, []spectypes.OperatorID{operator1, operator2, operator3}, call.Signers)
	})

	t.Run("no duplicate publication on subsequent calls", func(t *testing.T) {
		listener.Reset()

		trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)

		trace.Lock()
		defer trace.Unlock()

		threshold := uint64(3)

		// Call twice - second call should not publish
		collector.checkAndPublishQuorumForRoleByIndex(logger, trace, spectypes.BNRoleAttester, slot, vIndex1, threshold)
		collector.checkAndPublishQuorumForRoleByIndex(logger, trace, spectypes.BNRoleAttester, slot, vIndex1, threshold)

		// Verify only ONE publication
		calls := listener.GetCalls()
		require.Len(t, calls, 0, "second call should not publish (already published)")
	})
}
