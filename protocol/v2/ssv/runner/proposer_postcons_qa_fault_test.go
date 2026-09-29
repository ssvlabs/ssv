package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

func postconsTestEntry(root byte, index phase0.ValidatorIndex) *spectypes.PartialSignatureMessage {
	sig := make(spectypes.Signature, 96)
	sig[0] = root
	return &spectypes.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      [32]byte{root},
		Signer:           5,
		ValidatorIndex:   index,
	}
}

// honestPostcons returns a fresh honest packet: {block} on an external-bid slot, {block, envelope}
// on a self-build slot, plus a deep snapshot used to prove the helper never mutates it.
func honestPostcons(selfBuild bool) (honest, snapshot []*spectypes.PartialSignatureMessage) {
	honest = []*spectypes.PartialSignatureMessage{postconsTestEntry(0xb1, 42)}
	if selfBuild {
		honest = append(honest, postconsTestEntry(0xe1, 42))
	}
	for _, m := range honest {
		snapshot = append(snapshot, clonePartialSig(m))
	}
	return honest, snapshot
}

var postconsValues = []faults.Fault{
	faults.PostConsBlockOnly, faults.PostCons3Entries, faults.PostConsMixedIndex, faults.PostConsForeignRoot,
}

func requireIdentity(t *testing.T, plan postconsFaultPlan, honest []*spectypes.PartialSignatureMessage) {
	t.Helper()
	require.False(t, plan.fired)
	require.Nil(t, plan.firedFields)
	require.Len(t, plan.packets, 1)
	require.Equal(t, len(honest), len(plan.packets[0]))
	for i := range honest {
		require.Same(t, honest[i], plan.packets[0][i], "identity must hand back the honest entries themselves")
	}
}

func TestPostconsFaultPackets(t *testing.T) {
	t.Run("FAULT=none is the honest packet at every slot kind", func(t *testing.T) {
		for _, gloas := range []bool{false, true} {
			for _, selfBuild := range []bool{false, true} {
				honest, _ := honestPostcons(selfBuild)
				plan := postconsFaultPackets(faults.None, honest, gloas, selfBuild)
				requireIdentity(t, plan, honest)
				require.Empty(t, plan.notApplicable)
			}
		}
	})

	t.Run("a non-section-6 value is the honest packet", func(t *testing.T) {
		honest, _ := honestPostcons(true)
		plan := postconsFaultPackets(faults.BlockWrongVersion, honest, true, true)
		requireIdentity(t, plan, honest)
		require.Empty(t, plan.notApplicable)
	})

	t.Run("pre-Gloas slots never inject and stay silent", func(t *testing.T) {
		for _, f := range postconsValues {
			honest, _ := honestPostcons(false)
			plan := postconsFaultPackets(f, honest, false, false)
			requireIdentity(t, plan, honest)
			require.Empty(t, plan.notApplicable, "%s", f)
		}
	})

	t.Run("block-only", func(t *testing.T) {
		honest, snap := honestPostcons(true)
		plan := postconsFaultPackets(faults.PostConsBlockOnly, honest, true, true)
		require.True(t, plan.fired)
		require.Len(t, plan.packets, 1, "block-only replaces the honest packet")
		require.Len(t, plan.packets[0], 1)
		require.Equal(t, snap[0], plan.packets[0][0], "entry 0 identical: root, index, signature, signer")
		require.Equal(t, snap, honest, "honest packet not mutated")

		ext, _ := honestPostcons(false)
		plan = postconsFaultPackets(faults.PostConsBlockOnly, ext, true, false)
		requireIdentity(t, plan, ext)
		require.Contains(t, plan.notApplicable, "external-bid slot")
	})

	t.Run("3-entries", func(t *testing.T) {
		for _, selfBuild := range []bool{false, true} {
			honest, snap := honestPostcons(selfBuild)
			plan := postconsFaultPackets(faults.PostCons3Entries, honest, true, selfBuild)
			require.True(t, plan.fired)
			require.Len(t, plan.packets, 2, "3-entries goes alongside the honest packet")
			require.Equal(t, snap, plan.packets[0], "first packet byte-identical to honest")
			forged := plan.packets[1]
			require.Len(t, forged, 3)
			for i, m := range forged {
				require.Equal(t, spectypes.OperatorID(5), m.Signer)
				require.Equal(t, phase0.ValidatorIndex(42), m.ValidatorIndex)
				if i < len(honest) {
					require.Equal(t, snap[i], m)
				} else {
					require.Equal(t, snap[0], m, "padding copies entry 0")
				}
				require.NotSame(t, honest[0], m)
			}
			require.Equal(t, snap, honest, "honest packet not mutated")
			require.Empty(t, plan.notApplicable)
		}
	})

	t.Run("mixed-index", func(t *testing.T) {
		for _, selfBuild := range []bool{false, true} {
			honest, snap := honestPostcons(selfBuild)
			plan := postconsFaultPackets(faults.PostConsMixedIndex, honest, true, selfBuild)
			require.True(t, plan.fired)
			require.Len(t, plan.packets, 2, "mixed-index goes alongside the honest packet")
			require.Equal(t, snap, plan.packets[0])
			forged := plan.packets[1]
			require.Len(t, forged, 2, "exactly two entries, so the entry cap does not fire first")
			require.Equal(t, snap[0], forged[0], "entry 0 keeps the real index")
			require.Equal(t, phase0.ValidatorIndex(43), forged[1].ValidatorIndex)
			require.Equal(t, spectypes.OperatorID(5), forged[1].Signer)
			want := snap[len(snap)-1]
			require.Equal(t, want.SigningRoot, forged[1].SigningRoot)
			require.Equal(t, want.PartialSignature, forged[1].PartialSignature)
			require.Equal(t, snap, honest, "honest packet not mutated")
		}
	})

	t.Run("foreign-root", func(t *testing.T) {
		honest, snap := honestPostcons(true)
		plan := postconsFaultPackets(faults.PostConsForeignRoot, honest, true, true)
		require.True(t, plan.fired)
		require.Len(t, plan.packets, 1, "foreign-root replaces the honest packet")
		forged := plan.packets[0]
		require.Len(t, forged, 2)
		require.Equal(t, snap[0], forged[0], "block entry unchanged")
		require.NotEqual(t, snap[1].SigningRoot, forged[1].SigningRoot)
		require.NotEqual(t, snap[0].SigningRoot, forged[1].SigningRoot)
		require.Equal(t, snap[1].PartialSignature, forged[1].PartialSignature, "partial signature kept")
		require.Equal(t, snap[1].ValidatorIndex, forged[1].ValidatorIndex)
		require.Equal(t, snap[1].Signer, forged[1].Signer)
		require.Equal(t, snap, honest, "honest packet not mutated")

		ext, _ := honestPostcons(false)
		plan = postconsFaultPackets(faults.PostConsForeignRoot, ext, true, false)
		requireIdentity(t, plan, ext)
		require.Contains(t, plan.notApplicable, "wrong expected roots count")
	})
}

