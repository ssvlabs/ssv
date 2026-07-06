package store_test

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	store "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter/traces"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestSaveCommitteeDutyLinks(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)

	slot := phase0.Slot(123)
	links := map[phase0.ValidatorIndex]spectypes.CommitteeID{
		1: {1, 1, 1},
		2: {2, 2, 2},
		3: {3, 3, 3},
	}

	require.NoError(t, s.SaveCommitteeDutyLinks(slot, links))

	retrievedLinks, err := s.GetCommitteeDutyLinks(slot)
	require.NoError(t, err)
	require.Len(t, retrievedLinks, len(links))

	// convert slice to map for easier lookup
	retrievedMap := make(map[phase0.ValidatorIndex]spectypes.CommitteeID)
	for _, l := range retrievedLinks {
		retrievedMap[l.ValidatorIndex] = l.CommitteeID
	}

	for index, id := range links {
		retrievedID, ok := retrievedMap[index]
		require.True(t, ok, "link for validator index %d not found", index)
		assert.Equal(t, id, retrievedID)
	}
}

func TestSaveCommitteeDutyLink(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	store := store.New(db)

	cmdID := spectypes.CommitteeID{1, 2, 3}
	require.NoError(t, store.SaveCommitteeDutyLink(phase0.Slot(1), phase0.ValidatorIndex(39393), cmdID))

	gotID, err := store.GetCommitteeDutyLink(phase0.Slot(1), phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	assert.Equal(t, cmdID, gotID)

	cmdID2 := spectypes.CommitteeID{4, 5, 6}
	require.NoError(t, store.SaveCommitteeDutyLink(phase0.Slot(1), phase0.ValidatorIndex(39394), cmdID2))

	gotLinks, err := store.GetCommitteeDutyLinks(phase0.Slot(1))
	require.NoError(t, err)
	assert.Equal(t, cmdID, gotLinks[0].CommitteeID)
	assert.Equal(t, phase0.ValidatorIndex(39393), gotLinks[0].ValidatorIndex)
	assert.Equal(t, cmdID2, gotLinks[1].CommitteeID)
	assert.Equal(t, phase0.ValidatorIndex(39394), gotLinks[1].ValidatorIndex)
}

func TestSaveCommitteeDutyTrace(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeCTrace(1, 'a')
	trace2 := makeCTrace(2, 'b')

	store := store.New(db)
	require.NoError(t, store.SaveCommitteeDuty(spectypes.RoleCommittee, trace1))
	require.NoError(t, store.SaveCommitteeDuty(spectypes.RoleCommittee, trace2))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), spectypes.RoleCommittee, [32]byte{'a'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(trace1, duty))

	duty, err = store.GetCommitteeDuty(phase0.Slot(2), spectypes.RoleCommittee, [32]byte{'b'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(trace2, duty))
}

func TestSaveCommitteeDutyTrace_NoCollisionAcrossRunnerRoles(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	slot := phase0.Slot(5)
	committeeID := [32]byte{'x'}

	committeeDuty := makeCTrace(slot, 'x')
	committeeDuty.Role = spectypes.RoleCommittee
	committeeDuty.Attester = []*traces.SignerData{{Signer: 1}}

	aggCommitteeDuty := makeCTrace(slot, 'x')
	aggCommitteeDuty.Role = spectypes.RoleAggregatorCommittee
	aggCommitteeDuty.SyncCommittee = []*traces.SignerData{{Signer: 2}}

	require.NoError(t, s.SaveCommitteeDuty(spectypes.RoleCommittee, committeeDuty))
	require.NoError(t, s.SaveCommitteeDuty(spectypes.RoleAggregatorCommittee, aggCommitteeDuty))

	gotCommittee, err := s.GetCommitteeDuty(slot, spectypes.RoleCommittee, committeeID)
	require.NoError(t, err)
	require.Equal(t, spectypes.RoleCommittee, gotCommittee.Role)
	require.Len(t, gotCommittee.Attester, 1)
	require.Len(t, gotCommittee.SyncCommittee, 0)

	gotAggregatorCommittee, err := s.GetCommitteeDuty(slot, spectypes.RoleAggregatorCommittee, committeeID)
	require.NoError(t, err)
	require.Equal(t, spectypes.RoleAggregatorCommittee, gotAggregatorCommittee.Role)
	require.Len(t, gotAggregatorCommittee.Attester, 0)
	require.Len(t, gotAggregatorCommittee.SyncCommittee, 1)
}

func TestSaveCommitteeDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	cDuties := []*traces.CommitteeDutyTrace{makeCTrace(1, 'a'), makeCTrace(1, 'b')}

	store := store.New(db)
	require.NoError(t, store.SaveCommitteeDuties(phase0.Slot(1), spectypes.RoleCommittee, cDuties))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), spectypes.RoleCommittee, [32]byte{'a'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(cDuties[0], duty))

	duty, err = store.GetCommitteeDuty(phase0.Slot(1), spectypes.RoleCommittee, [32]byte{'b'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(cDuties[1], duty))

	duties, err := store.GetCommitteeDuties(phase0.Slot(1), spectypes.RoleCommittee)
	require.NoError(t, err)
	require.Len(t, duties, 2)
	require.True(t, committeeDutiesAreEqual(cDuties[0], duties[0]))
	require.True(t, committeeDutiesAreEqual(cDuties[1], duties[1]))
}

