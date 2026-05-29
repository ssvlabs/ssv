package base

import (
	"bytes"
	"fmt"
)

// Resolve runs the Phase-3 reconstruction walk (spec §Phase 3).
//
// At each layer, the cluster has three possible outcomes:
//   - σ-quorum reaches on some V (output produced).
//   - NR-quorum reaches (advance to next layer's chained-decryption).
//   - Neither reaches (slot misses).
//
// At layer 0, σ partials are plaintext. The σ-pool at L_0 includes the
// leader's Phase-1 σ_V (head start from spec §Phase 1) when retained.
//
// At layers k > 0, σ partials are chained-encrypted under nr_tag_0..nr_tag_{k-1}.
// The walk accumulates aggregated NR sigs from prior layers as it advances,
// using them to chain-decrypt the next layer's onion entries.
//
// Returns:
//   - (*Output, nil)  — σ-quorum reached at some layer; output is the decided
//     V + reconstructed full BLS signature.
//   - (nil, ErrNoQuorum) — exhausted all K layers without σ-quorum or
//     NR-quorum sufficient to advance.
//   - (nil, error) — internal error (malformed input, crypto failure).
//
// As a side effect, Rule-4 evidence (fake encrypted-presence at k > 0) is
// recorded for any peer whose Onion entry decrypts to garbage.
//
// Resolve is stateless / idempotent — re-running on late KindCommit
// arrivals incorporates additional contributions without contradicting
// prior outcomes (Pigeonhole semantics still hold). The canonical
// implementation calls Resolve opportunistically on every state delta
// (KindCommit / KindCertificate observation) starting at T_commit, not
// once at T_commit + Δ_2 — Δ_2 is the propagation budget, not a Resolve
// gate. ErrNoQuorum on incomplete state is returned cleanly without
// mutating Instance state, so observer-mode call sites pay no cost for
// pre-quorum attempts.
func (i *Instance) Resolve() (*Output, error) {
	if i == nil {
		return nil, fmt.Errorf("obft: nil instance")
	}
	if i.ended {
		return nil, ErrInstanceEnded
	}
	K := i.cfg.K()
	// chainedKeys[j] is the aggregated NR-partials sig on nr_tag_j. To
	// decrypt a layer-k onion entry, we apply chainedKeys[0..k-1] in order
	// (outermost-first).
	chainedKeys := make([][]byte, K)

	// Reset the per-layer trace for this Resolve call. Resolve is
	// idempotent / re-runnable — the trace reflects the LATEST call's
	// walk state, not an accumulation across calls.
	i.lastResolveTrace = i.lastResolveTrace[:0]
	qV := i.cfg.QV()
	qEnc := i.cfg.QEnc()

	for k := 0; k < K; k++ {
		// Try σ-pool reconstruction at this layer.
		out, sigmaPoolSize, err := i.tryReconstructLayer(k, chainedKeys)
		attempt := LayerAttempt{
			Layer:         k,
			SigmaPoolSize: sigmaPoolSize,
			QV:            qV,
			SigmaReached:  sigmaPoolSize >= qV,
			Decided:       out != nil,
		}
		if err != nil {
			// Record the attempt with whatever σ-pool state we have
			// before the error so debugging tools see how far the
			// walk got before the crypto failure.
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			return nil, fmt.Errorf("obft: layer %d reconstruction: %w", k, err)
		}
		if out != nil {
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			return out, nil
		}

		// σ-quorum did not reach. Try NR-quorum to advance to next layer.
		if k == K-1 {
			// Deepest layer: no NR tag to aggregate, so the attempt's
			// NR fields stay zero. Record + break to fall through to
			// the exhaustion-error return below.
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			break
		}
		nextKey, nrPoolSize, err := i.tryDeriveNextLayerKey(k)
		attempt.NRPoolSize = nrPoolSize
		attempt.QEnc = qEnc
		// NRReached reflects "NR-pool reached qEnc cluster-wide", not
		// "aggregation key was successfully derived". Under crypto
		// failure (poolSize ≥ qEnc but AggregatePartials errors)
		// nextKey is nil yet the quorum WAS reached — match the
		// LayerAttempt docstring's NRPoolSize >= QEnc semantic.
		attempt.NRReached = nrPoolSize >= qEnc
		i.lastResolveTrace = append(i.lastResolveTrace, attempt)
		if err != nil {
			return nil, fmt.Errorf("obft: layer %d NR aggregation: %w", k, err)
		}
		if nextKey == nil {
			// Neither σ-quorum at k nor NR-quorum to advance. Stuck —
			// the HV1-style deadlock pathology; surface the layer so
			// upstream classifiers can distinguish it from the
			// walked-all-K-layers exhaustion case below.
			return nil, &ResolveError{StoppedAtLayer: k, Reason: ResolveFailureDeadlock}
		}
		chainedKeys[k] = nextKey
	}
	// Reached this point only by completing every layer's σ-attempt
	// (and, for layers 0..K-2, every NR-fallthrough) without producing
	// σ-quorum anywhere. Cluster never assembled a threshold signature.
	return nil, &ResolveError{StoppedAtLayer: K - 1, Reason: ResolveFailureExhaustion}
}