func TestQAPostconsPacketsLogs(t *testing.T) {
	newLogger := func() (*zap.Logger, *observer.ObservedLogs) {
		core, logs := observer.New(zap.DebugLevel)
		return zap.New(core), logs
	}

	t.Run("none logs nothing", func(t *testing.T) {
		faults.SetForTest(t, faults.None)
		logger, logs := newLogger()
		honest, _ := honestPostcons(true)
		packets := qaPostconsPackets(logger, 8, true, true, honest)
		require.Len(t, packets, 1)
		require.Equal(t, 0, logs.Len())
	})

	t.Run("fired carries slot and shape", func(t *testing.T) {
		faults.SetForTest(t, faults.PostCons3Entries)
		logger, logs := newLogger()
		honest, _ := honestPostcons(false)
		qaPostconsPackets(logger, 8, true, false, honest)
		entries := logs.FilterMessage("🧪 qa fault injected").All()
		require.Len(t, entries, 1)
		ctx := entries[0].ContextMap()
		require.Equal(t, "postcons-3-entries", ctx["qa_fault"])
		require.EqualValues(t, 8, ctx["slot"])
		require.EqualValues(t, 3, ctx["entries"])
		require.EqualValues(t, 1, ctx["honest_entries"])
		require.Equal(t, false, ctx["replaces_honest"])
	})

	t.Run("not applicable on external-bid slots", func(t *testing.T) {
		for _, f := range []faults.Fault{faults.PostConsBlockOnly, faults.PostConsForeignRoot} {
			faults.SetForTest(t, f)
			logger, logs := newLogger()
			honest, _ := honestPostcons(false)
			qaPostconsPackets(logger, 8, true, false, honest)
			require.Equal(t, 0, logs.FilterMessage("🧪 qa fault injected").Len())
			require.Equal(t, 1, logs.FilterMessage("🧪 qa fault not applicable").Len(), "%s", f)
		}
	})

	t.Run("pre-Gloas is silent", func(t *testing.T) {
		for _, f := range postconsValues {
			faults.SetForTest(t, f)
			logger, logs := newLogger()
			honest, _ := honestPostcons(false)
			qaPostconsPackets(logger, 8, false, false, honest)
			require.Equal(t, 0, logs.Len(), "%s", f)
		}
	})
}