func TestSaveValidatorDutyTrace(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeVTrace(1)
	trace2 := makeVTrace(2)

	store := store.New(db)
	require.NoError(t, store.SaveValidatorDuty(trace1))
	require.NoError(t, store.SaveValidatorDuty(trace2))

	trace, err := store.GetValidatorDuty(phase0.Slot(1), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	assert.True(t, validatorDutiesAreEqual(trace1, trace))

	trace, err = store.GetValidatorDuty(phase0.Slot(2), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	assert.True(t, validatorDutiesAreEqual(trace2, trace))

	_, err = store.GetValidatorDuty(phase0.Slot(3), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.Error(t, err)

	traces, err := store.GetValidatorDuties(spectypes.BNRoleAttester, phase0.Slot(1))
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.True(t, validatorDutiesAreEqual(trace1, traces[0]))

	traces, err = store.GetValidatorDuties(spectypes.BNRoleAttester, phase0.Slot(2))
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.True(t, validatorDutiesAreEqual(trace2, traces[0]))
}

func TestSaveValidatorDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	trace1 := makeVTrace(1)
	trace2 := makeVTrace(2)

	store := store.New(db)
	require.NoError(t, store.SaveValidatorDuties([]*traces.ValidatorDutyTrace{trace1, trace2}))

	trace, err := store.GetValidatorDuty(phase0.Slot(1), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	assert.True(t, validatorDutiesAreEqual(trace1, trace))

	trace, err = store.GetValidatorDuty(phase0.Slot(2), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.NoError(t, err)
	assert.True(t, validatorDutiesAreEqual(trace2, trace))

	_, err = store.GetValidatorDuty(phase0.Slot(3), spectypes.BNRoleAttester, phase0.ValidatorIndex(39393))
	require.Error(t, err)

	traces, err := store.GetValidatorDuties(spectypes.BNRoleAttester, phase0.Slot(1))
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.True(t, validatorDutiesAreEqual(trace1, traces[0]))

	traces, err = store.GetValidatorDuties(spectypes.BNRoleAttester, phase0.Slot(2))
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.True(t, validatorDutiesAreEqual(trace2, traces[0]))
}

func TestSaveScheduledDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	slot := phase0.Slot(42)

	initial := map[phase0.ValidatorIndex]rolemask.Mask{
		1: rolemask.BitAttester | rolemask.BitProposer,
	}
	require.NoError(t, s.SaveScheduled(slot, initial))

	update := map[phase0.ValidatorIndex]rolemask.Mask{
		1: rolemask.BitAggregator,
		2: rolemask.BitSyncCommittee,
	}
	require.NoError(t, s.SaveScheduled(slot, update))

	sched, err := s.GetScheduled(slot)
	require.NoError(t, err)
	require.Len(t, sched, 2)
	assert.Equal(t, rolemask.BitAttester|rolemask.BitProposer|rolemask.BitAggregator, sched[1])
	assert.Equal(t, rolemask.BitSyncCommittee, sched[2])

	attesters, err := s.GetScheduledRole(slot, spectypes.BNRoleAttester)
	require.NoError(t, err)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{1}, attesters)

	syncers, err := s.GetScheduledRole(slot, spectypes.BNRoleSyncCommittee)
	require.NoError(t, err)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{2}, syncers)
}

func TestAddScheduledRole_UnionsIndices(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	slot := phase0.Slot(12)

	require.NoError(t, s.SetScheduledRole(slot, spectypes.BNRoleAttester, []phase0.ValidatorIndex{1, 3}))
	require.NoError(t, s.AddScheduledRole(slot, spectypes.BNRoleAttester, []phase0.ValidatorIndex{3, 4}))

	indices, err := s.GetScheduledRole(slot, spectypes.BNRoleAttester)
	require.NoError(t, err)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{1, 3, 4}, indices)
}

