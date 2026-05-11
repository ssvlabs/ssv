package qbft

import (
	"crypto/rsa"
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

// proposerForRound implements the round-robin proposer selection used by
// QBFT in SSV: ((round - 1) mod n) + 1 → operator at that index in the
// canonically-ordered committee. Mirrors qbft.RoundRobinProposer (we don't
// import it to keep the signature simple — it takes a *State which we don't
// always have at call time).
func proposerForRound(operators []spectypes.OperatorID, round specqbft.Round) spectypes.OperatorID {
	n := len(operators)
	if n == 0 {
		return 0
	}
	idx := (int(round) - 1) % n
	if idx < 0 {
		idx += n
	}
	return operators[idx]
}

// makeProposalEnvelope constructs a signed PROPOSE envelope for `value` from
// `signerOp` at (height, round). Used by byz patterns that need to fabricate
// PROPOSEs without going through an Instance.Start (e.g. equivocation).
func makeProposalEnvelope(
	signerOp spectypes.OperatorID,
	signerKey *rsa.PrivateKey,
	identifier []byte,
	height specqbft.Height,
	round specqbft.Round,
	value []byte,
) (*spectypes.SignedSSVMessage, error) {
	root, err := specqbft.HashDataRoot(value)
	if err != nil {
		return nil, fmt.Errorf("qbft adapter: hash full data: %w", err)
	}
	qbftMsg := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     height,
		Round:      round,
		Identifier: identifier,
		Root:       root,
	}
	encoded, err := qbftMsg.Encode()
	if err != nil {
		return nil, fmt.Errorf("qbft adapter: encode proposal: %w", err)
	}
	msgID := spectypes.MessageID{}
	copy(msgID[:], identifier)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    encoded,
	}
	sig, err := signRSAWithCache(signerKey, ssvMsg)
	if err != nil {
		return nil, fmt.Errorf("qbft adapter: sign proposal: %w", err)
	}
	return &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{signerOp},
		SSVMessage:  ssvMsg,
		FullData:    append([]byte{}, value...),
	}, nil
}

// stableIdentifier returns a stable 56-byte identifier for the cluster's
// QBFT instance. The bytes don't carry meaningful role/cluster info — the
// adapter uses a fixed test identifier so the spec's MessageID handling is
// exercised without coupling to SSV's role-encoded MessageID layout.
func stableIdentifier() []byte {
	return spectestingutils.TestingIdentifier
}
