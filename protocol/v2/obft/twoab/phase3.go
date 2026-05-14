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
// At layer 0, σ partials are plaintext. At layers k > 0, σ partials
// are chained-encrypted under nr_tag_0..nr_tag_{k-1}; the accumulated
// NR-quorum aggregates from prior layers serve as decryption keys
// (outermost-first).
//
// 2abOBFT differs from bare OBFT in two structural ways for Phase 3:
//   - No Phase-1 σ_V partials (Variant C); the σ-pool at every layer
//     is built entirely from peer Onion2b contributions + the local
//     operator's own σ partial (cached after BuildOwnOnion2b).
//   - No LeaderSigmaWitness array; no witness-harvest branch.
//
// Returns:
//   - (*Output, nil) — σ-quorum reached at some layer.
//   - (nil, ErrNoQuorum) — walked all reachable layers without σ-quorum;
//     NR-quorum either failed at some intermediate layer (sealing the
//     remaining chain) or the walk reached K-1 without σ-quorum.
//   - (nil, error) — internal error (crypto failure on aggregation).
//
// As a side effect, Rule-4 evidence (fake encrypted-presence at k > 0)
// is recorded for any peer whose Onion2b entry decrypts to garbage or
// fails to decrypt. Rule 4 is per-(op, layer) deduped — multiple
// distinct entries from the same byzantine at the same layer surface
// once.
//
// Resolve is stateless / idempotent — re-running on late KindOnion2b
// arrivals incorporates additional contributions without contradicting
// prior outcomes (Pigeonhole semantics still hold). The canonical
// implementation calls Resolve opportunistically on every state delta
// (KindOnion2b / KindCertificate observation) starting at TCommit, not
// once at TCommit + Δ_2b — Δ_2b is the propagation budget, not a Resolve
// gate. ErrNoQuorum on incomplete state is returned cleanly without
// mutating Instance state, so observer-mode call sites pay no cost for
// pre-quorum attempts.
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
			// Neither σ-quorum at k nor NR-quorum to advance. Stuck.
			return nil, ErrNoQuorum
		}
		chainedKeys[k] = nextKey
	}
	return nil, ErrNoQuorum
}

// sigGroup holds σ partials grouped by the V they sign at a given
// layer. Multiple groups indicate cross-onion equivocation across
// distinct V's at the same layer; each group is counted independently
// per Pigeonhole 2 (at most one V reaches qV cluster-wide).
type sigGroup struct {
	value    Value
	partials map[OperatorID]Signature
}