func TestSaveScheduledMergesExistingBitmaps(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	slot := phase0.Slot(21)

	require.NoError(t, s.SetScheduledRole(slot, spectypes.BNRoleAggregator, []phase0.ValidatorIndex{9}))

	schedule := map[phase0.ValidatorIndex]rolemask.Mask{
		5: rolemask.BitAggregator | rolemask.BitProposer,
	}
	require.NoError(t, s.SaveScheduled(slot, schedule))

	aggregators, err := s.GetScheduledRole(slot, spectypes.BNRoleAggregator)
	require.NoError(t, err)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{5, 9}, aggregators)

	proposers, err := s.GetScheduledRole(slot, spectypes.BNRoleProposer)
	require.NoError(t, err)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{5}, proposers)
}

func TestDeleteScheduledRoleAndSlot(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	slot := phase0.Slot(33)

	require.NoError(t, s.SetScheduledRole(slot, spectypes.BNRoleProposer, []phase0.ValidatorIndex{1, 2}))

	require.NoError(t, s.DeleteScheduledRole(slot, spectypes.BNRoleProposer))
	_, err = s.GetScheduledRole(slot, spectypes.BNRoleProposer)
	assert.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, s.SetScheduledRole(slot, spectypes.BNRoleAttester, []phase0.ValidatorIndex{1}))
	require.NoError(t, s.SetScheduledRole(slot, spectypes.BNRoleAggregator, []phase0.ValidatorIndex{2}))
	require.NoError(t, s.DeleteScheduledSlot(slot))
	_, err = s.GetScheduledRole(slot, spectypes.BNRoleAttester)
	assert.ErrorIs(t, err, store.ErrNotFound)
	require.NoError(t, s.DeleteScheduledSlot(slot))
}

func TestGetScheduledRoleMissing(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	s := store.New(db)
	_, err = s.GetScheduledRole(0, spectypes.BNRoleAttester)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func makeVTrace(slot phase0.Slot) *traces.ValidatorDutyTrace {
	return &traces.ValidatorDutyTrace{
		Slot:      slot,
		Role:      spectypes.BNRoleAttester,
		Validator: phase0.ValidatorIndex(39393),
	}
}

func makeCTrace(slot phase0.Slot, committee byte) *traces.CommitteeDutyTrace {
	return &traces.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: [32]byte{committee},
		OperatorIDs: nil,
	}
}