// broadcastPostconsPackets returns the post-consensus packets the runner broadcast, in order.
func broadcastPostconsPackets(t *testing.T, network *protocoltesting.TestingNetwork) []*spectypes.PartialSignatureMessages {
	t.Helper()
	var out []*spectypes.PartialSignatureMessages
	for _, msg := range network.BroadcastedMsgs {
		if msg.SSVMessage == nil || msg.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType {
			continue
		}
		decoded := &spectypes.PartialSignatureMessages{}
		require.NoError(t, decoded.Decode(msg.SSVMessage.Data))
		if decoded.Type == spectypes.PostConsensusPartialSig {
			out = append(out, decoded)
		}
	}
	return out
}

// Runner level: the section 6 hook in ProcessConsensus broadcasts the planned packets, and an honest
// receiver's runner reacts to each forged packet with the rule the value targets.
func TestProposerRunnerProcessConsensusPostconsFaults(t *testing.T) {
	const slot = phase0.Slot(8)

	type want struct {
		packets     []int  // entry count per broadcast packet, in order
		receiverErr string // honest receiver's error on the LAST packet; "" = accepted
		honestFirst bool   // packet 0 is the honest packet
	}
	cases := []struct {
		fault     faults.Fault
		selfBuild bool
		want      want
	}{
		{faults.None, true, want{packets: []int{2}, honestFirst: true}},
		{faults.None, false, want{packets: []int{1}, honestFirst: true}},
		{faults.PostConsBlockOnly, true, want{packets: []int{1}}},
		{faults.PostConsBlockOnly, false, want{packets: []int{1}, honestFirst: true}},
		{faults.PostConsForeignRoot, true, want{packets: []int{2}, receiverErr: "unexpected signing root"}},
		{faults.PostConsForeignRoot, false, want{packets: []int{1}, honestFirst: true}},
		{faults.PostCons3Entries, true, want{packets: []int{2, 3}, receiverErr: "wrong expected roots count", honestFirst: true}},
		{faults.PostCons3Entries, false, want{packets: []int{1, 3}, receiverErr: "wrong expected roots count", honestFirst: true}},
		{faults.PostConsMixedIndex, true, want{packets: []int{2, 2}, receiverErr: "unknown validator index", honestFirst: true}},
		{faults.PostConsMixedIndex, false, want{packets: []int{1, 2}, receiverErr: "unknown validator index", honestFirst: true}},
	}

	for _, tc := range cases {
		name := string(tc.fault) + "/external"
		if tc.selfBuild {
			name = string(tc.fault) + "/self-build"
		}
		t.Run(name, func(t *testing.T) {
			faults.SetForTest(t, tc.fault)

			var proposal *gloas.GloasProposalData
			if tc.selfBuild {
				proposal, _ = gloasSelfBuildProposal(t, slot)
			} else {
				proposal = gloasExternalBuildProposal(slot)
			}

			ctx, logger := context.Background(), zap.NewNop()
			sender, keySet, network := newProposerRunnerForTest(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, 0, gloasTestConfig(slot))
			require.NoError(t, sender.StartNewDuty(ctx, logger, gloasProposerDuty(slot), keySet.Threshold))
			require.NoError(t, decideProposerDuty(t, sender, keySet, slot, gloasConsensusData(t, proposal)))

			packets := broadcastPostconsPackets(t, network)
			require.Len(t, packets, len(tc.want.packets))
			for i, n := range tc.want.packets {
				require.Len(t, packets[i].Messages, n, "packet %d", i)
				require.Equal(t, slot, packets[i].Slot)
			}

			blockRoot, envelopeRoot, err := sender.gloasPostConsensusSigningRoots(ctx, proposal)
			require.NoError(t, err)
			if tc.want.honestFirst {
				require.Equal(t, blockRoot, packets[0].Messages[0].SigningRoot)
				if tc.selfBuild {
					require.Equal(t, envelopeRoot, packets[0].Messages[1].SigningRoot)
				}
			}

			// The honest receiver has decided on the same value and has not yet seen a quorum.
			receiver, _ := newGloasProposerForPostConsensus(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, proposal)
			for i, p := range packets {
				err := receiver.ProcessPostConsensus(ctx, logger, p)
				if i == len(packets)-1 && tc.want.receiverErr != "" {
					require.ErrorContains(t, err, tc.want.receiverErr)
					continue
				}
				require.NoError(t, err, "packet %d", i)
			}
		})
	}
}
