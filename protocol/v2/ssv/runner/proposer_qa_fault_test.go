package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

func TestGloasBlockVersion(t *testing.T) {
	t.Run("honest", func(t *testing.T) {
		require.Equal(t, networkconfig.DataVersionGloas, gloasBlockVersion())
	})

	t.Run("block-wrong-version stamps fulu", func(t *testing.T) {
		faults.SetForTest(t, faults.BlockWrongVersion)
		require.Equal(t, spec.DataVersionFulu, gloasBlockVersion())
	})
}

// The §4 pin strings of the honest proposer value check (protocol/v2/ssv/value_check.go), in evaluation
// order. Each §4 value must trip exactly its own pin on an honest operator, and no other.
var gloasProposalPins = []string{
	"consensus data duty slot does not match running duty slot",      // pin 1
	"value version does not match slot fork",                         // pin 2
	"gloas block slot does not match duty slot",                      // pin 3
	"gloas block proposer index does not match duty validator index", // pin 4
	"gloas payload_root presence does not match self-build bid",      // pin 5
}

const (
	pin2  = 1
	pin3  = 2
	pin4  = 3
	pin5  = 4
	noPin = -1
)

// TestGloasProposalFaults drives gloasProposalInput under each §4 value and checks the resulting consensus
// value with an HONEST proposer checker (FAULT=none's, with the running-duty slot set), as an honest
// follower would. It also pins what the faulted node does to its own check, and its evidence line.
func TestGloasProposalFaults(t *testing.T) {
	const slot = phase0.Slot(8)
	pk := spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey)
	share := spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex)

	for _, tc := range []struct {
		name     string
		fault    faults.Fault
		external bool // the beacon node returns an external bid instead of a self-build block
		wantPin  int  // index into gloasProposalPins the honest checker must report; noPin = accepted
		// permissive: the faulted node must swap in PermissiveValueCheck (its own check would refuse the value).
		permissive bool
		// evidence: "fired", "not-applicable" or "" (no 🧪 line at all).
		evidence   string
		wantFields map[string]any
		check      func(t *testing.T, honest, got *gloas.GloasProposalData)
	}{
		{name: "none self-build", fault: faults.None, wantPin: noPin},
		{name: "none external", fault: faults.None, external: true, wantPin: noPin},
		{
			name: "block-wrong-version", fault: faults.BlockWrongVersion, wantPin: pin2, permissive: true, evidence: "fired",
			wantFields: map[string]any{"stamped_version": spec.DataVersionFulu.String()},
		},
		{
			name: "block-wrong-slot", fault: faults.BlockWrongSlot, wantPin: pin3, permissive: true, evidence: "fired",
			wantFields: map[string]any{"honest_block_slot": uint64(slot), "stamped_block_slot": uint64(slot + 1), "honest_self_build": true},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.Equal(t, slot+1, got.Block.Slot)
				require.Equal(t, honest.Block.ProposerIndex, got.Block.ProposerIndex)
			},
		},
		{
			name: "block-wrong-proposer-index", fault: faults.BlockWrongProposerIndex, wantPin: pin4, permissive: true, evidence: "fired",
			wantFields: map[string]any{
				"honest_proposer_index":  uint64(spectestingutils.TestingValidatorIndex),
				"stamped_proposer_index": uint64(spectestingutils.TestingValidatorIndex + 1),
				"honest_self_build":      true,
			},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.Equal(t, slot, got.Block.Slot)
				require.Equal(t, phase0.ValidatorIndex(spectestingutils.TestingValidatorIndex+1), got.Block.ProposerIndex)
			},
		},
		{
			name: "payload-root-on-external, natural external bid", fault: faults.PayloadRootOnExternal, external: true,
			wantPin: pin5, permissive: true, evidence: "fired",
			wantFields: map[string]any{"honest_self_build": false, "synthetic_external": false, "builder_index": uint64(5)},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.False(t, got.SelfBuild(), "the bid is not forged")
				require.NotEqual(t, phase0.Root{}, got.PayloadRoot)
			},
		},
		{
			// Decision D1: no synthetic external bid. A self-build slot is left honest and says so.
			name: "payload-root-on-external, self-build slot", fault: faults.PayloadRootOnExternal,
			wantPin: noPin, evidence: "not-applicable",
			wantFields: map[string]any{"reason": "self-build slot: no external bid to stamp"},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.True(t, got.SelfBuild(), "no bid is forged")
				require.Equal(t, honest.PayloadRoot, got.PayloadRoot)
			},
		},
		{
			name: "payload-root-zero-selfbuild", fault: faults.PayloadRootZeroSelfBuild, wantPin: pin5, permissive: true, evidence: "fired",
			wantFields: map[string]any{"honest_self_build": true},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.True(t, got.SelfBuild())
				require.Equal(t, phase0.Root{}, got.PayloadRoot)
			},
		},
		{
			name: "payload-root-zero-selfbuild, external bid", fault: faults.PayloadRootZeroSelfBuild, external: true,
			wantPin: noPin, evidence: "not-applicable",
			wantFields: map[string]any{"reason": "external bid: payload_root is already zero"},
		},
		{
			// Passes every pin, so the faulted node keeps its honest check.
			name: "payload-root-fabricated", fault: faults.PayloadRootFabricated, wantPin: noPin, evidence: "fired",
			wantFields: map[string]any{"honest_self_build": true},
			check: func(t *testing.T, honest, got *gloas.GloasProposalData) {
				require.True(t, got.SelfBuild())
				require.NotEqual(t, phase0.Root{}, got.PayloadRoot)
				require.NotEqual(t, honest.PayloadRoot, got.PayloadRoot)
			},
		},
		{
			name: "payload-root-fabricated, external bid", fault: faults.PayloadRootFabricated, external: true,
			wantPin: noPin, evidence: "not-applicable",
			wantFields: map[string]any{"reason": "external bid: no payload_root to fabricate"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			faults.SetForTest(t, tc.fault)

			beacon := newProposerTestBeacon(nil)
			var honest *gloas.GloasProposalData
			if tc.external {
				honest = gloasExternalBuildProposal(slot)
				beacon.getGloasBlock = gloasExternalBuildProposal(slot).Block
			} else {
				honest, _ = gloasSelfBuildProposal(t, slot)
				beacon.getGloasBlock = gloas.TestingBeaconBlock(slot)
				beacon.getGloasEnvelope = gloasTestEnvelope(t, beacon.getGloasBlock)
			}
			cfg := gloasTestConfig(slot)
			runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, cfg)
			honestCheck := runner.ValCheck

			core, logs := observer.New(zapcore.DebugLevel)
			input, err := runner.gloasProposalInput(context.Background(), zap.New(core), gloasProposerDuty(slot), []byte("randao"))
			require.NoError(t, err)
			byts, err := input.Encode()
			require.NoError(t, err)

			// An honest follower's checker for the running duty.
			follower := ssv.NewProposerChecker(
				ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
				cfg.Beacon, pk, spectestingutils.TestingValidatorIndex, phase0.BLSPubKey(share.SharePubKey),
				func() phase0.Slot { return slot },
			)
			checkErr := follower.CheckValue(byts)
			if tc.wantPin == noPin {
				require.NoError(t, checkErr)
			} else {
				require.ErrorContains(t, checkErr, gloasProposalPins[tc.wantPin])
				for i, other := range gloasProposalPins {
					if i != tc.wantPin {
						require.NotContains(t, checkErr.Error(), other)
					}
				}
			}

			if tc.permissive {
				require.IsType(t, faults.PermissiveValueCheck{}, runner.ValCheck)
			} else {
				require.Same(t, honestCheck, runner.ValCheck, "the faulted node keeps its honest check")
			}

			fired := logs.FilterMessage("🧪 qa fault injected").All()
			notApplicable := logs.FilterMessage("🧪 qa fault not applicable").All()
			var line observer.LoggedEntry
			switch tc.evidence {
			case "fired":
				require.Len(t, fired, 1)
				require.Empty(t, notApplicable)
				line = fired[0]
			case "not-applicable":
				require.Empty(t, fired)
				require.Len(t, notApplicable, 1)
				line = notApplicable[0]
			default:
				require.Empty(t, fired)
				require.Empty(t, notApplicable)
			}
			if tc.evidence != "" {
				require.Equal(t, zapcore.WarnLevel, line.Level)
				ctxMap := line.ContextMap()
				require.Equal(t, string(tc.fault), ctxMap["qa_fault"])
				require.Equal(t, uint64(slot), ctxMap["slot"])
				for k, v := range tc.wantFields {
					require.Equal(t, v, ctxMap[k], "field %s", k)
				}
			}

			if tc.fault == faults.None {
				// FAULT=none is byte-identical to the honest path.
				want, err := (&spectypes.ProposerConsensusData{
					Duty:    *gloasProposerDuty(slot),
					Version: networkconfig.DataVersionGloas,
					DataSSZ: mustEncodeProposal(t, honest),
				}).Encode()
				require.NoError(t, err)
				require.Equal(t, want, byts)
				return
			}

			got, err := gloas.DecodeGloasProposalData(input.DataSSZ)
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, honest, got)
			}
		})
	}
}