func partialSigTracesAreEqual(a []*traces.PartialSigTrace, b []*traces.PartialSigTrace) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	for i := range a {
		if !partialSigTraceAreEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func partialSigTraceAreEqual(a *traces.PartialSigTrace, b *traces.PartialSigTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if a.BeaconRoot != b.BeaconRoot {
		return false
	}
	if a.Signer != b.Signer {
		return false
	}

	return true
}

func qBFTTracesAreEqual(a []*traces.QBFTTrace, b []*traces.QBFTTrace) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	for i := range a {
		if !qBFTTraceAreEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func qBFTTraceAreEqual(a *traces.QBFTTrace, b *traces.QBFTTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Round != b.Round {
		return false
	}
	if a.BeaconRoot != b.BeaconRoot {
		return false
	}
	if a.Signer != b.Signer {
		return false
	}
	if a.ReceivedTime != b.ReceivedTime {
		return false
	}
	return true
}

func proposalTraceAreEqual(a *traces.ProposalTrace, b *traces.ProposalTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !qBFTTraceAreEqual(&a.QBFTTrace, &b.QBFTTrace) {
		return false
	}
	if !roundChangeTracesAreEqual(a.RoundChanges, b.RoundChanges) {
		return false
	}
	if !qBFTTracesAreEqual(a.PrepareMessages, b.PrepareMessages) {
		return false
	}
	return true
}

func roundChangeTracesAreEqual(a []*traces.RoundChangeTrace, b []*traces.RoundChangeTrace) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	for i := range a {
		if !roundChangeTraceAreEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func roundChangeTraceAreEqual(a *traces.RoundChangeTrace, b *traces.RoundChangeTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !qBFTTraceAreEqual(&a.QBFTTrace, &b.QBFTTrace) {
		return false
	}
	if a.PreparedRound != b.PreparedRound {
		return false
	}
	if !qBFTTracesAreEqual(a.PrepareMessages, b.PrepareMessages) {
		return false
	}
	return true
}

func roundTracesAreEqual(a []*traces.RoundTrace, b []*traces.RoundTrace) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	for i := range a {
		if !roundTraceAreEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func roundTraceAreEqual(a *traces.RoundTrace, b *traces.RoundTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Proposer != b.Proposer {
		return false
	}
	if !proposalTraceAreEqual(a.ProposalTrace, b.ProposalTrace) {
		return false
	}
	if !qBFTTracesAreEqual(a.Prepares, b.Prepares) {
		return false
	}
	if !qBFTTracesAreEqual(a.Commits, b.Commits) {
		return false
	}
	if !roundChangeTracesAreEqual(a.RoundChanges, b.RoundChanges) {
		return false
	}
	return true
}

func decidedTracesAreEqual(a []*traces.DecidedTrace, b []*traces.DecidedTrace) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	for i := range a {
		if !decidedTraceAreEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func decidedTraceAreEqual(a *traces.DecidedTrace, b *traces.DecidedTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Round != b.Round {
		return false
	}
	if a.BeaconRoot != b.BeaconRoot {
		return false
	}
	if len(a.Signers) != len(b.Signers) {
		return false
	}
	for i := range a.Signers {
		if a.Signers[i] != b.Signers[i] {
			return false
		}
	}
	return true
}

func consensusTracesAreEqual(a *traces.ConsensusTrace, b *traces.ConsensusTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !roundTracesAreEqual(a.Rounds, b.Rounds) {
		return false
	}
	if !decidedTracesAreEqual(a.Decideds, b.Decideds) {
		return false
	}
	return true
}

func validatorDutiesAreEqual(a *traces.ValidatorDutyTrace, b *traces.ValidatorDutyTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Slot != b.Slot {
		return false
	}
	if a.Role != b.Role {
		return false
	}
	if a.Validator != b.Validator {
		return false
	}
	if !roundTracesAreEqual(a.Rounds, b.Rounds) {
		return false
	}
	if !decidedTracesAreEqual(a.Decideds, b.Decideds) {
		return false
	}
	if !consensusTracesAreEqual(&a.ConsensusTrace, &b.ConsensusTrace) {
		return false
	}
	if !partialSigTracesAreEqual(a.Pre, b.Pre) {
		return false
	}
	if !partialSigTracesAreEqual(a.Post, b.Post) {
		return false
	}

	return true
}

func committeeDutiesAreEqual(a, b *traces.CommitteeDutyTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if !compareConsensusTrace(&a.ConsensusTrace, &b.ConsensusTrace) {
		return false
	}

	if a.Slot != b.Slot {
		return false
	}

	if a.CommitteeID != b.CommitteeID {
		return false
	}

	if !compareOperatorIDSlices(a.OperatorIDs, b.OperatorIDs) {
		return false
	}

	if !compareSignerDataSlices(a.SyncCommittee, b.SyncCommittee) {
		return false
	}

	if !compareSignerDataSlices(a.Attester, b.Attester) {
		return false
	}

	return true
}

func compareConsensusTrace(a, b *traces.ConsensusTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if !compareRoundTraceSlices(a.Rounds, b.Rounds) {
		return false
	}
	if !compareDecidedTraceSlices(a.Decideds, b.Decideds) {
		return false
	}
	return true
}

func compareRoundTraceSlices(a, b []*traces.RoundTrace) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareRoundTrace(a[i], b[i]) {
			return false
		}
	}
	return true
}

func compareRoundTrace(a, b *traces.RoundTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Proposer != b.Proposer {
		return false
	}
	if !compareProposalTrace(a.ProposalTrace, b.ProposalTrace) {
		return false
	}
	if !compareQBFTTraceSlices(a.Prepares, b.Prepares) {
		return false
	}
	if !compareQBFTTraceSlices(a.Commits, b.Commits) {
		return false
	}
	if !compareRoundChangeTraceSlices(a.RoundChanges, b.RoundChanges) {
		return false
	}
	return true
}

func compareProposalTrace(a, b *traces.ProposalTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !compareQBFTTrace(&a.QBFTTrace, &b.QBFTTrace) {
		return false
	}
	if !compareRoundChangeTraceSlices(a.RoundChanges, b.RoundChanges) {
		return false
	}
	if !compareQBFTTraceSlices(a.PrepareMessages, b.PrepareMessages) {
		return false
	}
	return true
}

func compareRoundChangeTraceSlices(a, b []*traces.RoundChangeTrace) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareRoundChangeTrace(a[i], b[i]) {
			return false
		}
	}
	return true
}
func compareRoundChangeTrace(a, b *traces.RoundChangeTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !compareQBFTTrace(&a.QBFTTrace, &b.QBFTTrace) {
		return false
	}
	if a.PreparedRound != b.PreparedRound {
		return false
	}
	if !compareQBFTTraceSlices(a.PrepareMessages, b.PrepareMessages) {
		return false
	}
	return true
}

func compareDecidedTraceSlices(a, b []*traces.DecidedTrace) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareDecidedTrace(a[i], b[i]) {
			return false
		}
	}
	return true
}

