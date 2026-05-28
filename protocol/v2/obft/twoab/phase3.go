package twoab

import (
	"bytes"
	"fmt"
)

// Resolve runs the Phase-3 reconstruction walk per spec §Phase 3.
//
// At each layer the cluster has three possible outcomes:
//   - σ-quorum reaches on some V → output produced; walk halts.
//   - NR-quorum reaches → aggregate forms the chained-decryption key
//     for the next layer; walk advances.
//   - Neither reaches → walk terminates without an output (slot misses).
//
// At layer 0, σ partials are plaintext in KindValue.L0Partial and the
// leader's LeaderSigma in retained Phase-1 bundles. At layers k > 0, the
// layer leader's LeaderSigma is likewise plaintext in its retained bundle
// (a head-start), while every other op's σ partial is chained-encrypted
// inside Phase-2a emissions (ValueMsg / NoValueMsg /
// Commit-NRDirect LayerEntries with Kind=SigmaChained); the accumulated
// NR-quorum aggregates from prior layers serve as decryption keys
// (outermost-first).
//
// Returns:
//   - (*Output, nil) — σ-quorum reached at some layer.
//   - (nil, ErrNoQuorum) — walked all reachable layers without σ-quorum;
//     NR-quorum either failed at some intermediate layer (sealing the
//     remaining chain) or the walk reached K-1 without σ-quorum.
//   - (nil, error) — internal error (crypto failure on aggregation).
//
// As a side effect, Rule-4 evidence (fake encrypted-presence at k > 0)
// is recorded for any peer whose Phase-2a LayerEntry decrypts to garbage
// or fails to decrypt. Rule 4 is per-(op, layer) deduped — multiple
// distinct entries from the same byzantine at the same layer surface once.
//
// Resolve is stateless / idempotent — re-running on late Commit arrivals
// incorporates additional contributions without contradicting prior
// outcomes (Pigeonhole semantics still hold). The canonical implementation
// calls Resolve opportunistically on every state delta starting at the
// Phase-2a fire-time; ErrNoQuorum on incomplete state is returned cleanly
// without mutating Instance state, so observer-mode call sites pay no
// cost for pre-quorum attempts.
func (i *Instance) Resolve() (*Output, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return nil, ErrInstanceEnded
	}
	K := i.cfg.K()
	// chainedKeys[j] is the aggregated NR-partials sig on nr_tag_j.
	// To decrypt a layer-k onion entry, apply chainedKeys[0..k-1] in
	// order (outermost-first).
	chainedKeys := make([][]byte, K)

	// Reset the per-layer trace for this Resolve call (see Instance
	// lastResolveTrace docstring for the snapshot semantics).
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
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			return nil, fmt.Errorf("twoab: layer %d reconstruction: %w", k, err)
		}
		if out != nil {
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			return out, nil
		}

		// σ-quorum did not reach. Try NR-quorum to advance to next layer.
		// At the deepest layer there is no NR tag — the walk terminates.
		if k == K-1 {
			i.lastResolveTrace = append(i.lastResolveTrace, attempt)
			break
		}
		nextKey, nrPoolSize, err := i.tryDeriveNextLayerKey(k)
		attempt.NRPoolSize = nrPoolSize
		attempt.QEnc = qEnc
		// See obft/base/phase3.go for the rationale: NRReached
		// reflects "NR-pool reached qEnc cluster-wide", not "aggregation
		// key was derived". Crypto failure leaves nextKey=nil but the
		// quorum WAS reached.
		attempt.NRReached = nrPoolSize >= qEnc
		i.lastResolveTrace = append(i.lastResolveTrace, attempt)
		if err != nil {
			return nil, fmt.Errorf("twoab: layer %d NR aggregation: %w", k, err)
		}
		if nextKey == nil {
			// Neither σ-quorum at k nor NR-quorum to advance. Stuck;
			// surface the layer so upstream classifiers can distinguish
			// this from the walked-all-K-layers exhaustion case below.
			return nil, &ResolveError{StoppedAtLayer: k, Reason: ResolveFailureDeadlock}
		}
		chainedKeys[k] = nextKey
	}
	return nil, &ResolveError{StoppedAtLayer: K - 1, Reason: ResolveFailureExhaustion}
}

