package controller

import (
	"bytes"
	"context"
	"crypto/rsa"
	"errors"
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// TestController_IdentifierAtHeight verifies that the per-height identifier resolves to the
// fork-correct SSV domain. Pre-Boole heights must yield the config's DomainType (Alan) and
// post-Boole heights must yield its NextDomainType (Boole).
//
// The test also confirms the regression: without IdentifierFn the controller uses the static
// Identifier frozen at construction time, causing post-fork heights to still carry the pre-fork
// domain.
func TestController_IdentifierAtHeight(t *testing.T) {
	ks := spectestingutils.Testing4SharesSet()
	member := spectestingutils.TestingCommitteeMember(ks)
	role := spectypes.RoleCommittee

	// Build a post-Boole config: Boole active from genesis (epoch 0).
	postBooleSSV := *networkconfig.TestNetwork.SSV
	postBooleSSV.Forks = networkconfig.SSVForks{Boole: 0}
	postBooleCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &postBooleSSV}

	// Build a pre-Boole config explicitly: Boole disabled (epoch MaxUint64). Built from an
	// explicit copy rather than aliasing the package-global TestNetwork, whose Boole epoch can
	// be flipped to 0 by SSV_TEST_BOOLE_FORK=post (see networkconfig/test-network.go init).
	preBooleSSV := *networkconfig.TestNetwork.SSV
	preBooleSSV.Forks = networkconfig.SSVForks{Boole: phase0.Epoch(math.MaxUint64)}
	preBooleCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &preBooleSSV}

	committeeID := member.CommitteeID

	t.Run("post-Boole height yields Boole domain", func(t *testing.T) {
		identifierFn := func(height specqbft.Height) []byte {
			domain := postBooleCfg.DomainTypeAtSlot(phase0.Slot(height))
			id := spectypes.NewMsgID(domain, committeeID[:], role)
			return id[:]
		}

		// height 1 is slot 1, which in an epoch-0 Boole fork is already post-fork.
		postBooleHeight := specqbft.Height(1)
		identifier := identifierFn(specqbft.FirstHeight)

		ctrl := NewController(identifier, member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn

		resolvedID := ctrl.identifierAtHeight(postBooleHeight)
		msgID := spectypes.MessageID(resolvedID[0:56])

		expectedDomain := postBooleCfg.DomainTypeAtSlot(phase0.Slot(postBooleHeight))
		require.Equal(t, expectedDomain[:], msgID.GetDomain(),
			"post-Boole height must use NextDomainType (Boole)")
		require.Equal(t, postBooleCfg.NextDomainType[:], msgID.GetDomain(),
			"post-Boole domain must equal cfg.NextDomainType")
	})

	t.Run("pre-Boole height yields Alan domain", func(t *testing.T) {
		identifierFn := func(height specqbft.Height) []byte {
			domain := preBooleCfg.DomainTypeAtSlot(phase0.Slot(height))
			id := spectypes.NewMsgID(domain, committeeID[:], role)
			return id[:]
		}

		// With Boole at MaxUint64, any normal slot is pre-fork.
		preForkHeight := specqbft.Height(1000)
		identifier := identifierFn(specqbft.FirstHeight)

		ctrl := NewController(identifier, member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn

		resolvedID := ctrl.identifierAtHeight(preForkHeight)
		msgID := spectypes.MessageID(resolvedID[0:56])

		expectedDomain := preBooleCfg.DomainTypeAtSlot(phase0.Slot(preForkHeight))
		require.Equal(t, expectedDomain[:], msgID.GetDomain(),
			"pre-Boole height must use DomainType (Alan)")
		require.Equal(t, preBooleCfg.DomainType[:], msgID.GetDomain(),
			"pre-Boole domain must equal cfg.DomainType")
	})

	t.Run("fork-spanning controller switches domain at fork boundary", func(t *testing.T) {
		// Build a config where Boole activates at epoch 10.
		forkEpoch := phase0.Epoch(10)
		midBooleSSV := *networkconfig.TestNetwork.SSV
		midBooleSSV.Forks = networkconfig.SSVForks{Boole: forkEpoch}
		midBooleCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &midBooleSSV}

		identifierFn := func(height specqbft.Height) []byte {
			domain := midBooleCfg.DomainTypeAtSlot(phase0.Slot(height))
			id := spectypes.NewMsgID(domain, committeeID[:], role)
			return id[:]
		}
		identifier := identifierFn(specqbft.FirstHeight)

		ctrl := NewController(identifier, member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn

		slotsPerEpoch := phase0.Slot(networkconfig.TestNetwork.SlotsPerEpoch)

		preForkSlot := phase0.Slot(forkEpoch)*slotsPerEpoch - 1
		postForkSlot := phase0.Slot(forkEpoch) * slotsPerEpoch

		preForkID := spectypes.MessageID(ctrl.identifierAtHeight(specqbft.Height(preForkSlot))[0:56])
		postForkID := spectypes.MessageID(ctrl.identifierAtHeight(specqbft.Height(postForkSlot))[0:56])

		require.Equal(t, midBooleCfg.DomainType[:], preForkID.GetDomain(),
			"slot before fork must carry Alan domain")
		require.Equal(t, midBooleCfg.NextDomainType[:], postForkID.GetDomain(),
			"slot at/after fork must carry Boole domain")
	})

	t.Run("nil IdentifierFn falls back to static Identifier (regression guard)", func(t *testing.T) {
		// Build a static identifier pinned to Alan domain.
		staticID := spectypes.NewMsgID(preBooleCfg.DomainType, committeeID[:], role)
		ctrl := NewController(staticID[:], member, nil, nil, false)
		// IdentifierFn intentionally left nil.

		// Even for a post-Boole height, the fallback returns the static (Alan) identifier.
		// This is the bug the fix addresses — demonstrated by checking that the domain does NOT
		// change across heights when IdentifierFn is absent.
		postBooleHeight := specqbft.Height(1)
		resolvedID := spectypes.MessageID(ctrl.identifierAtHeight(postBooleHeight)[0:56])
		require.Equal(t, preBooleCfg.DomainType[:], resolvedID.GetDomain(),
			"without IdentifierFn, domain must be frozen at construction-time value (Alan)")
	})

	t.Run("unpatched controller (no IdentifierFn) fails post-Boole domain check", func(t *testing.T) {
		// Demonstrate the regression: a controller WITHOUT IdentifierFn carries Alan domain at
		// post-Boole heights. The Boole domain would NOT match, confirming the bug.
		staticID := spectypes.NewMsgID(preBooleCfg.DomainType, committeeID[:], role)
		ctrl := NewController(staticID[:], member, nil, nil, false)

		postBooleHeight := specqbft.Height(1)
		resolvedID := spectypes.MessageID(ctrl.identifierAtHeight(postBooleHeight)[0:56])

		expectedBooleDomain := postBooleCfg.DomainTypeAtSlot(phase0.Slot(postBooleHeight))
		require.NotEqual(t, expectedBooleDomain[:], resolvedID.GetDomain(),
			"unpatched controller must NOT produce Boole domain (proves the bug exists without fix)")
	})
}

// makeSignedQBFTMsg builds a minimal SignedSSVMessage carrying a RoundChange QBFT message
// with the given identifier and height. It does not require a running instance; the
// signature is produced with operator 1's RSA key from the supplied key set.
func makeSignedQBFTMsg(ks *spectestingutils.TestKeySet, identifier []byte, height specqbft.Height) *spectypes.SignedSSVMessage {
	qbftMsg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     height,
		Round:      specqbft.FirstRound,
		Identifier: identifier,
	}
	return spectestingutils.SignQBFTMsg(ks.OperatorKeys[1], 1, qbftMsg)
}