func compareDecidedTrace(a, b *traces.DecidedTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Round != b.Round {
		return false
	}
	if a.BeaconRoot != b.BeaconRoot {
		return false
	}
	if !compareOperatorIDSlices(a.Signers, b.Signers) {
		return false
	}
	if a.ReceivedTime != b.ReceivedTime {
		return false
	}
	return true
}

func compareQBFTTraceSlices(a, b []*traces.QBFTTrace) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareQBFTTrace(a[i], b[i]) {
			return false
		}
	}
	return true
}

func compareQBFTTrace(a, b *traces.QBFTTrace) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Round != b.Round {
		return false
	}
	if a.BeaconRoot != b.BeaconRoot {
		return false
	}
	if a.Signer != b.Signer {
		return false
	}
	if a.ReceivedTime != b.ReceivedTime {
		return false
	}
	return true
}

func compareSignerDataSlices(a, b []*traces.SignerData) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true // empty slices are equal
	}
	if len(a) == 0 || len(b) == 0 {
		return false // one is empty and the other is not
	}

	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareSignerData(a[i], b[i]) {
			return false
		}
	}
	return true
}

func compareSignerData(a, b *traces.SignerData) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Signer != b.Signer {
		return false
	}
	if !compareValidatorIndexSlices(a.ValidatorIdx, b.ValidatorIdx) {
		return false
	}
	if a.ReceivedTime != b.ReceivedTime {
		return false
	}
	return true
}

func compareValidatorIndexSlices(a, b []phase0.ValidatorIndex) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compareOperatorIDSlices(a, b []spectypes.OperatorID) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		return true // empty slices are equal
	}
	if len(a) == 0 || len(b) == 0 {
		return false // one is empty and the other is not
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