// LastResolveLayerAttempts returns a snapshot of the most recent
// Resolve() walk's per-layer state. Mirrors obft/base's getter — same
// semantics and caller contract (slice owned by the caller; overwritten
// on next Resolve). Consumed by the consensustest framework's bucket-3
// walk-consistency invariant.
func (i *Instance) LastResolveLayerAttempts() []LayerAttempt {
	if i == nil {
		return nil
	}
	return i.lastResolveTrace
}

// tryReconstructLayer attempts σ-quorum reconstruction at `layer`.
//
// Returns:
//   - (*Output, poolSize, nil) — σ-quorum reached, output produced.
//   - (nil, poolSize, nil) — no σ-quorum (caller should attempt NR-advance).
//   - (nil, poolSize, error) — internal error (e.g., aggregation crypto failure).
//
// poolSize is the largest sigGroup's distinct-emitter count at this
// layer; mirrors the OBFT base helper for symmetry with the Resolve
// trace.
//
// At L_0: σ partials come from sigmaPool[0]. Populated via
// verifyAndPoolL0Partial in phase2a.go from peer ValueMsg.L0Partial (the
// emitter's plaintext σ partial), plus the L_0 leader's Phase1Bundle
// LeaderSigma contribution and any harvested forwarded-witness contributions
// from peer-reflood KindValue. Build groups by V_root (typically
// one group, but cross-σ-V equivocation could produce more).
//
// At L_k > 0: σ partials come from two sources combined in
// sigmaPool[k] / groups: (a) the layer leader's plaintext LeaderSigma, seeded
// at Phase-1 bundle observation (a head-start; 1 < qV so the witness alone
// can't reach σ-quorum); and (b) Phase-2a SigmaChained entries inside
// peerValueMsg / peerNoValueMsg / peerCommit (Side=NRDirect), whose
// ciphertext payloads are decrypted via chainedKeys (outermost-first). On
// decryption failure or post-decryption verification failure, Rule 4 fires.
// The leader's plaintext witness and its own decrypted chained entry (if
// any) key the same OperatorID, so they coalesce — no double-count.
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, int, error) {
	groups := make(map[[32]byte]*sigGroup)

	// 1) σ-pool entries already populated for this layer.
	if pools, ok := i.sigmaPool[layer]; ok {
		for vRoot, opPartials := range pools {
			for op, partial := range opPartials {
				g := groups[vRoot]
				if g == nil {
					// Reconstruct V from peer messages; we have the root
					// but need the bytes. For L_0 sigmaPool, the
					// V is in ownValueMsg.V / peerValueMsg[op].V (with a
					// retainedBundles[0][leader] fallback when no KindValue
					// has been observed yet but the leader's LeaderSigma has
					// already contributed to sigmaPool). For L_k>0 sigmaPool,
					// the V is in the original LayerEntry. Locate it via the
					// helper.
					v, ok := i.recoverV(layer, vRoot)
					if !ok {
						// Shouldn't happen if sigmaPool entries were
						// populated correctly. Skip defensively.
						continue
					}
					g = &sigGroup{value: append(Value{}, v...), partials: map[OperatorID]Signature{}}
					groups[vRoot] = g
				}
				g.partials[op] = partial
			}
		}
	}

	// 2) At L_k>0: walk the peer messages, decrypt SigmaChained entries,
	//    and add to groups.
	if layer > 0 {
		i.aggregatePeerLayerEntries(layer, chainedKeys, groups)
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

// sigGroup holds σ partials grouped by the V they sign at a given layer.
type sigGroup struct {
	value    Value
	partials map[OperatorID]Signature
}

// aggregatePeerLayerEntries walks all peer Phase-2a emissions and
// extracts σ partials at `layer` (decrypting via chainedKeys), adding
// them to `groups` keyed by V_root. Fires Rule 4 evidence on decryption
// failure or post-decryption verification failure.
//
// F4: cache-miss σ partials are collected into a single batch and verified
// via one VerifyPartialBatch call after the three peer-store loops finish.
// L_0 entries are pre-verified at observation and always cache-hit so they
// take the inline addToGroup path; the batch fires only on L_k>0 first-walk
// cache misses. On batch failure we fall back to per-tuple verify to
// preserve Rule-4 attribution per (op, layer).
func (i *Instance) aggregatePeerLayerEntries(layer int, chainedKeys [][]byte, groups map[[32]byte]*sigGroup) {
	var pending []pendingVerify
	// Iterate peer ValueMsgs.
	for op, vm := range i.peerValueMsg {
		i.classifySigmaFromEntries(op, layer, vm.LayerEntries, chainedKeys, groups, &pending)
	}
	// Iterate peer NoValueMsgs.
	for op, nv := range i.peerNoValueMsg {
		i.classifySigmaFromEntries(op, layer, nv.LayerEntries, chainedKeys, groups, &pending)
	}
	// Iterate peer Commits with Side=NRDirect (they carry LayerEntries).
	for op, c := range i.peerCommit {
		if c.Side != CommitSideNRDirect {
			continue
		}
		i.classifySigmaFromEntries(op, layer, c.LayerEntries, chainedKeys, groups, &pending)
	}

	// F4: batch-verify all collected cache-miss tuples in one MultiVerify
	// call. On batch success: cache-populate + add-to-group for every tuple
	// in one pass. On batch failure: fall back to per-tuple verify to
	// attribute Rule-4 evidence per failing (op, layer). Empty pending list
	// = no batch call (the L_0-only cache-hit common path).
	if len(pending) > 0 {
		if !i.batchVerifyAndPopulate(layer, pending, groups) {
			i.sequentialVerifyAndAttribute(layer, pending, groups)
		}
	}
}

// classifySigmaFromEntries decrypts the SigmaChained entry at `layer` (if
// present) in `entries`, fires Rule-4 evidence on decryption failure, and
// either adds the decrypted partial to `groups` directly (F1 cache hit) or
// pushes it to `pending` for the F4 batch verify (cache miss). Returns
// without invoking the BLS verify itself — that's the batch's job.
//
// Only one SigmaChained entry per (op, layer) is expected by construction;
// the loop returns on the first match.
func (i *Instance) classifySigmaFromEntries(op OperatorID, layer int, entries []LayerEntry,
	chainedKeys [][]byte, groups map[[32]byte]*sigGroup, pending *[]pendingVerify) {
	for _, e := range entries {
		if e.Layer != layer || e.Kind != LayerEntrySigmaChained {
			continue
		}
		// Decrypt the chained payload. Decryption failure at k > 0 is Rule 4
		// evidence (post-NR-quorum the chain key is wrong → decrypted bytes
		// would be garbage). Same per-(op, layer) recordRule4 dedup as base.
		pt, err := i.chainDecryptForLayer(layer, e.Payload, chainedKeys)
		if err != nil {
			if i.recordRule4(op, layer) {
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakeEncryptedPresence,
					OperatorID: op,
					Layer:      layer,
					FakeEncryptedPresence: &FakeEncryptedPresenceEvidence{
						Ciphertext:   append([]byte{}, e.Payload...),
						DecryptError: err.Error(),
					},
				})
			}
			return
		}
		opPub, ok := i.pubKeyShares[op]
		if !ok || len(opPub) == 0 {
			return
		}
		// F1: cache hit → add directly, skip the batch. At L_0 this is the
		// common case (the cache was populated at observe time, see twoab's
		// L_0 σ-pool ingestion in phase2a.go); at L_k>0 it's the warm path
		// after the first Resolve walk. Value-binding in the cache key is
		// load-bearing — see verifyCacheKey doc in instance.go.
		if i.alreadyVerified(op, layer, e.V, Signature(pt)) {
			vRoot := ValueRoot(e.V)
			g := groups[vRoot]
			if g == nil {
				g = &sigGroup{value: append(Value{}, e.V...), partials: map[OperatorID]Signature{}}
				groups[vRoot] = g
			}
			g.partials[op] = append(Signature{}, pt...)
			return
		}
		// F4: cache miss — collect for the batch. ciphertext is captured for
		// the Rule-4 evidence path in the sequential fallback.
		*pending = append(*pending, pendingVerify{
			op:         op,
			pubShare:   opPub,
			value:      e.V,
			partial:    Signature(pt),
			ciphertext: e.Payload,
		})
		return
	}
}