func mustEncodeProposal(t *testing.T, p *gloas.GloasProposalData) []byte {
	t.Helper()
	b, err := p.Encode()
	require.NoError(t, err)
	return b
}

// payload-root-fabricated end to end (B.6): the value passes every pin and is decided, every operator signs
// the block and the envelope derived from the fabricated payload_root, and nobody holds the payload behind
// it. So the block is submitted, no envelope is published, and the builder operator's own match comes out
// "other" — silently, at Debug. There is NO "could not match the produced envelope" Warn: that line needs
// an error, and a mismatched root is not one. This pins the corrected EPE-04 / FLT-06 oracle.
func TestProposerRunnerGloasFabricatedPayloadRootDoesNotPublish(t *testing.T) {
	ctx := context.Background()
	proposal, produced := gloasSelfBuildProposal(t, 8)
	proposal.PayloadRoot[0] ^= 0xff // what payload-root-fabricated decides on
	beacon := newProposerTestBeacon(nil)
	runner, keySet := newGloasProposerForPostConsensus(t, beacon, &stubDoppelganger{canSign: true}, proposal)
	runner.gloasDuty.producedEnvelope = produced // this operator built the real payload

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, opID, proposal, true)))
	}

	require.Len(t, beacon.submittedGloasBlocks, 1)
	require.Empty(t, beacon.submittedEnvelopes)
	require.Equal(t, envelopeMatchOther, runner.gloasDuty.envelopeMatch)
	require.Nil(t, runner.gloasDuty.producedEnvelope)
	require.True(t, runner.State.Succeeded)
	require.Empty(t, logs.FilterLevelExact(zapcore.WarnLevel).All())
	require.Len(t, logs.FilterMessage("envelope signature reconstructed; this operator did not build the envelope, not publishing").All(), 1)
	require.Empty(t, logs.FilterMessage("✅ published execution payload envelope").All())
}