// LastResolveLayerAttempts returns a snapshot of the most recent
// successful (or partial-walk) Resolve() call's per-layer state. Each
// entry records that layer's σ-pool / NR-pool sizes, quorum thresholds,
// whether each side reached, and whether the walk decided at this
// layer.
//
// Returns nil iff no prior Resolve has populated the trace. A
// subsequent Resolve on an already-ended Instance returns
// ErrInstanceEnded BEFORE the per-call trace-reset, so the trace
// from the last pre-ended walk survives — this is the intended
// behavior (caller's last-snapshot view of "how did the final walk go"
// remains valid even if the Instance was ended between then and the
// LastResolveLayerAttempts call). The returned slice is owned by the
// caller — Instance.Resolve overwrites its backing array on the next
// non-ended Resolve call, so callers must copy if they need to retain
// across multiple Resolves.
//
// Used by the consensustest framework's bucket-3 walk-consistency
// invariant (docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md); production
// runner paths are free to ignore it.
func (i *Instance) LastResolveLayerAttempts() []LayerAttempt {
	if i == nil {
		return nil
	}
	return i.lastResolveTrace
}

// sigGroup holds σ partials grouped by the V they sign at a given layer.
// Multiple groups indicate cross-leader / cross-onion equivocation; each
// group is counted independently per Pigeonhole 2.
type sigGroup struct {
	value    Value
	partials map[OperatorID]Signature
}

