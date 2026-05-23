package twoab

import (
	"errors"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// NewVerifierFromShare constructs a twoab.Verifier from a validator's SSV
// share. Used by the message-validation layer to verify inner 2abOBFT BLS /
// IBE-tag material at the validation boundary, before the message reaches the
// consensus-critical path. Mirror of the bare-OBFT NewVerifierFromShare; it
// builds the 2abOBFT Verifier (extra VerifyValueMsg / VerifyNoValueMsg
// methods, NR partials carried in LayerEntries) instead of the bare one.
//
// Returns a verify-only Verifier (its Signers are constructed without a secret
// share — they can verify partials and aggregates but cannot sign).
//
// `beacon` is required so the V-side verifier can translate the block in V into
// the proposer-domain signing root (matching the production signer — see
// proposer_signer.go).
//
// IBE pub-key shares default to the V-shares (Option A: V-keypair doubles as
// the IBE keypair via DST separation). Pass a non-nil ibePubKeyShares override
// for Option B deployments.
func NewVerifierFromShare(
	share *spectypes.Share,
	ibePubKeyShares map[spectypes.OperatorID][]byte,
	beacon *networkconfig.Beacon,
) (*twoabcore.Verifier, error) {
	if share == nil {
		return nil, errors.New("twoab adapter: nil share")
	}
	if len(share.Committee) == 0 {
		return nil, errors.New("twoab adapter: empty committee in share")
	}
	if beacon == nil {
		return nil, errors.New("twoab adapter: nil beacon config")
	}

	pubKeyShares := make(map[twoabcore.OperatorID][]byte, len(share.Committee))
	for _, m := range share.Committee {
		if m == nil || len(m.SharePubKey) == 0 {
			return nil, errors.New("twoab adapter: committee member with empty share pubkey")
		}
		pubKeyShares[twoabcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
	}

	var nrShares map[twoabcore.OperatorID][]byte
	if ibePubKeyShares != nil {
		nrShares = make(map[twoabcore.OperatorID][]byte, len(ibePubKeyShares))
		for id, pk := range ibePubKeyShares {
			nrShares[twoabcore.OperatorID(id)] = append([]byte(nil), pk...)
		}
	}

	vSigner, err := NewProposerSigner(blsbackend.New(nil), beacon)
	if err != nil {
		return nil, fmt.Errorf("twoab adapter: build verify-only proposer signer: %w", err)
	}

	return &twoabcore.Verifier{
		Signer:         vSigner,
		TagSigner:      blsbackend.NewKyberSigner(nil),
		PubKeyShares:   pubKeyShares,
		NRPubKeyShares: nrShares,
		ClusterPubKey:  append([]byte(nil), share.ValidatorPubKey[:]...),
	}, nil
}
