package obft

import (
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// internalByz is the OBFT-specific byz interface. Each method has an honest
// default; concrete patterns selectively override.
type internalByz interface {
	LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan
	AllowCommitBroadcast(op obft.OperatorID) bool
	AllowCertificateBroadcast(op obft.OperatorID) bool
	AllowDelivery(from, to obft.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to obft.OperatorID, kind ct.MsgKind) time.Duration
	OverrideCommit(s *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit
}

type broadcastPlan struct {
	V          obft.Value
	Recipients []obft.OperatorID // nil = all peers
}

// translateByz maps an abstract consensustest.ByzPattern to an OBFT-internal
// impl. All catalog kinds are OBFT-applicable; QBFT-only kinds would return
// ct.ErrNotApplicable.
func translateByz(p ct.ByzPattern) (internalByz, error) {
	switch p.Kind {
	case ct.ByzNone:
		return byzNone{}, nil
	case ct.ByzSilentLeader:
		return byzSilentLeader{Byz: obft.OperatorID(p.PrimaryByz), Layer: 0}, nil
	case ct.ByzMultiSilent:
		k := p.K
		if k <= 0 {
			k = 3 // default: top 3 silent (covers BFT-comparison "multi-silent" row)
		}
		return byzMultiSilent{OnlyHonestLayer: k}, nil
	case ct.ByzEquivocate111:
		return byzEquivoc111{Byz: obft.OperatorID(p.PrimaryByz)}, nil
	case ct.ByzEquivocateAllNR:
		return byzEquivocAllNR{Byz: obft.OperatorID(p.PrimaryByz)}, nil
	case ct.ByzEquivocateSigmaLockedSplit:
		return byzEquivocSigmaLockedSplit{
			Byz:        obft.OperatorID(p.PrimaryByz),
			RecipientA: pickRecipient(p.Recipients, 0, 2),
			RecipientB: pickRecipient(p.Recipients, 1, 3),
		}, nil
	case ct.ByzHV1SelectiveDelivery:
		return byzHV1Selective{
			Byz:       obft.OperatorID(p.PrimaryByz),
			Recipient: pickRecipient(p.Recipients, 0, 2),
		}, nil
	case ct.ByzFakeEncryptedPresence:
		return byzFakeEncryptedPresence{
			Byz:          obft.OperatorID(p.PrimaryByz),
			SilentLayer:  0,
			GarbageLayer: 1,
		}, nil
	case ct.ByzSigmaRefusal:
		return byzSigmaRefusal{Byz: obft.OperatorID(p.PrimaryByz)}, nil
	default:
		return nil, fmt.Errorf("obft adapter: unknown ByzKind %v", p.Kind)
	}
}

func pickRecipient(list []ct.OperatorID, idx int, fallback obft.OperatorID) obft.OperatorID {
	if idx < len(list) {
		return obft.OperatorID(list[idx])
	}
	return fallback
}

// ---- honest defaults (mixin) -------------------------------------------

type honestDefaults struct{}

func (honestDefaults) AllowCommitBroadcast(obft.OperatorID) bool      { return true }
func (honestDefaults) AllowCertificateBroadcast(obft.OperatorID) bool { return true }
func (honestDefaults) AllowDelivery(_, _ obft.OperatorID, _ ct.MsgKind) bool {
	return true
}
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (honestDefaults) OverrideCommit(_ *sim, _ obft.OperatorID, c *obft.Commit) *obft.Commit {
	return c
}

// ---- byzNone -----------------------------------------------------------

type byzNone struct{ honestDefaults }

func (byzNone) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// ---- byzSilentLeader ---------------------------------------------------

type byzSilentLeader struct {
	honestDefaults
	Byz   obft.OperatorID
	Layer int
}

func (b byzSilentLeader) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader == b.Byz && layer == b.Layer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzMultiSilent ----------------------------------------------------

type byzMultiSilent struct {
	honestDefaults
	OnlyHonestLayer int
}

func (b byzMultiSilent) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if layer < b.OnlyHonestLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzEquivoc111 -----------------------------------------------------

type byzEquivoc111 struct {
	honestDefaults
	Byz obft.OperatorID
}

func (b byzEquivoc111) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	others := make([]obft.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != b.Byz {
			others = append(others, op)
		}
	}
	plans := make([]broadcastPlan, 0, len(others))
	for i, recipient := range others {
		v := append(obft.Value{}, []byte{'b', 'y', 'z', '-', 'V', byte('1' + i)}...)
		plans = append(plans, broadcastPlan{V: v, Recipients: []obft.OperatorID{b.Byz, recipient}})
	}
	return plans
}

// ---- byzEquivocAllNR ---------------------------------------------------

type byzEquivocAllNR struct {
	honestDefaults
	Byz obft.OperatorID
}

func (b byzEquivocAllNR) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	all := s.operators
	return []broadcastPlan{
		{V: append(obft.Value{}, "byz-V-A"...), Recipients: all},
		{V: append(obft.Value{}, "byz-V-B"...), Recipients: all},
	}
}

// ---- byzEquivocSigmaLockedSplit ----------------------------------------

type byzEquivocSigmaLockedSplit struct {
	honestDefaults
	Byz        obft.OperatorID
	RecipientA obft.OperatorID
	RecipientB obft.OperatorID
}

func (b byzEquivocSigmaLockedSplit) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	vA := append(obft.Value{}, "byz-V-A"...)
	vB := append(obft.Value{}, "byz-V-B"...)
	return []broadcastPlan{
		{V: vA, Recipients: []obft.OperatorID{b.Byz, b.RecipientA}},
		{V: vB, Recipients: []obft.OperatorID{b.Byz, b.RecipientB}},
	}
}

// ---- byzHV1Selective ---------------------------------------------------

type byzHV1Selective struct {
	honestDefaults
	Byz       obft.OperatorID
	Recipient obft.OperatorID
}

func (b byzHV1Selective) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	return []broadcastPlan{
		{V: honestV, Recipients: []obft.OperatorID{b.Byz, b.Recipient}},
	}
}

// ---- byzFakeEncryptedPresence ------------------------------------------

type byzFakeEncryptedPresence struct {
	honestDefaults
	Byz          obft.OperatorID
	SilentLayer  int
	GarbageLayer int
}

func (b byzFakeEncryptedPresence) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if leader == b.Byz && layer == b.SilentLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakeEncryptedPresence) OverrideCommit(_ *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit {
	if op != b.Byz {
		return c
	}
	if b.GarbageLayer < 0 || b.GarbageLayer >= len(c.Layers) {
		return c
	}
	cp := &obft.Commit{
		OperatorID: c.OperatorID,
		Height:     c.Height,
		Layers:     make([]obft.EncryptedLayer, len(c.Layers)),
		NRPartials: c.NRPartials,
	}
	copy(cp.Layers, c.Layers)
	cp.Layers[b.GarbageLayer] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, "byz-fake-V-at-deeper-layer"...),
		Ciphertext: []byte("garbage-bytes-not-a-valid-ibe-ciphertext"),
	}
	return cp
}

// ---- byzSigmaRefusal ---------------------------------------------------

type byzSigmaRefusal struct {
	honestDefaults
	Byz obft.OperatorID
}

func (b byzSigmaRefusal) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzSigmaRefusal) AllowCommitBroadcast(op obft.OperatorID) bool {
	return op != b.Byz
}
