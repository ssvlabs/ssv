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
// At layer 0, σ partials are plaintext in KindValue.L0Partial (post Op5)
// and the leader's L0Witness in retained Phase-1 bundles (Op3). At layers
// k > 0, σ partials are chained-encrypted inside Phase-2a emissions
// (ValueMsg / NoValueMsg / Commit-NRDirect LayerEntries with
// Kind=SigmaChained); the accumulated NR-quorum aggregates from prior
// layers serve as decryption keys (outermost-first).
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
	K := i.cfg.K()
	// chainedKeys[j] is the aggregated NR-partials sig on nr_tag_j.
	// To decrypt a layer-k onion entry, apply chainedKeys[0..k-1] in
	// order (outermost-first).
	chainedKeys := make([][]byte, K)

	for k := 0; k < K; k++ {
		// Try σ-pool reconstruction at this layer.
		out, err := i.tryReconstructLayer(k, chainedKeys)
		if err != nil {
			return nil, fmt.Errorf("twoab: layer %d reconstruction: %w", k, err)
		}
		if out != nil {
			return out, nil
		}

		// σ-quorum did not reach. Try NR-quorum to advance to next layer.
		// At the deepest layer there is no NR tag — the walk terminates.
		if k == K-1 {
			break
		}
		nextKey, err := i.tryDeriveNextLayerKey(k)
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

// tryReconstructLayer attempts σ-quorum reconstruction at `layer`.
//
// Returns:
//   - (*Output, nil) — σ-quorum reached, output produced.
//   - (nil, nil) — no σ-quorum (caller should attempt NR-advance).
//   - (nil, error) — internal error (e.g., aggregation crypto failure).
//
// At L_0: σ partials come from sigmaPool[0] (populated on Commit-Signed
// observation in phase2b.go). Build groups by V_root (typically one
// group, but cross-σ-V equivocation could produce more).
//
// At L_k > 0: σ partials come from Phase-2a SigmaChained entries inside
// peerValueMsg / peerNoValueMsg / peerCommit (Side=NRDirect). The
// ciphertext payloads are decrypted via chainedKeys (outermost-first).
// On decryption failure or post-decryption verification failure, Rule 4
// fires.
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, error) {
	groups := make(map[[32]byte]*sigGroup)

	// 1) σ-pool entries already populated for this layer.
	if pools, ok := i.sigmaPool[layer]; ok {
		for vRoot, opPartials := range pools {
			for op, partial := range opPartials {
				g := groups[vRoot]
				if g == nil {
					// Reconstruct V from peer messages; we have the root
					// but need the bytes. For L_0 sigmaPool, the V is in
					// ownCommit.L0Value or peerCommit[op].L0Value. For
					// L_k>0 sigmaPool, the V is in the original LayerEntry.
					// Locate it via the helper.
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
	if winning == nil || len(winning.partials) < i.cfg.QV() {
		return nil, nil
	}

	full, err := i.signer.AggregatePartials(winning.partials)
	if err != nil {
		return nil, fmt.Errorf("aggregate σ partials: %w", err)
	}
	return &Output{
		Layer:     layer,
		Value:     append(Value{}, winning.value...),
		Signature: full,
	}, nil
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
func (i *Instance) aggregatePeerLayerEntries(layer int, chainedKeys [][]byte, groups map[[32]byte]*sigGroup) {
	// Iterate peer ValueMsgs.
	for op, vm := range i.peerValueMsg {
		i.extractSigmaFromEntries(op, layer, vm.LayerEntries, chainedKeys, groups)
	}
	// Iterate peer NoValueMsgs.
	for op, nv := range i.peerNoValueMsg {
		i.extractSigmaFromEntries(op, layer, nv.LayerEntries, chainedKeys, groups)
	}
	// Iterate peer Commits with Side=NRDirect (they carry LayerEntries).
	for op, c := range i.peerCommit {
		if c.Side != CommitSideNRDirect {
			continue
		}
		i.extractSigmaFromEntries(op, layer, c.LayerEntries, chainedKeys, groups)
	}
}

// extractSigmaFromEntries decrypts the SigmaChained entry at `layer` (if
// present) in `entries` and adds the resulting partial to `groups` (if
// it verifies).
func (i *Instance) extractSigmaFromEntries(op OperatorID, layer int, entries []LayerEntry,
	chainedKeys [][]byte, groups map[[32]byte]*sigGroup) {
	for _, e := range entries {
		if e.Layer != layer || e.Kind != LayerEntrySigmaChained {
			continue
		}
		// Decrypt the chained payload.
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
			continue
		}
		// Verify the decrypted partial against op's pubshare on V.
		opPub, ok := i.pubKeyShares[op]
		if !ok || len(opPub) == 0 {
			continue
		}
		if !i.signer.VerifyPartial(opPub, e.V, Signature(pt)) {
			if i.recordRule4(op, layer) {
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakeEncryptedPresence,
					OperatorID: op,
					Layer:      layer,
					FakeEncryptedPresence: &FakeEncryptedPresenceEvidence{
						Ciphertext:     append([]byte{}, e.Payload...),
						DecryptedBytes: append([]byte{}, pt...),
					},
				})
			}
			continue
		}
		// Add to groups.
		vRoot := ValueRoot(e.V)
		g := groups[vRoot]
		if g == nil {
			g = &sigGroup{value: append(Value{}, e.V...), partials: map[OperatorID]Signature{}}
			groups[vRoot] = g
		}
		g.partials[op] = append(Signature{}, pt...)
		return // only one SigmaChained entry per (op, layer) by construction
	}
}

// tryDeriveNextLayerKey aggregates ≥ qEnc NR partials on nr_tag_layer.
// Returns the aggregated full signature (= chained-decryption key for
// layer+1's outermost wrap), or nil if NR-quorum did not reach.
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, error) {
	partials := i.nrTagPool[layer]
	if len(partials) < i.cfg.QEnc() {
		return nil, nil
	}
	full, err := i.tagSigner.AggregatePartials(partials)
	if err != nil {
		return nil, err
	}
	return []byte(full), nil
}

// recoverV locates the V bytes corresponding to a given (layer, vRoot).
// Used by tryReconstructLayer to populate sigGroup.value from sigmaPool
// entries (which key by vRoot, not V bytes).
//
// At L_0 (post Op5): V comes from ownValueMsg or any peerValueMsg
// matching the root (KindValue is the σ-side terminal emission carrying
// the σ partial directly). Also from any retainedBundle at L_0 — the
// leader's bundle preserves V even if no peer KindValue carrying that
// V has arrived yet (e.g., harvest-only state where σ-pool was seeded
// from peer L0Witness alone).
//
// At L_k>0: V comes from any peer SigmaChained LayerEntry at this layer
// matching the root.
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
		// (e.g., σ-pool was seeded purely from the leader's L0Witness
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
	return nil, false
}

// selectWinningGroup picks the group with the most partials;
// lexicographic V tiebreak for determinism across operators.
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
func (i *Instance) BuildCertificate(out *Output) (*Certificate, error) {
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