// tryReconstructLayer attempts σ-quorum reconstruction at `layer`.
//
// Returns:
//   - (*Output, poolSize, nil)    — σ-quorum reached, output produced.
//   - (nil, poolSize, nil)        — no σ-quorum (caller should attempt NR-advance).
//   - (nil, poolSize, error)      — internal error (e.g. crypto failure).
//
// `poolSize` is the largest sigGroup's distinct-emitter count at this
// layer (the V most likely to reach qV cluster-wide; Pigeonhole 2
// bounds the cluster-wide answer to at most one V). Zero when no
// partials were observed at this layer. Used by Resolve to populate
// the per-layer trace (Instance.lastResolveTrace).
//
// `chainedKeys` carries aggregated NR sigs from prior layers; only the
// first `layer` entries are used (chainedKeys[0..layer-1]).
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, int, error) {
	groups := make([]*sigGroup, 0, 1)

	// 1) Leader's Phase-1 σ_V partials at this layer. The retention bound
	//    of 2 distinct V's per (layer, leader_id) means up to 2 entries
	//    can be present under leader equivocation; each counts as one
	//    partial in its respective V's group. Pigeonhole 2 ensures only
	//    one V can reach qV cluster-wide regardless of split.
	leaderID := i.cfg.Layers[layer].Leader
	// NewInstance ensures every layer's leader has a registered pub-share.
	pubShare := i.pubKeyShares[leaderID]
	for _, b := range i.bundles[layer][leaderID] {
		// F1: verifyOrCached hits the verify-cache populated at retention
		// (phase1.go ObservePhase1Bundle) for the common path; falls through
		// to a fresh signer.VerifyPartial on miss. Equivalent semantics to
		// the previous unconditional signer.VerifyPartial call.
		if i.verifyOrCached(leaderID, layer, pubShare, b.Value, b.LeaderSigma) {
			addToGroup(&groups, b.Value, leaderID, b.LeaderSigma)
		}
	}

	// 1b) Leader's σ_V harvested from peer KindCommit witness sections
	//     (spec §Phase 2 wire format). Plaintext at every layer (not
	//     chained-encrypted); contributes to σ-pool when the leader's
	//     bundle never reached this operator but V is known locally via a
	//     peer's σ-onion entry. addToGroup dedups per (operator, V), so
	//     adding the same leader's partial twice (once via bundle, once
	//     via witness — byte-identical for honest leaders) collapses to
	//     one partial in the pool.
	//
	//     Pigeonhole-2 invariant: under leader equivocation, both V_a and
	//     V_b can have a leader-σ contribution locally (one entry each in
	//     witnessedLeaderSigma); cluster-wide only one V reaches qV
	//     because the leader's single partial counts toward exactly one
	//     V's pool at any honest receiver. Local transient pre-quorum
	//     states may show non-trivial counts in both groups; the lex-
	//     tiebreak on Value below makes the eventual "winner" deterministic
	//     across operators if both groups ever reach qV simultaneously.
	for _, ws := range i.witnessedLeaderSigma[layer] {
		addToGroup(&groups, ws.Value, leaderID, ws.Sigma)
	}

	// 2) Onion contributions. Decrypt at layers > 0 using accumulated
	//    chained keys. F4: cache-miss σ partials at L_k > 0 are collected
	//    into a single batch and verified via one MultiVerify pairing
	//    equation (helpers below). L_0 entries are pre-verified at
	//    observation (phase2.go peerSigmaAtL0Verdict) so F1's cache always
	//    hits and they take the inline addToGroup path — never enter the
	//    batch. On batch failure we fall back to per-tuple verify to
	//    preserve Rule-4 attribution per (op, layer).
	var pending []pendingVerify
	for opID, entries := range i.peerOnions[layer] {
		// Skip the layer's leader — Phase-1 σ_V already counted above.
		if opID == leaderID {
			continue
		}
		for _, el := range entries {
			var partial Signature
			if layer == 0 {
				partial = Signature(el.Ciphertext)
			} else {
				pt, err := i.chainDecryptForLayer(layer, el.Ciphertext, chainedKeys)
				if err != nil {
					// Decryption failure at k > 0 is Rule 4 evidence.
					// Deduplicated per (op, layer): a byzantine that emits
					// multiple distinct onion entries at the same layer is
					// already attributed via Rule 3 (cross-onion equivocation)
					// — re-firing Rule 4 per entry would inflate the evidence
					// log without adding attribution.
					if i.recordRule4(opID, layer) {
						i.recordEvidence(Evidence{
							Rule:       EvidenceFakeEncryptedPresence,
							OperatorID: opID,
							Layer:      layer,
							FakeEncryptedPresence: &FakeEncryptedPresenceEvidence{
								Ciphertext:   append([]byte{}, el.Ciphertext...),
								DecryptError: err.Error(),
							},
						})
					}
					continue
				}
				partial = Signature(pt)
			}

			pubShare := i.pubKeyShares[opID]
			if pubShare == nil {
				continue
			}
			// F1: cache hit → add directly, skip the batch. At L_0 this is
			// always the case (observe-time populate). At L_k > 0 this is
			// the warm path on opportunistic re-Resolves.
			if i.alreadyVerified(opID, layer, el.Value, partial) {
				addToGroup(&groups, el.Value, opID, partial)
				continue
			}
			// F4: cache miss — collect for the batch. Only reachable at
			// L_k > 0 in practice (L_0's cache is always populated at
			// observe time). The capture of el.Ciphertext is needed for
			// the Rule-4 evidence path when the batch falls back to
			// sequential.
			pending = append(pending, pendingVerify{
				op:         opID,
				pubShare:   pubShare,
				value:      el.Value,
				partial:    partial,
				ciphertext: el.Ciphertext,
			})
		}
	}

	// F4: batch-verify all collected cache-miss tuples in one MultiVerify
	// call. On batch success: cache-populate + addToGroup for every tuple.
	// On batch failure: fall back to per-tuple verify to attribute Rule-4
	// evidence per failing (op, layer). Empty pending list = no batch call
	// (the L_0-only cache-hit common path).
	if len(pending) > 0 {
		if !i.batchVerifyAndPopulate(layer, pending, &groups) {
			i.sequentialVerifyAndAttribute(layer, pending, &groups)
		}
	}

	// 3) Pick the group with the most partials; check qV.
	winning := selectWinningGroup(groups)
	poolSize := 0
	if winning != nil {
		poolSize = len(winning.partials)
	}
	if winning == nil || poolSize < i.cfg.QV() {
		return nil, poolSize, nil
	}

	full, err := i.signer.AggregatePartials(winning.partials)
	if err != nil {
		return nil, poolSize, fmt.Errorf("aggregate σ partials: %w", err)
	}
	return &Output{
		Layer:     layer,
		Value:     append(Value{}, winning.value...),
		Signature: full,
	}, poolSize, nil
}

