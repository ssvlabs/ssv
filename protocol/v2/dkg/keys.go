// Package dkg implements the Pedersen DKG ceremony between SSV cluster
// operators that produces a per-cluster threshold IBE keypair for use by
// the TBFT proposer-duty path under Option B.
//
// This package is intentionally independent of the SSV transport. The
// Coordinator type talks to peers through an injected Transport
// interface; production hooks SSV's P2P broadcaster behind it,
// while tests use a synthetic in-memory transport.
package dkg

import (
	"errors"

	"github.com/drand/kyber"
)

// Keypair is a fresh-per-ceremony kyber long-term keypair. Each operator
// generates one at the start of every DKG run. The secret seeds
// `kyber_dkg.Config.Longterm` (deal decryption); the public is shared
// with peers via the pre-DKG Exchange message and seeds
// `kyber_dkg.Config.NewNodes`.
//
// The keypair is throwaway — it has no role outside the ceremony and
// must not be persisted or reused across runs. A fresh keypair per run
// gives forward secrecy across cluster reconfigs and re-DKGs at no
// implementation cost.
type Keypair struct {
	Secret kyber.Scalar
	Public kyber.Point
}

// GenerateKeypair samples a fresh scalar from the group's RNG and
// computes the corresponding public point. Returns an error if the
// group is nil or its randomness source is unavailable.
func GenerateKeypair(group kyber.Group) (*Keypair, error) {
	if group == nil {
		return nil, errors.New("dkg: nil kyber group")
	}
	random, ok := group.(kyber.Random)
	if !ok {
		return nil, errors.New("dkg: group does not provide a random stream")
	}
	s := group.Scalar().Pick(random.RandomStream())
	return &Keypair{
		Secret: s,
		Public: group.Point().Mul(s, nil),
	}, nil
}
