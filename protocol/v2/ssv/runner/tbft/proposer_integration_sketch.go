package tbft

// proposer_integration_sketch.go is a documentation-style file describing the
// shape of modifications to `protocol/v2/ssv/runner/proposer.go` to use the
// TBFT adapter in place of QBFT. It does NOT compile a parallel runner — that
// would require duplicating SSV's runner-base infrastructure (beacon, network,
// signer, doppelganger, slashing-protection, metrics, tracing, …) which the
// existing ProposerRunner pulls in through its embedded BaseRunner.
//
// Instead this file lays out, in pseudo-code with concrete field references,
// the call-site changes needed in the existing ProposerRunner to swap the
// QBFT consensus path for the TBFT adapter. The expectation is that the
// SSV team takes this sketch, reads alongside the existing proposer.go,
// and applies the changes in a focused PR with test coverage.
//
// All references like `r.QBFTController` and `r.GetBeaconNode()` are to
// the existing ProposerRunner struct and its embedded BaseRunner — see
// [protocol/v2/ssv/runner/proposer.go](../proposer.go) for the actual
// definitions.

/*

================================================================================
Sketch: ProposerRunner modifications for TBFT
================================================================================

PRECONDITION
------------
The runner is constructed with an additional dependency: a *tbft.Controller
and *tbft.Scheduler instantiated in NewProposerRunner. These wrap the
TBFT-adapter machinery built in this package.

Construction (in NewProposerRunner, around line 78 of proposer.go):

	tbftCtrl, err := NewController(ControllerOptions{
	    OperatorID:    opts.CommitteeMember.OperatorID,
	    Committee:     committeeIDs(opts.CommitteeMember.Committee),
	    ClusterID:     deriveClusterID(opts.Share.ValidatorPubKey),
	    ClusterPubKey: opts.Share.ValidatorPubKey[:],
	    PubKeyShares:  buildPubKeyShares(opts.Share),
	    Signer:        blsbackend.New(),
	    IBE:           blsbackend.NewSignerGatedIBE(blsSignerInstance, opts.Share.ValidatorPubKey[:]),
	    Overrides:     nil, // use defaults; tune per cluster later
	})
	if err != nil { return nil, err }

	tbftHooks := &LifecycleHooks{
	    FetchCandidate: r.tbftFetchCandidate,
	    Broadcast:      r.tbftBroadcast,
	    SubmitOutput:   r.tbftSubmitOutput,
	    OnMissedSlot:   r.tbftOnMissedSlot,
	}
	tbftSched, err := NewScheduler(tbftCtrl, tbftHooks, opts.Share.SharePubKey.Serialize())
	if err != nil { return nil, err }

	r.tbftCtrl  = tbftCtrl
	r.tbftSched = tbftSched
	r.tbftRL    = NewRateLimiter()

(Once Option B real-IBE lands, replace SignerGatedIBE with TLockIBE.)


PHASE BOUNDARY 1: StartNewDuty
------------------------------
The existing flow at line 105 does:
	1. r.BaseRunner.baseStartNewDuty(...) — generic prep
	2. r.executeDuty(...) — pre-consensus + RANDAO + block fetch + QBFT decide

Replace step 2's "QBFT decide" with:

	if err := r.tbftStartSlot(ctx, logger, duty.Slot()); err != nil {
	    return err
	}

Where tbftStartSlot is:

	func (r *ProposerRunner) tbftStartSlot(ctx context.Context, logger *zap.Logger, slot phase0.Slot) error {
	    // RandaO pre-consensus REMAINS: still need to sign & collect
	    // 2f+1 RANDAO partials before block-fetch. Existing executeDuty
	    // logic at lines 470-519 stays.

	    slotStart := r.GetBeaconNetwork().EstimatedTimeAtSlot(slot)

	    // Drive TBFT in a separate goroutine so this method returns
	    // immediately to the runner's existing dispatch loop. The
	    // goroutine exits when the slot's TBFT instance ends.
	    go func() {
	        ctx, cancel := context.WithDeadline(ctx, slotStart.Add(12*time.Second))
	        defer cancel()
	        if err := RunProposerSlot(ctx, r.tbftCtrl, r.tbftSched, slot, slotStart); err != nil {
	            logger.Warn("TBFT slot failed", zap.Uint64("slot", uint64(slot)), zap.Error(err))
	        }
	        r.tbftRL.Forget(slot)
	    }()
	    return nil
	}


PHASE BOUNDARY 2: Hooks (replacing block-fetch + post-consensus)
-----------------------------------------------------------------
The Scheduler's hooks call back into runner methods that wrap existing
SSV infrastructure:

	// FetchCandidate: invoked when this operator is a layer leader.
	// Reuses existing block-fetch path; ignores the layer index because
	// each layer fetches the same kind of block (the runner can use the
	// layer to vary fetch timing/source if desired).
	func (r *ProposerRunner) tbftFetchCandidate(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
	    // Existing pre-consensus + RANDAO partial-sig collection must
	    // already be complete; this hook fires after the FetchAt
	    // offset, which is set so RANDAO has time to finish first.
	    fullSig := r.assembleRandaoSig(slot) // existing helper (see line 159)
	    proposal, err := r.GetBeaconNode().GetBeaconBlock(ctx, slot, r.graffiti, fullSig)
	    if err != nil { return nil, err }
	    return blindutil.EnsureBlinded(proposal).MarshalSSZ()
	}

	// Broadcast: SSV's existing message-publish path.
	func (r *ProposerRunner) tbftBroadcast(ctx context.Context, slot phase0.Slot, data []byte) error {
	    msgID := newSSVMessageID(MsgTypeTBFT, r.MsgIDForSlot(slot))
	    ssvMsg := &spectypes.SSVMessage{
	        MsgType: MsgTypeTBFT, // SSV-internal MsgType, NOT in spec
	        MsgID:   msgID,
	        Data:    data,
	    }
	    signed, err := r.GetOperatorSigner().SignSSVMessage(ssvMsg)
	    if err != nil { return err }
	    return r.GetNetwork().Broadcast(msgID, signed)
	}

	// SubmitOutput: replaces ProcessPostConsensus + beacon submit.
	func (r *ProposerRunner) tbftSubmitOutput(ctx context.Context, slot phase0.Slot, output *tbft.Output) error {
	    // Doppelganger / slashing-protection check — must run BEFORE
	    // submitting (existing CanSign() check at line 272).
	    if err := r.checkDoppelganger(); err != nil { return err }

	    // Output.Value is the SSZ-encoded blinded block; Output.Signature
	    // is the full reconstructed validator BLS sig on the block-root.
	    // Submit identically to how ProcessPostConsensus does today
	    // (lines 405-409).
	    return r.GetBeaconNode().SubmitBeaconBlock(ctx, output.Value, output.Signature)
	}

	// OnMissedSlot: log, emit a metric, mark the duty as missed.
	func (r *ProposerRunner) tbftOnMissedSlot(ctx context.Context, slot phase0.Slot, reason error) {
	    metricsTBFTMissedSlot.WithLabelValues(...).Inc()
	    r.GetLogger().Warn("TBFT slot missed", zap.Uint64("slot", uint64(slot)), zap.Error(reason))
	}


PHASE BOUNDARY 3: Network receive path
--------------------------------------
SSV's network layer dispatches by MsgType. Add:

	switch ssvMsg.MsgType {
	case MsgTypeTBFT:
	    // Parse the embedded operator-signed TBFT envelope.
	    env, err := wire.Unwrap(ssvMsg.Data)
	    if err != nil { return err }

	    // Rate-limit before delivering to the controller.
	    senderID := signedMsg.GetOperatorIDs()[0]
	    switch env.Kind {
	    case wire.KindOnion:
	        if err := r.tbftRL.AllowOnion(slot, senderID); err != nil { return err }
	    case wire.KindNonReceipt:
	        if err := r.tbftRL.AllowNonReceipt(slot, senderID, env.NonReceipt.Layer); err != nil { return err }
	    case wire.KindCandidate:
	        if err := r.tbftRL.AllowCandidate(slot, senderID, env.Candidate.Layer); err != nil { return err }
	    }

	    // Dispatch to the right Process* method on the controller.
	    return DispatchEnvelope(r.tbftCtrl, env)

	case spectypes.SSVConsensusMsgType, spectypes.SSVPartialSignatureMsgType:
	    // Existing QBFT path — keep until rollout completes.
	    return r.processQBFTPath(...)
	}


REMOVALS
--------
With TBFT active, several pieces of ProposerRunner go away:

  - r.QBFTController            — replaced by r.tbftCtrl
  - r.ProcessConsensus(...)     — TBFT has no separate consensus phase exposed
                                  to the runner; the Controller absorbs it
  - r.ProcessPostConsensus(...) — TBFT folds post-consensus into Phase 3
  - The post-consensus partial-sig container and reconstruction code
    (lines ~566–630 in proposer.go) — `tbft.Output.Signature` is the full
    sig directly

Keep:

  - RANDAO pre-consensus (executeDuty + ProcessPreConsensus, lines 105–228)
  - Doppelganger/CanSign checks (lines 272-275, called from tbftSubmitOutput)
  - Beacon-node submit logic (lines 405-409, called from tbftSubmitOutput)
  - All metrics, tracing, logging, MarshalJSON, etc.


COEXISTENCE WITH QBFT
---------------------
For safe rollout, gate the TBFT path on a per-cluster registry flag:

	if r.cluster.UsesTBFT() {
	    r.tbftStartSlot(ctx, logger, slot)
	} else {
	    r.executeDuty(ctx, logger, duty)  // existing QBFT path
	}

This lets old clusters continue on QBFT until they re-register with
TBFT explicitly. Mixed clusters (some operators on TBFT-binary, some
on QBFT-only) are not safe — operator deployment must hard-prevent
this (registry-side enforcement, refusing to start a cluster where
operator binary versions advertise different protocol versions).


WHAT THIS SKETCH DOESN'T ADDRESS
--------------------------------
- Real `drand/tlock` IBE (Option B refactor — see docs/IBE-INTEGRATION.md).
  Until that lands, the IBE is `SignerGatedIBE` (functionally correct
  but no cryptographic confidentiality).
- The MsgType value for TBFT messages and registration in SSV's network
  routing tables. Coordinate with SSV team for an unused MsgType byte.
- Metrics nomenclature, log keys, dashboard updates. Integrate with
  SSV's existing observability conventions.
- Spec-test integration. Per the user's Phase 0 decision, TBFT is
  independent of ssv-spec; spec tests are not added.

*/
