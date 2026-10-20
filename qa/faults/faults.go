// Package faults is QA instrumentation for pass M3 of the Glamsterdam (Gloas / ePBS) QA
// programme. One environment variable, FAULT, selects one deliberate misbehaviour; a restart of
// the node changes it. See qa/FAULTS.md for the operator runbook and
// docs/qa-glamsterdam-test-plan-passes.md section 6.3 in ssv-scout for the specification.
//
// This package exists only on the qa/gloas-m3-fault-menu branch. It must never reach stage.
package faults

import (
	"fmt"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"
)

// Fault is one value of the menu.
type Fault string

const (
	None Fault = "none"

	// Section 2 — the committee vote.
	Vote112B           Fault = "vote-112b"
	VoteIndex2         Fault = "vote-index-2"
	VoteIndexFlip      Fault = "vote-index-flip"
	VoteIndex1SameSlot Fault = "vote-index-1-same-slot"
	DoubleVoteIndex    Fault = "double-vote-index"

	// Section 3 — PTC on the wire.
	PTCQBFT      Fault = "ptc-qbft"
	TwoEntries   Fault = "two-entries"
	PTC3PerEpoch Fault = "ptc-3-per-epoch"

	// Section 4 — the block and its value-check pins.
	BlockWrongVersion        Fault = "block-wrong-version"
	BlockWrongSlot           Fault = "block-wrong-slot"
	BlockWrongProposerIndex  Fault = "block-wrong-proposer-index"
	PayloadRootOnExternal    Fault = "payload-root-on-external"
	PayloadRootZeroSelfBuild Fault = "payload-root-zero-selfbuild"
	PayloadRootFabricated    Fault = "payload-root-fabricated"

	// Section 5 — proposer preferences and the request-auth overlay.
	PrefsConflict          Fault = "prefs-conflict"
	Prefs34Apart           Fault = "prefs-34-apart"
	Prefs5Roots            Fault = "prefs-5-roots"
	PrefsEarly             Fault = "prefs-early"
	PrefsEarlyEpoch        Fault = "prefs-early-epoch"
	PrefsLate              Fault = "prefs-late"
	PrefsReplay            Fault = "prefs-replay"
	AuthNoBuilders         Fault = "auth-no-builders"
	Auth9Entries           Fault = "auth-9-entries"
	AuthMixedSigner        Fault = "auth-mixed-signer"
	AuthRepeatRootInPacket Fault = "auth-repeat-root-in-packet"
	RequestAuthLate        Fault = "request-auth-late"

	// Section 6 — the proposer post-consensus packet (generation 3: block + envelope entries).
	// envelope-foreign-root, envelope-builder-index and envelope-round-3 were retired on
	// 2026-09-28 with the code they targeted; setting any of them aborts startup.
	PostConsBlockOnly   Fault = "postcons-block-only"
	PostCons3Entries    Fault = "postcons-3-entries"
	PostConsMixedIndex  Fault = "postcons-mixed-index"
	PostConsForeignRoot Fault = "postcons-foreign-root"

	// Section 7 — the fork gates and the proposer round cap.
	Role7PreFork        Fault = "role7-prefork"
	VRPostFork          Fault = "vr-postfork"
	VRPreForkSlotReplay Fault = "vr-prefork-slot-replay"
	ProposerRound3      Fault = "proposer-round-3"
)

// Entry documents one menu value. Scenarios are the ssv-scout scenario IDs the value serves.
type Entry struct {
	Fault     Fault
	Scenarios string
	Behaviour string
	Site      string
}

