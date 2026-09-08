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
	Vote112B        Fault = "vote-112b"
	VoteIndex2      Fault = "vote-index-2"
	VoteIndexFlip   Fault = "vote-index-flip"
	DoubleVoteIndex Fault = "double-vote-index"

	// Section 3 — PTC on the wire.
	PTCQBFT      Fault = "ptc-qbft"
	TwoEntries   Fault = "two-entries"
	PTC3PerEpoch Fault = "ptc-3-per-epoch"

	// Section 4 — the block.
	BlockWrongVersion Fault = "block-wrong-version"

	// Section 5 — proposer preferences and the request-auth overlay.
	PrefsConflict  Fault = "prefs-conflict"
	Prefs34Apart   Fault = "prefs-34-apart"
	Prefs5Roots    Fault = "prefs-5-roots"
	PrefsEarly     Fault = "prefs-early"
	PrefsLate      Fault = "prefs-late"
	PrefsReplay    Fault = "prefs-replay"
	AuthNoBuilders Fault = "auth-no-builders"

	// Section 6 — the execution payload envelope.
	EnvelopeForeignRoot  Fault = "envelope-foreign-root"
	EnvelopeBuilderIndex Fault = "envelope-builder-index"

	// Section 7 — the fork gates.
	Role7PreFork Fault = "role7-prefork"
	VRPostFork   Fault = "vr-postfork"
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
	{DoubleVoteIndex, "ATT-04", "sign index 0 then index 1 for the same slot against the local signer", "protocol/v2/ssv/runner/committee.go signAttesterDuty"},
	{PTCQBFT, "PTC-07, MSG-03", "send a QBFT consensus message under role 7", "qa/faultnet"},
	{TwoEntries, "MSG-03", "send two entries in one role-7 partial-signature container", "qa/faultnet"},
	{PTC3PerEpoch, "MSG-07", "send three PTC partials at three slots in one epoch; the two forged ones are refused by the per-epoch assignment gate", "qa/faultnet"},
	{BlockWrongVersion, "PRO-07", "propose a Gloas block stamped with the Fulu data version", "protocol/v2/ssv/runner/proposer.go gloasProposalInput"},
	{PrefsConflict, "PRF-07, FLT-07", "emit a preference whose fee recipient differs from the cluster's", "protocol/v2/ssv/runner/proposer_preferences.go buildProposerPreferences"},
	{Prefs34Apart, "MSG-06", "alternate the preference root for proposal slots 34 slots apart", "protocol/v2/ssv/runner/proposer_preferences.go buildProposerPreferences"},
	{Prefs5Roots, "MSG-05, FLT-07", "emit five distinct preference roots for one slot", "qa/faultnet"},
	{PrefsEarly, "MSG-04", "emit preferences 65 slots early, one past the lookahead allowance", "qa/faultnet"},
	{PrefsLate, "MSG-04", "emit preferences three slots late", "qa/faultnet"},
	{PrefsReplay, "FLT-11", "replay one valid preference at a high rate across 66 slots", "qa/faultnet"},
	{AuthNoBuilders, "MSG-10", "broadcast request-auth partials that the receivers have no Builders for", "cli/operator/node.go newNode"},
	{EnvelopeForeignRoot, "EPE-04, FLT-06", "propose an envelope with a foreign BeaconBlockRoot", "protocol/v2/ssv/runner/envelope.go produceBlindedEnvelope"},
	{EnvelopeBuilderIndex, "EPE-04", "propose an envelope with a non-self-build BuilderIndex", "protocol/v2/ssv/runner/envelope.go produceBlindedEnvelope"},
	{Role7PreFork, "MSG-02", "send role 7, 8 and 9 messages for a fixed pre-fork slot, cloned from any outgoing partial-signature message", "qa/faultnet"},
	{VRPostFork, "MSG-02", "keep the validator-registration heartbeat running at Gloas slots", "operator/duties/validator_registration.go (scheduling) + protocol/v2/ssv/runner/validator_registration.go executeDuty (fires once broadcast)"},
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

// Fired records that an injection happened. Every site must call it: the M3 oracles read the
// honest operators, so a silent honest side is only evidence when the fault provably fired.
func Fired(logger *zap.Logger, fields ...zap.Field) {
	logger.Warn("🧪 qa fault injected", append([]zap.Field{zap.String("qa_fault", string(Active()))}, fields...)...)
}

// Banner logs the boot state of the instrumentation, loudly when a fault is active.
func Banner(logger *zap.Logger) {
	if !Enabled() {
		logger.Info("qa fault instrumentation present, no fault active", zap.String("qa_fault", string(None)))
		return
	}
	for _, e := range menu {
		if e.Fault == Active() {
			logger.Warn("🧪 QA FAULT INSTRUMENTATION ACTIVE — this node misbehaves on purpose",
				zap.String("qa_fault", string(e.Fault)),
				zap.String("behaviour", e.Behaviour),
				zap.String("site", e.Site),
				zap.String("scenarios", e.Scenarios))
			return
		}
	}
}