// tryDeriveNextLayerKey aggregates qEnc NR partials on nr_tag_layer.
//
// Returns:
//   - (key, poolSize, nil) — NR-quorum reached; key is the aggregated
//     full sig (the chained-decryption key for layer+1's outermost wrap).
//   - (nil, poolSize, nil) — NR-quorum did not reach.
//   - (nil, poolSize, error) — internal error (e.g. crypto failure).
//
// poolSize is the count of distinct-emitter NR partials observed at this
// layer; used by Resolve to populate the per-layer trace.
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, int, error) {
	partials := i.peerNR[layer]
	poolSize := len(partials)
	if poolSize < i.cfg.QEnc() {
		return nil, poolSize, nil
	}
	full, err := i.tagSigner.AggregatePartials(partials)
	if err != nil {
		return nil, poolSize, err
	}
	return []byte(full), poolSize, nil
}

func addToGroup(groups *[]*sigGroup, value Value, opID OperatorID, partial Signature) {
	for _, g := range *groups {
		if bytes.Equal(g.value, value) {
			g.partials[opID] = partial
			return
		}
	}
	g := &sigGroup{
		value:    append(Value{}, value...),
		partials: map[OperatorID]Signature{opID: partial},
	}
	*groups = append(*groups, g)
}

// BuildCertificate produces the final-certificate gossip message after a
// successful Resolve.
//
// Per spec §Final-certificate gossip, an operator that reconstructed (V, S)
// gossips the certificate so that other operators (including those that
// failed to reconstruct locally) can submit (V, S) downstream — protecting
// against the lone-reconstructor's beacon path failing.
func (i *Instance) BuildCertificate(out *Output) (*Certificate, error) {
	if i == nil {
		return nil, fmt.Errorf("obft: nil instance")
	}
	if out == nil {
		return nil, fmt.Errorf("obft: nil output")
	}
	if len(out.Value) == 0 || len(out.Signature) == 0 {
		return nil, fmt.Errorf("obft: empty value or signature")
	}
	return &Certificate{
		ClusterID: i.cfg.ClusterID,
		Height:    i.cfg.Height,
		Value:     append(Value{}, out.Value...),
		Signature: append(Signature{}, out.Signature...),
	}, nil
}