var menu = []Entry{
	{Vote112B, "ATT-02", "propose a pre-Gloas 112-byte BeaconVote at a Gloas slot", "protocol/v2/ssv/runner/committee.go executeDuty"},
	{VoteIndex2, "ATT-03", "propose AttestationDataIndex = 2", "protocol/v2/ssv/runner/committee.go executeDuty"},
	{VoteIndexFlip, "FLT-05", "propose the wrong but valid index (0 becomes 1, 1 becomes 0)", "protocol/v2/ssv/runner/committee.go executeDuty"},
	{VoteIndex1SameSlot, "ATT-10", "propose AttestationDataIndex = 1 when the head is the same-slot block (the same-slot sub-case of vote-index-flip)", "protocol/v2/ssv/runner/committee.go executeDuty"},
	{DoubleVoteIndex, "ATT-04", "sign index 0 then index 1 for the same slot against the local signer", "protocol/v2/ssv/runner/committee.go signAttesterDuty"},
	{PTCQBFT, "PTC-07, MSG-03", "send a QBFT consensus message under role 7", "qa/faultnet"},
	{TwoEntries, "MSG-03", "send two entries in one role-7 partial-signature container", "qa/faultnet"},
	{PTC3PerEpoch, "MSG-07", "send three PTC partials at three slots in one epoch; the two forged ones are refused by the per-epoch assignment gate", "qa/faultnet"},
	{BlockWrongVersion, "PRO-07", "propose a Gloas block stamped with the Fulu data version", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{BlockWrongSlot, "PRO-10", "propose a Gloas block whose slot is not the duty slot", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{BlockWrongProposerIndex, "PRO-10", "propose a Gloas block whose proposer_index is not the duty validator index", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{PayloadRootOnExternal, "PRO-10, EPE-04", "propose an external-bid value with a non-zero PayloadRoot (natural external bids only; logs not-applicable on self-build slots)", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{PayloadRootZeroSelfBuild, "PRO-10, EPE-04", "propose a self-build value with a zero PayloadRoot", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{PayloadRootFabricated, "EPE-04, FLT-06", "propose a self-build value with a fabricated PayloadRoot that passes the value check", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{PrefsConflict, "PRF-07, FLT-07", "emit a preference whose fee recipient differs from the cluster's", "protocol/v2/ssv/runner/proposer_preferences.go buildProposerPreferences"},
	{Prefs34Apart, "MSG-06", "fill the 4-root budget at proposal slot P, open slot P+34, then send a fifth root at P", "qa/faultnet"},
	{Prefs5Roots, "MSG-05, FLT-07", "emit five distinct preference roots for one slot, then repeat an accepted root", "qa/faultnet"},
	{PrefsEarly, "MSG-04", "emit preferences 65 slots ahead of the emission slot, which always crosses the epoch earliness bound", "qa/faultnet"},
	{PrefsEarlyEpoch, "MSG-04", "probe the epoch earliness bound: one message a slot before it (ignored) and a control exactly at it (accepted)", "qa/faultnet"},
	{PrefsLate, "MSG-04", "emit preferences three slots late", "qa/faultnet"},
	{PrefsReplay, "FLT-11", "replay one valid preference at a high rate across 66 slots", "qa/faultnet"},
	{AuthNoBuilders, "MSG-10", "broadcast request-auth partials that the receivers have no Builders for", "cli/operator/node.go newNode"},
	{Auth9Entries, "MSG-03, MSG-10", "send nine entries in one type-9 request-auth packet", "qa/faultnet"},
	{AuthMixedSigner, "MSG-03, MSG-10", "send type-9 entries with two validator indices", "qa/faultnet"},
	{AuthRepeatRootInPacket, "MSG-10", "send a type-9 packet that repeats one root in two entries", "qa/faultnet"},
	{RequestAuthLate, "BLD-B-04", "hold this operator's request-auth (type 9) packet for proposal slot S and release it at S start + 4 s, after S's produce call", "qa/faultnet"},
	{PostConsBlockOnly, "EPE-09", "omit the envelope entry from the proposer post-consensus packet on self-build slots", "protocol/v2/ssv/runner/proposer.go ProcessConsensus"},
	{PostCons3Entries, "EPE-09, MSG-03", "send, after the honest packet, a second proposer post-consensus packet with three entries at a Gloas slot", "protocol/v2/ssv/runner/proposer.go ProcessConsensus"},
	{PostConsMixedIndex, "EPE-09, MSG-03", "send, after the honest packet, a second proposer post-consensus packet whose entries carry two validator indices", "protocol/v2/ssv/runner/proposer.go ProcessConsensus"},
	{PostConsForeignRoot, "EPE-09", "replace the honest packet with one whose envelope entry carries a root that matches no expected root (self-build slots)", "protocol/v2/ssv/runner/proposer.go ProcessConsensus"},
	{Role7PreFork, "MSG-02", "send role 7 and 8 messages for a fixed pre-fork slot, cloned from outgoing partial-signature messages (throttled)", "qa/faultnet"},
	{VRPostFork, "MSG-02", "keep the validator-registration heartbeat running at Gloas slots, one validator per epoch", "operator/duties/validator_registration.go (scheduling) + protocol/v2/ssv/runner/validator_registration.go (gates, fires once broadcast)"},
	{VRPreForkSlotReplay, "MSG-02 (gate 3)", "send a validator-registration partial stamped with a pre-fork slot after the fork epoch", "qa/faultnet"},
	{ProposerRound3, "TIM-08", "broadcast RoundChange(3) after the proposer instance's round-2 expiry", "protocol/v2/qbft/instance/timeout.go"},
}

// active holds the Fault selected at boot. It is written once, before any goroutine that reads it
// starts, and read on hot paths, so it is an atomic value rather than a plain variable.
var active atomic.Value

func init() { active.Store(None) }

// Menu returns a copy of the menu.
func Menu() []Entry {
	out := make([]Entry, len(menu))
	copy(out, menu)
	return out
}

// Names returns every menu value except none, in menu order.
func Names() []string {
	out := make([]string, 0, len(menu))
	for _, e := range menu {
		out = append(out, string(e.Fault))
	}
	return out
}

// Parse resolves the FAULT environment value. An empty value and "none" both mean no fault. Any
// other unknown value is an error: a typo must abort startup rather than silently run a clean node
// that the tester then records as a passing fault run.
func Parse(s string) (Fault, error) {
	v := Fault(strings.ToLower(strings.TrimSpace(s)))
	if v == "" || v == None {
		return None, nil
	}
	for _, e := range menu {
		if e.Fault == v {
			return v, nil
		}
	}
	return None, fmt.Errorf("unknown FAULT %q, known values: none, %s", s, strings.Join(Names(), ", "))
}

// Init sets the active fault. Call it once, at boot, before the network or any runner exists.
func Init(f Fault) { active.Store(f) }

// Active returns the fault selected at boot.
func Active() Fault { return active.Load().(Fault) }

// Enabled reports whether any fault is active.
func Enabled() bool { return Active() != None }

// Is reports whether f is the active fault. This is the guard used at every injection site.
func Is(f Fault) bool { return Active() == f }

// ── Modifier: FAULT_NO_LOCAL_VALIDATION ─────────────────────────────────────────────────────────
//
// This is a MODIFIER, not a menu value, and that distinction is the point. FAULT selects exactly
// one misbehaviour; a 20th menu value would force a choice between (say) prefs-early AND seeing
// the honest side's reaction to it, which is precisely the combination the M3 run showed we need.
// As a separate variable it composes with any menu value.
//
// Why it exists: the 2026-09-09 M3 ladder found SIX values whose rule fired only on the FAULTED
// operator, with the honest side silent — prefs-early (11/11), prefs-late (15/15), ptc-qbft
// (20/20), two-entries (19/19), role7-prefork (3006/3009) and prefs-replay (75,512). For those the
// passes doc's §6.6 oracle ("expected result on the honest operators") could not be evidenced at
// all: we proved the sender enforces the rule and never tested the receivers. Setting this
// modifier removes the sender's pubsub validation gate so the forged message reaches the mesh and
// the honest operators' own enforcement becomes observable.
//
// Scope, deliberately: it wraps ONLY the pubsub gate (cli/operator/node.go's
// cfg.P2pNetworkConfig.MessageValidator), not the runner's own validator use. Runner-level
// rejections — "no duty assigned" (ptc-3-per-epoch), "no builders configured" (auth-no-builders) —
// are real M3 results and must keep working. It is also accept-ALL rather than accept-own: the
// faulted operator therefore stops validating INBOUND peer messages too. Harmless in a QA enclave
// where it is the only misbehaving sender, and cheaper than plumbing an own-signer check, but it
// means this node's receive path is NOT a valid observation point while the modifier is on.
var noLocalValidation atomic.Bool

// ParseNoLocalValidation resolves FAULT_NO_LOCAL_VALIDATION. Empty means off. Anything
// unrecognised is an error rather than a silent false, for the same reason Parse rejects an unknown
// FAULT: a typo must abort startup, not hand back a node that quietly still validates locally
// while the tester records the run as having bypassed it.
func ParseNoLocalValidation(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "false", "0", "no", "off":
		return false, nil
	case "true", "1", "yes", "on":
		return true, nil
	}
	return false, fmt.Errorf("unknown FAULT_NO_LOCAL_VALIDATION %q, want one of: true, false (empty means false)", s)
}

// InitNoLocalValidation sets the modifier. Call once at startup, before the network is built.
func InitNoLocalValidation(b bool) { noLocalValidation.Store(b) }

// NoLocalValidation reports whether the sender's pubsub validation gate is disabled.
func NoLocalValidation() bool { return noLocalValidation.Load() }

// Fired records that an injection happened. Every site must call it: the M3 oracles read the
// honest operators, so a silent honest side is only evidence when the fault provably fired.
func Fired(logger *zap.Logger, fields ...zap.Field) {
	logger.Warn("🧪 qa fault injected", append([]zap.Field{zap.String("qa_fault", string(Active()))}, fields...)...)
}

// NotApplicable records that the active fault had nothing to act on at this site. A value that
// is inapplicable on a slot must say so rather than stay silent, so the absence of any 🧪 line
// still means a broken rig, never an inapplicable value (see payload-root-on-external).
func NotApplicable(logger *zap.Logger, reason string, fields ...zap.Field) {
	logger.Warn("🧪 qa fault not applicable", append([]zap.Field{
		zap.String("qa_fault", string(Active())), zap.String("reason", reason)}, fields...)...)
}

// Banner logs the boot state of the instrumentation, loudly when a fault is active.
func Banner(logger *zap.Logger) {
	if NoLocalValidation() {
		logger.Warn("🧪 QA MODIFIER: LOCAL PUBSUB VALIDATION DISABLED — this node does not validate "+
			"messages on the gossip path, so its own forged messages reach the mesh and it also "+
			"stops rejecting invalid INBOUND messages. Do not read this node's receive path as evidence.",
			zap.Bool("no_local_validation", true))
	}
	if !Enabled() {
		logger.Info("qa fault instrumentation present, no fault active",
			zap.String("qa_fault", string(None)),
			zap.Bool("no_local_validation", NoLocalValidation()))
		return
	}
	for _, e := range menu {
		if e.Fault == Active() {
			logger.Warn("🧪 QA FAULT INSTRUMENTATION ACTIVE — this node misbehaves on purpose",
				zap.String("qa_fault", string(e.Fault)),
				zap.String("behaviour", e.Behaviour),
				zap.String("site", e.Site),
				zap.String("scenarios", e.Scenarios),
				zap.Bool("no_local_validation", NoLocalValidation()))
			return
		}
	}
}
