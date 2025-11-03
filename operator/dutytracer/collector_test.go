package validator

import (
    "testing"
    "time"

    "github.com/attestantio/go-eth2-client/spec/phase0"
    "go.uber.org/mock/gomock"
    "go.uber.org/zap"

    spectypes "github.com/ssvlabs/ssv-spec/types"

    "github.com/ssvlabs/ssv/exporter"
    "github.com/ssvlabs/ssv/networkconfig"
    storage "github.com/ssvlabs/ssv/registry/storage"
    storagemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
    ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// newTestCollector builds a Collector with a mocked ValidatorStore and a capture hook for decided events.
func newTestCollector(t *testing.T, vs *storagemocks.MockValidatorStore, onDecided func(DecidedInfo)) *Collector {
    t.Helper()
    b := &networkconfig.Beacon{
        Name:                         "testnet",
        SlotDuration:                 time.Second,
        SlotsPerEpoch:                32,
        EpochsPerSyncCommitteePeriod: 256,
        GenesisTime:                  time.Unix(0, 0),
    }
    logger := zap.NewNop()
    // client (DomainDataProvider) and store are not needed for quorum tests.
    return New(logger, vs, nil, nil, b, onDecided, nil)
}

func TestCollector_CheckAndPublishQuorum_RootAware_SyncOnly(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    vs := storagemocks.NewMockValidatorStore(ctrl)

    // Committee with 4 operators -> threshold = 3
    committeeID := spectypes.CommitteeID{}
    operators := []spectypes.OperatorID{1, 2, 3, 4}
    vs.EXPECT().Committee(committeeID).AnyTimes().Return(&storage.Committee{ID: committeeID, Operators: operators}, true)

    // Validator exists by index
    vIdx := phase0.ValidatorIndex(123)
    vs.EXPECT().ValidatorByIndex(vIdx).AnyTimes().Return(&ssvtypes.SSVShare{}, true)

    var published []DecidedInfo
    c := newTestCollector(t, vs, func(msg DecidedInfo) { published = append(published, msg) })

    // Prepare committee trace with role roots ready
    trace := &committeeDutyTrace{
        CommitteeDutyTrace: exporter.CommitteeDutyTrace{},
        roleRootsReady:     true,
    }
    // Define distinct roots
    scRoot := phase0.Root{1}
    attRoot := phase0.Root{2}
    trace.syncCommitteeRoot = scRoot
    trace.attestationRoot = attRoot

    // Provide signer data for SyncCommittee only (3 unique signers including vIdx)
    trace.SyncCommittee = []*exporter.SignerData{
        {Signer: 1, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 2, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 3, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
    }

    // Build a post‑consensus partial signature message for sync‑committee root
    p := &spectypes.PartialSignatureMessages{
        Type: spectypes.PostConsensusPartialSig,
        Slot: 42,
        Messages: []*spectypes.PartialSignatureMessage{{
            ValidatorIndex: vIdx,
            Signer:         1,
            SigningRoot:    scRoot,
        }},
    }

    // Call under test
    trace.Lock()
    c.checkAndPublishQuorum(zap.NewNop(), p, committeeID, trace)
    trace.Unlock()

    if len(published) != 1 {
        t.Fatalf("expected 1 decided publish, got %d", len(published))
    }
    got := published[0]
    if got.Role != spectypes.BNRoleSyncCommittee {
        t.Fatalf("expected role SyncCommittee, got %v", got.Role)
    }
    if got.Index != vIdx || got.Slot != p.Slot {
        t.Fatalf("unexpected decided payload: %+v", got)
    }
}

func TestCollector_CheckAndPublishQuorum_RootAware_AttesterOnly(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    vs := storagemocks.NewMockValidatorStore(ctrl)

    committeeID := spectypes.CommitteeID{}
    operators := []spectypes.OperatorID{10, 11, 12, 13}
    vs.EXPECT().Committee(committeeID).AnyTimes().Return(&storage.Committee{ID: committeeID, Operators: operators}, true)

    vIdx := phase0.ValidatorIndex(777)
    vs.EXPECT().ValidatorByIndex(vIdx).AnyTimes().Return(&ssvtypes.SSVShare{}, true)

    var published []DecidedInfo
    c := newTestCollector(t, vs, func(msg DecidedInfo) { published = append(published, msg) })

    trace := &committeeDutyTrace{CommitteeDutyTrace: exporter.CommitteeDutyTrace{}, roleRootsReady: true}
    scRoot := phase0.Root{1}
    attRoot := phase0.Root{9}
    trace.syncCommitteeRoot = scRoot
    trace.attestationRoot = attRoot

    trace.Attester = []*exporter.SignerData{
        {Signer: 10, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 11, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 12, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
    }

    p := &spectypes.PartialSignatureMessages{
        Type: spectypes.PostConsensusPartialSig,
        Slot: 64,
        Messages: []*spectypes.PartialSignatureMessage{{
            ValidatorIndex: vIdx,
            Signer:         10,
            SigningRoot:    attRoot,
        }},
    }

    trace.Lock()
    c.checkAndPublishQuorum(zap.NewNop(), p, committeeID, trace)
    trace.Unlock()

    if len(published) != 1 {
        t.Fatalf("expected 1 decided publish, got %d", len(published))
    }
    if published[0].Role != spectypes.BNRoleAttester {
        t.Fatalf("expected role Attester, got %v", published[0].Role)
    }
}

func TestCollector_CheckAndPublishQuorum_RootsNotReady_NoPublish(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()
    vs := storagemocks.NewMockValidatorStore(ctrl)

    committeeID := spectypes.CommitteeID{}
    operators := []spectypes.OperatorID{1, 2, 3, 4}
    vs.EXPECT().Committee(committeeID).AnyTimes().Return(&storage.Committee{ID: committeeID, Operators: operators}, true)

    vIdx := phase0.ValidatorIndex(5)
    vs.EXPECT().ValidatorByIndex(vIdx).AnyTimes().Return(&ssvtypes.SSVShare{}, true)

    var published []DecidedInfo
    c := newTestCollector(t, vs, func(msg DecidedInfo) { published = append(published, msg) })

    trace := &committeeDutyTrace{CommitteeDutyTrace: exporter.CommitteeDutyTrace{}, roleRootsReady: false}
    scRoot := phase0.Root{7}
    // Provide signer data enough for quorum, but roots not ready means no publish yet
    trace.SyncCommittee = []*exporter.SignerData{
        {Signer: 1, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 2, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
        {Signer: 3, ValidatorIdx: []phase0.ValidatorIndex{vIdx}},
    }

    p := &spectypes.PartialSignatureMessages{
        Type: spectypes.PostConsensusPartialSig,
        Slot: 99,
        Messages: []*spectypes.PartialSignatureMessage{{
            ValidatorIndex: vIdx,
            Signer:         1,
            SigningRoot:    scRoot,
        }},
    }

    trace.Lock()
    c.checkAndPublishQuorum(zap.NewNop(), p, committeeID, trace)
    trace.Unlock()

    if len(published) != 0 {
        t.Fatalf("expected no publishes when roots not ready, got %d", len(published))
    }
}