// ObserveCertificate records a peer's KindCertificate. The signature is
// verified against the cluster's V-keypair pubkey on Value; valid certificates
// are stored for the runner to use as an alternative submission path
// (RetainedCertificate accessor).
//
// Per spec, receivers SHOULD re-run host-application validity on Value before
// submitting downstream — that's a host concern, not in this method's scope.
func (i *Instance) ObserveCertificate(c *Certificate) error {
	if i == nil {
		return fmt.Errorf("obft: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidateCertificate(c, i.cfg); err != nil {
		return err
	}
	if !i.signer.VerifyAggregate(i.clusterPubKey, c.Value, c.Signature) {
		return fmt.Errorf("obft: certificate signature does not verify against cluster pubkey")
	}
	if i.receivedCertificate == nil {
		// Deep copy: detach the cert's slice fields from the caller's bytes
		// so retention is robust even if the caller mutates / reuses them.
		i.receivedCertificate = &Certificate{
			ClusterID: c.ClusterID,
			Height:    c.Height,
			Value:     append(Value{}, c.Value...),
			Signature: append(Signature{}, c.Signature...),
		}
	}
	return nil
}

// RetainedCertificate returns a deep copy of the peer-broadcast certificate
// previously observed via ObserveCertificate, or nil if none.
func (i *Instance) RetainedCertificate() *Certificate {
	if i.receivedCertificate == nil {
		return nil
	}
	src := i.receivedCertificate
	return &Certificate{
		ClusterID: src.ClusterID,
		Height:    src.Height,
		Value:     append(Value{}, src.Value...),
		Signature: append(Signature{}, src.Signature...),
	}
}

// pendingVerify carries a σ-walk entry that missed F1's verify cache and
// needs a fresh BLS verify. tryReconstructLayer collects these into a
// batch (F4) and either approves the whole set via VerifyPartialBatch +
// cache-populate, or falls back to per-tuple verify to attribute Rule-4
// evidence on the byzantine entries.
//
// ciphertext is captured for the Rule-4 evidence path in the sequential
// fallback — the same FakeEncryptedPresenceEvidence shape the pre-F4 code
// fired inline. value and partial are the post-decryption tuple the cache
// key binds.
type pendingVerify struct {
	op         OperatorID
	pubShare   []byte
	value      Value
	partial    Signature
	ciphertext []byte
}

// batchVerifyAndPopulate runs one VerifyPartialBatch over the pending
// tuples. On success: every tuple is cache-populated and added to its
// sigGroup; returns true. On failure: returns false without touching the
// cache or groups — the caller (tryReconstructLayer) then runs
// sequentialVerifyAndAttribute to identify the bad tuple(s).
//
// F4. The batch returns false only
// if at least one tuple would have failed individually; the
// random-linear-combination security argument is at the same level as
// N independent VerifyPartial calls. On success the cache-populate is
// safe per F1's invariant ("populate only on verify-success").
func (i *Instance) batchVerifyAndPopulate(layer int, pending []pendingVerify, groups *[]*sigGroup) bool {
	pubs := make([][]byte, len(pending))
	msgs := make([][]byte, len(pending))
	sigs := make([]Signature, len(pending))
	for k, pv := range pending {
		pubs[k] = pv.pubShare
		msgs[k] = pv.value
		sigs[k] = pv.partial
	}
	if !i.signer.VerifyPartialBatch(pubs, msgs, sigs) {
		return false
	}
	for _, pv := range pending {
		i.markVerified(pv.op, layer, pv.value, pv.partial)
		addToGroup(groups, pv.value, pv.op, pv.partial)
	}
	return true
}

// sequentialVerifyAndAttribute is the per-tuple fallback after a batch
// verify failed. For each tuple that verifies individually: cache-populate
// + addToGroup. For each tuple that fails individually at L_k > 0: record
// Rule-4 evidence (same EvidenceFakeEncryptedPresence shape + same
// recordRule4 per-(op, layer) dedup as the inline pre-F4 code path; at
// L_0 this branch is unreachable since L_0 entries always cache-hit and
// never enter pending).
//
// This preserves the safety invariant that every byzantine σ partial gets
// per-(op, layer) Rule-4 attribution. Without it, a single bad sig in
// the batch would fail the whole batch silently, dropping attribution
// for everyone.
func (i *Instance) sequentialVerifyAndAttribute(layer int, pending []pendingVerify, groups *[]*sigGroup) {
	for _, pv := range pending {
		if i.signer.VerifyPartial(pv.pubShare, pv.value, pv.partial) {
			i.markVerified(pv.op, layer, pv.value, pv.partial)
			addToGroup(groups, pv.value, pv.op, pv.partial)
			continue
		}
		if layer > 0 {
			if i.recordRule4(pv.op, layer) {
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakeEncryptedPresence,
					OperatorID: pv.op,
					Layer:      layer,
					FakeEncryptedPresence: &FakeEncryptedPresenceEvidence{
						Ciphertext:     append([]byte{}, pv.ciphertext...),
						DecryptedBytes: append([]byte{}, pv.partial...),
					},
				})
			}
		}
	}
}

// selectWinningGroup picks the σ-group with the most partials at this
// layer, breaking ties lexicographically on V. Tiebreak determinism is
// load-bearing: without it, two groups with equal partial counts would
// resolve based on map-iteration order (groups is built from
// peerOnions[layer] which is a map), producing nondeterministic Output
// across operators on transient pre-quorum states. Pigeonhole 2
// guarantees only one V reaches qV cluster-wide given f-bound, but
// locally we can transiently observe two V's at equal-count below qV;
// deterministic tiebreak makes the "winner" identical across operators
// if both ever reach qV.
//
// Returns nil if groups is empty.
func selectWinningGroup(groups []*sigGroup) *sigGroup {
	var winning *sigGroup
	for _, g := range groups {
		if winning == nil {
			winning = g
			continue
		}
		switch {
		case len(g.partials) > len(winning.partials):
			winning = g
		case len(g.partials) == len(winning.partials) && bytes.Compare(g.value, winning.value) < 0:
			winning = g
		}
	}
	return winning
}
