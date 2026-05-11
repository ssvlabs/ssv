package obft

import (
	"errors"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// NewVerifierFromShare constructs an obft.Verifier from a validator's
// SSV share. Used by the message-validation layer to verify inner OBFT
// BLS / IBE-tag material at the validation boundary, before the message
// reaches the consensus-critical path.
//
// Returns a verify-only Verifier (its Signers are constructed without a
// secret share — they can verify partials and aggregates but cannot sign).
//
// `beacon` is required so the V-side verifier can translate the block in V
// into the proposer-domain signing root (matching the production signer
// — see proposer_signer.go).
//
// IBE pub-key shares default to the V-shares (Option A: V-keypair doubles
// as the IBE keypair via DST separation). Pass a non-nil ibePubKeyShares
// override for Option B deployments.
func NewVerifierFromShare(
	share *spectypes.Share,
	ibePubKeyShares map[spectypes.OperatorID][]byte,
	beacon *networkconfig.Beacon,
) (*obftcore.Verifier, error) {
	if share == nil {
		return nil, errors.New("obft adapter: nil share")
	}
	if len(share.Committee) == 0 {
		return nil, errors.New("obft adapter: empty committee in share")
	}
	if beacon == nil {
		return nil, errors.New("obft adapter: nil beacon config")
	}

	pubKeyShares := make(map[obftcore.OperatorID][]byte, len(share.Committee))
	for _, m := range share.Committee {
		if m == nil || len(m.SharePubKey) == 0 {
			return nil, errors.New("obft adapter: committee member with empty share pubkey")
		}
		pubKeyShares[obftcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
	}

	var nrShares map[obftcore.OperatorID][]byte
	if ibePubKeyShares != nil {
		nrShares = make(map[obftcore.OperatorID][]byte, len(ibePubKeyShares))
		for id, pk := range ibePubKeyShares {
			nrShares[obftcore.OperatorID(id)] = append([]byte(nil), pk...)
		}
	}

	vSigner, err := NewProposerSigner(blsbackend.New(nil), beacon)
	if err != nil {
		return nil, fmt.Errorf("obft adapter: build verify-only proposer signer: %w", err)
	}

	return &obftcore.Verifier{
		Signer:         vSigner,
		TagSigner:      blsbackend.NewKyberSigner(nil),
		PubKeyShares:   pubKeyShares,
		NRPubKeyShares: nrShares,
		ClusterPubKey:  append([]byte(nil), share.ValidatorPubKey[:]...),
	}, nil
}
