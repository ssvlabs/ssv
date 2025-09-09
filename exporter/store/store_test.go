package store_test

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	store "github.com/ssvlabs/ssv/exporter/store"
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
	require.NoError(t, store.SaveCommitteeDuty(trace1))
	require.NoError(t, store.SaveCommitteeDuty(trace2))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'a'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(trace1, duty))

	duty, err = store.GetCommitteeDuty(phase0.Slot(2), [32]byte{'b'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(trace2, duty))
}

func TestSaveCommitteeDuties(t *testing.T) {
	logger := zap.NewNop()
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	traces := []*exporter.CommitteeDutyTrace{makeCTrace(1, 'a'), makeCTrace(1, 'b')}

	store := store.New(db)
	require.NoError(t, store.SaveCommitteeDuties(phase0.Slot(1), traces))

	duty, err := store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'a'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(traces[0], duty))

	duty, err = store.GetCommitteeDuty(phase0.Slot(1), [32]byte{'b'})
	require.NoError(t, err)
	assert.True(t, committeeDutiesAreEqual(traces[1], duty))

	duties, err := store.GetCommitteeDuties(phase0.Slot(1))
	require.NoError(t, err)
	require.Len(t, duties, 2)
	require.True(t, committeeDutiesAreEqual(traces[0], duties[0]))
	require.True(t, committeeDutiesAreEqual(traces[1], duties[1]))
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
	require.NoError(t, store.SaveValidatorDuties([]*exporter.ValidatorDutyTrace{trace1, trace2}))

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

func makeVTrace(slot phase0.Slot) *exporter.ValidatorDutyTrace {
	return &exporter.ValidatorDutyTrace{
		Slot:      slot,
		Role:      spectypes.BNRoleAttester,
		Validator: phase0.ValidatorIndex(39393),
	}
}

func makeCTrace(slot phase0.Slot, committee byte) *exporter.CommitteeDutyTrace {
	return &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: [32]byte{committee},
		OperatorIDs: nil,
	}
}

func partialSigTracesAreEqual(a []*exporter.PartialSigTrace, b []*exporter.PartialSigTrace) bool {
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

func partialSigTraceAreEqual(a *exporter.PartialSigTrace, b *exporter.PartialSigTrace) bool {
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

func qBFTTracesAreEqual(a []*exporter.QBFTTrace, b []*exporter.QBFTTrace) bool {
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

func qBFTTraceAreEqual(a *exporter.QBFTTrace, b *exporter.QBFTTrace) bool {
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

func proposalTraceAreEqual(a *exporter.ProposalTrace, b *exporter.ProposalTrace) bool {
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

func roundChangeTracesAreEqual(a []*exporter.RoundChangeTrace, b []*exporter.RoundChangeTrace) bool {
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

func roundChangeTraceAreEqual(a *exporter.RoundChangeTrace, b *exporter.RoundChangeTrace) bool {
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

func roundTracesAreEqual(a []*exporter.RoundTrace, b []*exporter.RoundTrace) bool {
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

func roundTraceAreEqual(a *exporter.RoundTrace, b *exporter.RoundTrace) bool {
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

func decidedTracesAreEqual(a []*exporter.DecidedTrace, b []*exporter.DecidedTrace) bool {
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

func decidedTraceAreEqual(a *exporter.DecidedTrace, b *exporter.DecidedTrace) bool {
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

func consensusTracesAreEqual(a *exporter.ConsensusTrace, b *exporter.ConsensusTrace) bool {
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

func validatorDutiesAreEqual(a *exporter.ValidatorDutyTrace, b *exporter.ValidatorDutyTrace) bool {
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

func committeeDutiesAreEqual(a, b *exporter.CommitteeDutyTrace) bool {
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

func compareConsensusTrace(a, b *exporter.ConsensusTrace) bool {
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

func compareRoundTraceSlices(a, b []*exporter.RoundTrace) bool {
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

func compareRoundTrace(a, b *exporter.RoundTrace) bool {
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

func compareProposalTrace(a, b *exporter.ProposalTrace) bool {
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

func compareRoundChangeTraceSlices(a, b []*exporter.RoundChangeTrace) bool {
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
func compareRoundChangeTrace(a, b *exporter.RoundChangeTrace) bool {
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

func compareDecidedTraceSlices(a, b []*exporter.DecidedTrace) bool {
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

func compareDecidedTrace(a, b *exporter.DecidedTrace) bool {
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

func compareQBFTTraceSlices(a, b []*exporter.QBFTTrace) bool {
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

func compareQBFTTrace(a, b *exporter.QBFTTrace) bool {
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

func compareSignerDataSlices(a, b []*exporter.SignerData) bool {
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

func compareSignerData(a, b *exporter.SignerData) bool {
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