// tryReconstructLayer attempts σ-quorum reconstruction at `layer`.
//
// Returns:
//   - (*Output, nil) — σ-quorum reached, output produced.
//   - (nil, nil) — no σ-quorum (caller should attempt NR-advance).
//   - (nil, error) — internal error (e.g., aggregation crypto failure).
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, error) {
	groups := make([]*sigGroup, 0, 1)

	// 1) Local operator's own σ contribution at this layer (from
	//    BuildOwnOnion2b's cache). Only present if the local op decided
	//    σ at this layer per the convergence rule.
	if i.sigmaLocked[layer] {
		if partial, ok := i.ownPartials[layer]; ok {
			v := i.sigmaLockedV[layer]
			addToGroup(&groups, v, i.ownOperatorID, partial)
		}
	}

	// 2) Peer Onion2b contributions. Decrypt at layers > 0 using the
	//    accumulated chained keys.
	for opID, entries := range i.peerOnions[layer] {
		if opID == i.ownOperatorID {
			// Already counted above via ownPartials. Skip to avoid double-
			// counting if a peer-echoed self-observed onion has been
			// recorded.
			continue
		}
		for _, el := range entries {
			var partial Signature
			if layer == 0 {
				partial = Signature(el.Ciphertext)
			} else {
				pt, err := i.chainDecryptForLayer(layer, el.Ciphertext, chainedKeys)
				if err != nil {
					// Decryption failure at k > 0 is Rule-4 evidence.
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

			pubShare, ok := i.pubKeyShares[opID]
			if !ok || len(pubShare) == 0 {
				continue
			}
			if !i.signer.VerifyPartial(pubShare, el.Value, partial) {
				if layer > 0 {
					// Decrypted bytes are not a valid σ partial on the
					// claimed V — Rule 4 (post-decryption garbage).
					if i.recordRule4(opID, layer) {
						i.recordEvidence(Evidence{
							Rule:       EvidenceFakeEncryptedPresence,
							OperatorID: opID,
							Layer:      layer,
							FakeEncryptedPresence: &FakeEncryptedPresenceEvidence{
								Ciphertext:     append([]byte{}, el.Ciphertext...),
								DecryptedBytes: append([]byte{}, partial...),
							},
						})
					}
				}
				// At L_0, this would have been Rule 5 (handled at
				// ObserveOnion2b time, not here).
				continue
			}
			addToGroup(&groups, el.Value, opID, partial)
		}
	}

	// 3) Pick the group with the most partials; check qV. Lexicographic
	//    V tiebreak makes the winner deterministic across operators
	//    even when transient pre-quorum states show two V's at equal
	//    counts (Pigeonhole 2 ensures only one ultimately reaches qV).
	winning := selectWinning(groups)
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

// tryDeriveNextLayerKey aggregates ≥ qEnc NR partials on nr_tag_layer.
// Returns the aggregated full signature (= chained-decryption key for
// layer+1's outermost wrap), or nil if NR-quorum did not reach.
//
// Includes the local operator's own NR partial at this layer (cached
// in ownOnion2b.NRPartials) alongside peer NR partials from peerNR.
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, error) {
	partials := make(map[OperatorID]Signature, len(i.peerNR[layer])+1)

	// Own NR partial at this layer, if Phase-2b emitted one.
	if i.nrLocked[layer] && i.ownOnion2b != nil {
		for _, p := range i.ownOnion2b.NRPartials {
			if p.Layer == layer {
				partials[i.ownOperatorID] = append(Signature{}, p.PartialSig...)
				break
			}
		}
	}

	// Peer NR partials.
	for op, sig := range i.peerNR[layer] {
		if op == i.ownOperatorID {
			// Already added above via ownOnion2b; skip to avoid
			// double-counting if a peer echoed our self-observation.
			continue
		}
		partials[op] = sig
	}

	if len(partials) < i.cfg.QEnc() {
		return nil, nil
	}
	full, err := i.tagSigner.AggregatePartials(partials)
	if err != nil {
		return nil, err
	}
	return []byte(full), nil
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

// selectWinning picks the group with the most partials; lexicographic V
// tiebreak for determinism across operators.
func selectWinning(groups []*sigGroup) *sigGroup {
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
// Per spec §Final-certificate gossip, an operator that reconstructed
// (V, S) gossips this certificate so receivers without local
// reconstruction can submit (V, S) downstream.
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

// ObserveCertificate records a peer's Certificate. The signature is
// verified against the cluster's V-keypair pubkey on Value; valid
// certificates are stored for the runner to use as an alternative
// submission path (RetainedCertificate accessor).
//
// First-wins semantics: subsequent calls after a valid Certificate is
// retained are silent dedup no-ops. (All honest peers' Certificates
// carry the same (Value, Signature) per Pigeonhole 2; the first one
// retained is sufficient.)
//
// Per spec, receivers SHOULD re-run host-application validity on Value
// before submitting downstream — that's a host concern, not in this
// method's scope.
func (i *Instance) ObserveCertificate(c *Certificate) error {
	if err := ValidateCertificate(c, i.cfg); err != nil {
		return err
	}
	if !i.signer.VerifyAggregate(i.clusterPubKey, c.Value, c.Signature) {
		return fmt.Errorf("twoab: certificate signature does not verify against cluster pubkey")
	}
	if i.receivedCertificate == nil {
		// Deep copy so retention is robust against caller-owned slice
		// mutation post-Observe.
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
// Certificate previously observed via ObserveCertificate, or nil if
// none.
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