// TestController_ProcessMsg_ForkDomainCheck verifies that the ProcessMsg receive path
// enforces per-height domain resolution via identifierAtHeight, not the static Identifier.
func TestController_ProcessMsg_ForkDomainCheck(t *testing.T) {
	ks := spectestingutils.Testing4SharesSet()
	member := spectestingutils.TestingCommitteeMember(ks)
	role := spectypes.RoleCommittee
	committeeID := member.CommitteeID

	// Config where Boole activates at epoch 10.
	forkEpoch := phase0.Epoch(10)
	midBooleSSV := *networkconfig.TestNetwork.SSV
	midBooleSSV.Forks = networkconfig.SSVForks{Boole: forkEpoch}
	midBooleCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &midBooleSSV}

	slotsPerEpoch := phase0.Slot(networkconfig.TestNetwork.SlotsPerEpoch)
	preForkSlot := phase0.Slot(forkEpoch)*slotsPerEpoch - 1
	postForkSlot := phase0.Slot(forkEpoch) * slotsPerEpoch

	preForkHeight := specqbft.Height(preForkSlot)
	postForkHeight := specqbft.Height(postForkSlot)

	alanDomain := midBooleCfg.DomainType
	booleDomain := midBooleCfg.NextDomainType

	identifierFn := func(height specqbft.Height) []byte {
		domain := midBooleCfg.DomainTypeAtSlot(phase0.Slot(height))
		id := spectypes.NewMsgID(domain, committeeID[:], role)
		return id[:]
	}

	// Static identifier frozen at pre-fork domain (simulates old code path).
	staticID := spectypes.NewMsgID(alanDomain, committeeID[:], role)

	logger := zap.NewNop()

	// t.Run helper: build a controller pre-advanced to a height so a message at that
	// height is not classified as a future message (avoids FutureMessageErrorCode).
	advanceTo := func(ctrl *Controller, height specqbft.Height) {
		ctrl.LatestInstanceHeight = height
	}

	// isIdentifierMismatch returns true if err is the specific identifier-invalid SSV error.
	isIdentifierMismatch := func(err error) bool {
		var ssverr *spectypes.Error
		return err != nil && errors.As(err, &ssverr) &&
			ssverr.Code == spectypes.MessageIdentifierInvalidErrorCode
	}

	t.Run("receive path accepts message with matching post-fork (Boole) domain", func(t *testing.T) {
		ctrl := NewController(staticID[:], member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn
		advanceTo(ctrl, postForkHeight)

		booleID := spectypes.NewMsgID(booleDomain, committeeID[:], role)
		signedMsg := makeSignedQBFTMsg(ks, booleID[:], postForkHeight)

		_, err := ctrl.ProcessMsg(context.Background(), logger, signedMsg, nil)
		// The identifier check passes; subsequent failure (instance not found, etc.) is expected.
		// The one thing that must NOT happen is an identifier mismatch.
		require.False(t, isIdentifierMismatch(err),
			"post-fork message with correct Boole domain must pass the identifier check in ProcessMsg")
	})

	t.Run("receive path accepts message with matching pre-fork (Alan) domain", func(t *testing.T) {
		ctrl := NewController(staticID[:], member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn
		advanceTo(ctrl, preForkHeight)

		alanID := spectypes.NewMsgID(alanDomain, committeeID[:], role)
		signedMsg := makeSignedQBFTMsg(ks, alanID[:], preForkHeight)

		_, err := ctrl.ProcessMsg(context.Background(), logger, signedMsg, nil)
		require.False(t, isIdentifierMismatch(err),
			"pre-fork message with correct Alan domain must pass the identifier check in ProcessMsg")
	})

	t.Run("receive path rejects post-fork message carrying stale pre-fork (Alan) domain", func(t *testing.T) {
		// Controller has IdentifierFn, so post-fork height resolves to Boole domain.
		// A message carrying Alan domain at that height must be rejected.
		ctrl := NewController(staticID[:], member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn
		advanceTo(ctrl, postForkHeight)

		staleAlanID := spectypes.NewMsgID(alanDomain, committeeID[:], role)
		signedMsg := makeSignedQBFTMsg(ks, staleAlanID[:], postForkHeight)

		_, err := ctrl.ProcessMsg(context.Background(), logger, signedMsg, nil)
		require.Error(t, err, "post-fork message with Alan domain must be rejected")
		var ssverr *spectypes.Error
		require.ErrorAs(t, err, &ssverr)
		require.Equal(t, spectypes.MessageIdentifierInvalidErrorCode, ssverr.Code,
			"rejection must be an identifier mismatch error, not a different failure")
	})

	t.Run("receive path rejects pre-fork message carrying wrong (Boole) domain", func(t *testing.T) {
		// Controller has IdentifierFn, so pre-fork height resolves to Alan domain.
		// A message carrying Boole domain at that height must be rejected.
		ctrl := NewController(staticID[:], member, nil, nil, false)
		ctrl.IdentifierFn = identifierFn
		advanceTo(ctrl, preForkHeight)

		wrongBooleID := spectypes.NewMsgID(booleDomain, committeeID[:], role)
		signedMsg := makeSignedQBFTMsg(ks, wrongBooleID[:], preForkHeight)

		_, err := ctrl.ProcessMsg(context.Background(), logger, signedMsg, nil)
		require.Error(t, err, "pre-fork message with Boole domain must be rejected")
		var ssverr *spectypes.Error
		require.ErrorAs(t, err, &ssverr)
		require.Equal(t, spectypes.MessageIdentifierInvalidErrorCode, ssverr.Code,
			"rejection must be an identifier mismatch error")
	})
}

// TestController_NilIdentifierFn_ByteIdenticalToStatic confirms that a controller without
// IdentifierFn returns a byte-for-byte identical slice to the static Identifier field,
// regardless of height. This is the nil-fn contract: no IdentifierFn == frozen domain.
func TestController_NilIdentifierFn_ByteIdenticalToStatic(t *testing.T) {
	ks := spectestingutils.Testing4SharesSet()
	member := spectestingutils.TestingCommitteeMember(ks)
	role := spectypes.RoleCommittee
	committeeID := member.CommitteeID

	preBooleCfg := networkconfig.TestNetwork // Boole at MaxUint64
	staticID := spectypes.NewMsgID(preBooleCfg.DomainType, committeeID[:], role)

	ctrl := NewController(staticID[:], member, nil, nil, false)
	// IdentifierFn intentionally left nil.

	for _, height := range []specqbft.Height{0, 1, 100, 10000, specqbft.Height(^uint64(0) >> 1)} {
		got := ctrl.identifierAtHeight(height)
		require.True(t, bytes.Equal(staticID[:], got),
			"nil-fn controller must return byte-identical static Identifier at height %d", height)
	}
}

// okValueChecker accepts any value. Defined locally (rather than reusing
// protocol/v2/testing.TestingValueChecker) to avoid an import cycle: protocol/v2/testing
// imports this package.
type okValueChecker struct{}

func (okValueChecker) CheckValue([]byte) error { return nil }

// forkTestHarness bundles the fixtures shared by the send-path fork tests: a config with
// Boole activating at epoch 10, the fork-boundary heights, a fork-aware identifier function
// and a static identifier frozen at the pre-fork (Alan) domain.
type forkTestHarness struct {
	ks             *spectestingutils.TestKeySet
	member         *spectypes.CommitteeMember
	cfg            *networkconfig.Network
	preForkHeight  specqbft.Height
	postForkHeight specqbft.Height
	identifierFn   func(specqbft.Height) []byte
	staticID       spectypes.MessageID
	roundTimerF    ssv.QBFTRoundTimerF
}

func newForkTestHarness() *forkTestHarness {
	ks := spectestingutils.Testing4SharesSet()
	member := spectestingutils.TestingCommitteeMember(ks)
	role := spectypes.RoleCommittee
	committeeID := member.CommitteeID

	forkEpoch := phase0.Epoch(10)
	midBooleSSV := *networkconfig.TestNetwork.SSV
	midBooleSSV.Forks = networkconfig.SSVForks{Boole: forkEpoch}
	midBooleCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &midBooleSSV}

	slotsPerEpoch := phase0.Slot(networkconfig.TestNetwork.SlotsPerEpoch)

	identifierFn := func(height specqbft.Height) []byte {
		domain := midBooleCfg.DomainTypeAtSlot(phase0.Slot(height))
		id := spectypes.NewMsgID(domain, committeeID[:], role)
		return id[:]
	}

	return &forkTestHarness{
		ks:             ks,
		member:         member,
		cfg:            midBooleCfg,
		preForkHeight:  specqbft.Height(phase0.Slot(forkEpoch)*slotsPerEpoch - 1),
		postForkHeight: specqbft.Height(phase0.Slot(forkEpoch) * slotsPerEpoch),
		identifierFn:   identifierFn,
		staticID:       spectypes.NewMsgID(midBooleCfg.DomainType, committeeID[:], role),
		roundTimerF: func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
	}
}

func (h *forkTestHarness) newController() *Controller {
	cfg := &qbft.Config{
		// Some other operator is the round-1 proposer so Start doesn't attempt to create and
		// broadcast a proposal — irrelevant to the identifier under test.
		ProposerF: func(*specqbft.State, specqbft.Round) spectypes.OperatorID {
			return 2
		},
		CutOffRound: spectestingutils.TestingCutOffRound,
	}
	ctrl := NewController(h.staticID[:], h.member, cfg, nil, false)
	ctrl.IdentifierFn = h.identifierFn
	return ctrl
}

// instanceDomain extracts the SSV domain from an instance's State.ID.
func instanceDomain(t *testing.T, id []byte) []byte {
	t.Helper()
	require.Len(t, id, 56)
	return spectypes.MessageID(id).GetDomain()
}

// TestController_StartNewInstance_ForkDomain drives the real send path: StartNewInstance must
// create instances whose State.ID carries the fork-correct domain, even though the controller's
// static Identifier stays frozen at the pre-fork (Alan) domain. This guards the exact regression
// this fix addresses — reverting StartNewInstance to c.Identifier fails these assertions.
func TestController_StartNewInstance_ForkDomain(t *testing.T) {
	h := newForkTestHarness()
	logger := zap.NewNop()

	t.Run("post-fork height starts instance with Boole domain", func(t *testing.T) {
		ctrl := h.newController()

		inst, err := ctrl.StartNewInstance(context.Background(), logger, h.postForkHeight,
			spectestingutils.TestingQBFTFullData, okValueChecker{}, h.roundTimerF)
		require.NoError(t, err)

		domain := instanceDomain(t, inst.State.ID)
		require.Equal(t, h.cfg.NextDomainType[:], domain,
			"instance started at post-fork height must carry the Boole domain")
		require.NotEqual(t, h.staticID.GetDomain(), domain,
			"instance identifier must not fall back to the static (Alan) controller Identifier")
	})

	t.Run("pre-fork height starts instance with Alan domain", func(t *testing.T) {
		ctrl := h.newController()

		inst, err := ctrl.StartNewInstance(context.Background(), logger, h.preForkHeight,
			spectestingutils.TestingQBFTFullData, okValueChecker{}, h.roundTimerF)
		require.NoError(t, err)

		require.Equal(t, h.cfg.DomainType[:], instanceDomain(t, inst.State.ID),
			"instance started at pre-fork height must carry the Alan domain")
	})
}

// TestController_UponDecided_ForkDomain drives the decided fast-forward path: when UponDecided
// sees a quorum-decided message for an unknown height, the instance it creates must carry the
// fork-correct domain rather than the controller's static Identifier.
func TestController_UponDecided_ForkDomain(t *testing.T) {
	h := newForkTestHarness()
	logger := zap.NewNop()

	signers := []*rsa.PrivateKey{h.ks.OperatorKeys[1], h.ks.OperatorKeys[2], h.ks.OperatorKeys[3]}
	ids := []spectypes.OperatorID{1, 2, 3}

	t.Run("post-fork decided message fast-forwards instance with Boole domain", func(t *testing.T) {
		ctrl := h.newController()

		decidedMsg := spectestingutils.TestingCommitMultiSignerMessageWithHeightAndIdentifier(
			signers, ids, h.postForkHeight, h.identifierFn(h.postForkHeight))
		pmsg, err := specqbft.NewProcessingMessage(decidedMsg)
		require.NoError(t, err)

		decided, err := ctrl.UponDecided(logger, pmsg, h.roundTimerF)
		require.NoError(t, err)
		require.NotNil(t, decided, "quorum-decided message for unknown height must be newly decided")

		inst := ctrl.RecentInstances.FindInstance(h.postForkHeight)
		require.NotNil(t, inst)
		domain := instanceDomain(t, inst.State.ID)
		require.Equal(t, h.cfg.NextDomainType[:], domain,
			"fast-forwarded instance at post-fork height must carry the Boole domain")
		require.NotEqual(t, h.staticID.GetDomain(), domain,
			"fast-forwarded instance must not fall back to the static (Alan) controller Identifier")
	})

	t.Run("pre-fork decided message fast-forwards instance with Alan domain", func(t *testing.T) {
		ctrl := h.newController()

		decidedMsg := spectestingutils.TestingCommitMultiSignerMessageWithHeightAndIdentifier(
			signers, ids, h.preForkHeight, h.identifierFn(h.preForkHeight))
		pmsg, err := specqbft.NewProcessingMessage(decidedMsg)
		require.NoError(t, err)

		decided, err := ctrl.UponDecided(logger, pmsg, h.roundTimerF)
		require.NoError(t, err)
		require.NotNil(t, decided)

		inst := ctrl.RecentInstances.FindInstance(h.preForkHeight)
		require.NotNil(t, inst)
		require.Equal(t, h.cfg.DomainType[:], instanceDomain(t, inst.State.ID),
			"fast-forwarded instance at pre-fork height must carry the Alan domain")
	})
}
