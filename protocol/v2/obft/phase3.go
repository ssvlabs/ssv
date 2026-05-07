package obft

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
func (i *Instance) Resolve() (*Output, error) {
	K := i.cfg.K()
	// chainedKeys[j] is the aggregated NR-partials sig on nr_tag_j. To
	// decrypt a layer-k onion entry, we apply chainedKeys[0..k-1] in order
	// (outermost-first).
	chainedKeys := make([][]byte, K)

	for k := 0; k < K; k++ {
		// Try σ-pool reconstruction at this layer.
		out, err := i.tryReconstructLayer(k, chainedKeys)
		if err != nil {
			return nil, fmt.Errorf("obft: layer %d reconstruction: %w", k, err)
		}
		if out != nil {
			return out, nil
		}

		// σ-quorum did not reach. Try NR-quorum to advance to next layer.
		if k == K-1 {
			break // no NR tag for the deepest layer
		}
		nextKey, err := i.tryDeriveNextLayerKey(k)
		if err != nil {
			return nil, fmt.Errorf("obft: layer %d NR aggregation: %w", k, err)
		}
		if nextKey == nil {
			// Neither σ-quorum at k nor NR-quorum to advance. Stuck.
			return nil, ErrNoQuorum
		}
		chainedKeys[k] = nextKey
	}
	return nil, ErrNoQuorum
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
//   - (*Output, nil)    — σ-quorum reached, output produced.
//   - (nil, nil)        — no σ-quorum (caller should attempt NR-advance).
//   - (nil, error)      — internal error (e.g. crypto failure).
//
// `chainedKeys` carries aggregated NR sigs from prior layers; only the
// first `layer` entries are used (chainedKeys[0..layer-1]).
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, error) {
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
		if i.signer.VerifyPartial(pubShare, b.Value, b.SigmaV) {
			addToGroup(&groups, b.Value, leaderID, b.SigmaV)
		}
	}

	// 2) Onion contributions. Decrypt at layers > 0 using accumulated
	//    chained keys.
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
			if !i.signer.VerifyPartial(pubShare, el.Value, partial) {
				if layer > 0 {
					// Decrypted bytes are not a valid σ partial on the
					// claimed V — Rule 4 (post-decryption garbage). Same
					// per-(op, layer) dedup as the decrypt-failure branch.
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
				// At L_0, this would have been Rule 5 (handled at observe
				// time in ObserveCommit).
				continue
			}
			addToGroup(&groups, el.Value, opID, partial)
		}
	}

	// 3) Pick the group with the most partials; check qV. Tiebreak by
	// lexicographic V — without it, two groups with equal partial counts
	// would resolve based on map-iteration order (peerOnions[layer] is a
	// map), producing nondeterministic Output across operators on transient
	// pre-quorum states. Pigeonhole 2 guarantees only one V reaches qV
	// cluster-wide given f-bound, but locally we can transiently observe
	// two V's at equal-count below qV; deterministic tiebreak makes the
	// "winner" identical across operators if both ever reach qV.
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

// tryDeriveNextLayerKey aggregates qEnc NR partials on nr_tag_layer. Returns
// the aggregated full sig (which serves as the chained-decryption key for
// layer+1's outermost wrap), or nil if NR-quorum did not reach.
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, error) {
	partials := i.peerNR[layer]
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

// BuildCertificate produces the final-certificate gossip message after a
// successful Resolve.
//
// Per spec §Final-certificate gossip, an operator that reconstructed (V, S)
// gossips the certificate so that other operators (including those that
// failed to reconstruct locally) can submit (V, S) downstream — protecting
// against the lone-reconstructor's beacon path failing.
func (i *Instance) BuildCertificate(out *Output) (*Certificate, error) {
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