// tryDeriveNextLayerKey aggregates ≥ qEnc NR partials on nr_tag_layer.
//
// Returns:
//   - (key, poolSize, nil) — NR-quorum reached; key is the aggregated
//     full signature (chained-decryption key for layer+1's outermost wrap).
//   - (nil, poolSize, nil) — NR-quorum did not reach.
//   - (nil, poolSize, error) — internal error.
//
// poolSize is the count of distinct-emitter NR partials observed at this
// layer; mirrors the OBFT base helper for the Resolve trace.
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, int, error) {
	partials := i.nrTagPool[layer]
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

// recoverV locates the V bytes corresponding to a given (layer, vRoot).
// Used by tryReconstructLayer to populate sigGroup.value from sigmaPool
// entries (which key by vRoot, not V bytes).
//
// At L_0: V comes from ownValueMsg or any peerValueMsg matching the root
// (KindValue is the σ-side terminal emission carrying the σ partial
// directly). Also from any retainedBundle at L_0 — the leader's bundle
// preserves V even if no peer KindValue carrying that V has arrived yet
// (e.g., harvest-only state where σ-pool was seeded from a peer's
// forwarded witness alone).
//
// At L_k>0: V comes from any SigmaChained LayerEntry (own or peer) at this
// layer matching the root, or from any retained bundle at this layer (the
// leader's plaintext LeaderSigma seeds σ-pool[k] before any chained entry
// decrypts).
func (i *Instance) recoverV(layer int, vRoot [32]byte) (Value, bool) {
	if layer == 0 {
		if i.ownValueMsg != nil && ValueRoot(i.ownValueMsg.V) == vRoot {
			return i.ownValueMsg.V, true
		}
		for _, vm := range i.peerValueMsg {
			if ValueRoot(vm.V) == vRoot {
				return vm.V, true
			}
		}
		// Fall back to retained bundles — the leader's bundle preserves
		// V even when no KindValue carrying it has been observed yet
		// (e.g., σ-pool was seeded purely from the leader's LeaderSigma
		// via Phase-1 bundle observation, before any KindValue arrived).
		for _, retained := range i.retainedBundles[0] {
			for _, r := range retained {
				if ValueRoot(r.Bundle.Value) == vRoot {
					return r.Bundle.Value, true
				}
			}
		}
		return nil, false
	}
	// L_k>0: search own Phase-2a emission (which contains the L_k>0
	// SigmaChained entry the local op may have signed at fire-time).
	// Own Phase-2a emission is one of {ownValueMsg, ownNoValueMsg,
	// ownCommit(NRDirect)} — all three can carry LayerEntries; check
	// whichever is set.
	if i.ownValueMsg != nil {
		for _, e := range i.ownValueMsg.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	if i.ownNoValueMsg != nil {
		for _, e := range i.ownNoValueMsg.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	if i.ownCommit != nil && i.ownCommit.Side == CommitSideNRDirect {
		for _, e := range i.ownCommit.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	for _, vm := range i.peerValueMsg {
		for _, e := range vm.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	for _, nv := range i.peerNoValueMsg {
		for _, e := range nv.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	for _, c := range i.peerCommit {
		if c.Side != CommitSideNRDirect {
			continue
		}
		for _, e := range c.LayerEntries {
			if e.Layer == layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == vRoot {
				return e.V, true
			}
		}
	}
	// Fall back to retained bundles at this layer — the leader's LeaderSigma
	// preserves V even when no SigmaChained entry carrying that V at this
	// layer has arrived yet (e.g., σ-pool[k] was seeded purely from the
	// leader's plaintext witness via Phase-1 bundle observation, before
	// any peer's chained entry decrypted). Mirrors the L_0 fallback above.
	for _, retained := range i.retainedBundles[layer] {
		for _, r := range retained {
			if ValueRoot(r.Bundle.Value) == vRoot {
				return r.Bundle.Value, true
			}
		}
	}
	return nil, false
}

// selectWinningGroup picks the group with the most partials;
// lexicographic V tiebreak for determinism across operators.
//
// Signature note: takes a map keyed by V_root (twoab's natural grouping
// container — sigmaPool is V_root-keyed). The bare-OBFT sibling
// helper at `protocol/v2/obft/base.selectWinningGroup` takes a
// `[]*sigGroup` slice (base's grouping is slice-driven via addToGroup
// dedup). The function bodies are line-for-line identical; the input-
// shape divergence reflects each package's internal sigGroup-collection
// strategy. See docs/OBFT-TWOAB-CONVERGENCE-PLAN.md §L2 for the
// rationale for keeping the divergence.
func selectWinningGroup(groups map[[32]byte]*sigGroup) *sigGroup {
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

// BuildCertificate produces the final-certificate gossip message after
// a successful Resolve.
//
// No `i.ended` guard: BuildCertificate is a pure value-constructor
// (reads `i.cfg.ClusterID` / `Height`, builds Certificate from supplied
// Output) — no Instance state is mutated. Post-Finalize the method
// still works, letting a runner build a certificate from a cached
// Output for retry submission. Matches the base sibling.
func (i *Instance) BuildCertificate(out *Output) (*Certificate, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if out == nil {
		return nil, fmt.Errorf("twoab: nil output")
	}
	if len(out.Value) == 0 || len(out.Signature) == 0 {
		return nil, fmt.Errorf("twoab: empty value or signature")
	}
	return &Certificate{
		ClusterID: i.cfg.ClusterID,
		Height:    i.cfg.Height,
		Value:     append(Value{}, out.Value...),
		Signature: append(Signature{}, out.Signature...),
	}, nil
}

// ObserveCertificate records a peer's Certificate.
func (i *Instance) ObserveCertificate(c *Certificate) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidateCertificate(c, i.cfg); err != nil {
		return err
	}
	if !i.signer.VerifyAggregate(i.clusterPubKey, c.Value, c.Signature) {
		return fmt.Errorf("twoab: certificate signature does not verify against cluster pubkey")
	}
	if i.receivedCertificate == nil {
		i.receivedCertificate = &Certificate{
			ClusterID: c.ClusterID,
			Height:    c.Height,
			Value:     append(Value{}, c.Value...),
			Signature: append(Signature{}, c.Signature...),
		}
	}
	return nil
}

// RetainedCertificate returns a deep copy of the peer-broadcast
// Certificate previously observed via ObserveCertificate, or nil if none.
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
// needs a fresh BLS verify. aggregatePeerLayerEntries collects these into
// a batch (F4) and either approves the whole set via VerifyPartialBatch +
// cache-populate, or falls back to per-tuple verify to attribute Rule-4
// evidence on the byzantine entries.
//
// ciphertext is captured for the Rule-4 evidence path in the sequential
// fallback (same FakeEncryptedPresenceEvidence shape as the pre-F4 inline
// code path in extractSigmaFromEntries). value and partial are the
// post-decryption tuple the F1 cache key binds.
//
// Mirror of base.pendingVerify — see docs/OBFT-F4-IMPLEMENTATION-PLAN.md.
type pendingVerify struct {
	op         OperatorID
	pubShare   []byte
	value      Value
	partial    Signature
	ciphertext []byte
}

// batchVerifyAndPopulate runs one VerifyPartialBatch over the pending
// tuples. On success: every tuple is F1-cache-populated and added to its
// sigGroup (keyed by V_root); returns true. On failure: returns false
// without touching the cache or groups — the caller runs the sequential
// fallback to identify the bad tuple(s) and attribute Rule-4 evidence.
//
// Twoab mirror of base.batchVerifyAndPopulate. The only shape difference
// is the addToGroup convention: twoab keys groups by V_root in a map, base
// uses an addToGroup helper over a slice.
func (i *Instance) batchVerifyAndPopulate(layer int, pending []pendingVerify, groups map[[32]byte]*sigGroup) bool {
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
		vRoot := ValueRoot(pv.value)
		g := groups[vRoot]
		if g == nil {
			g = &sigGroup{value: append(Value{}, pv.value...), partials: map[OperatorID]Signature{}}
			groups[vRoot] = g
		}
		g.partials[pv.op] = append(Signature{}, pv.partial...)
	}
	return true
}

// sequentialVerifyAndAttribute is the per-tuple fallback after a batch
// verify failed. For each tuple that verifies individually: F1-cache-
// populate + addToGroup. For each tuple that fails individually at L_k>0:
// record Rule-4 evidence (same EvidenceFakeEncryptedPresence shape and
// recordRule4 per-(op, layer) dedup as the inline pre-F4 code path; at
// L_0 the fallback is unreachable in practice since L_0 entries always
// cache-hit and never enter pending, but the L_0-guard matches base for
// defense-in-depth).
//
// Twoab mirror of base.sequentialVerifyAndAttribute. Preserves the
// per-(op, layer) Rule-4 attribution exactly as the pre-F4 code did.
func (i *Instance) sequentialVerifyAndAttribute(layer int, pending []pendingVerify, groups map[[32]byte]*sigGroup) {
	for _, pv := range pending {
		if i.signer.VerifyPartial(pv.pubShare, pv.value, pv.partial) {
			i.markVerified(pv.op, layer, pv.value, pv.partial)
			vRoot := ValueRoot(pv.value)
			g := groups[vRoot]
			if g == nil {
				g = &sigGroup{value: append(Value{}, pv.value...), partials: map[OperatorID]Signature{}}
				groups[vRoot] = g
			}
			g.partials[pv.op] = append(Signature{}, pv.partial...)
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
