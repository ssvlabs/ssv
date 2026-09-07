package gloas

import (
	"fmt"

	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"

	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"
)

// The Gloas beacon-chain types are sourced from go-eth2-client's spec/gloas — the canonical,
// fork-maintained Ethereum types (progressive-SSZ correct) — rather than hand-rolled here, and the
// SSV wire type shared with other implementations (the §6 blinded envelope) from ssv-spec. Only the
// SSV-protocol consensus value (GloasBeaconVote) and the SSV-node types (BuilderConfig, PTCDuty,
// BuilderRequestAuth, ...) live in this package.
type (
	BeaconBlock                    = eth2gloas.BeaconBlock
	BeaconBlockBody                = eth2gloas.BeaconBlockBody
	SignedBeaconBlock              = eth2gloas.SignedBeaconBlock
	PayloadAttestation             = eth2gloas.PayloadAttestation
	PayloadAttestationData         = eth2gloas.PayloadAttestationData
	PayloadAttestationMessage      = eth2gloas.PayloadAttestationMessage
	ExecutionPayload               = eth2gloas.ExecutionPayload
	ExecutionPayloadBid            = eth2gloas.ExecutionPayloadBid
	SignedExecutionPayloadBid      = eth2gloas.SignedExecutionPayloadBid
	ExecutionPayloadEnvelope       = eth2gloas.ExecutionPayloadEnvelope
	SignedExecutionPayloadEnvelope = eth2gloas.SignedExecutionPayloadEnvelope
	ExecutionRequests              = eth2gloas.ExecutionRequests
	BuilderDepositRequest          = eth2gloas.BuilderDepositRequest
	BuilderExitRequest             = eth2gloas.BuilderExitRequest
	ProposerPreferences            = eth2gloas.ProposerPreferences
	SignedProposerPreferences      = eth2gloas.SignedProposerPreferences
	BuilderIndex                   = eth2gloas.BuilderIndex

	// BlindedExecutionPayloadEnvelope is the §6 dissemination value (SIP #94 §6): the full envelope with
	// the payload replaced by its hash_tree_root, whose progressive root equals the full envelope's, so
	// the threshold signature over it is valid for the full SignedExecutionPayloadEnvelope. It is
	// ssv-spec's wire type — it rides inside spectypes.EnvelopeDissemination — so the node signs and
	// disseminates exactly the bytes the spec fixtures and Anchor encode.
	BlindedExecutionPayloadEnvelope = specgloas.BlindedExecutionPayloadEnvelope
)

// BuilderIndexSelfBuild (BUILDER_INDEX_SELF_BUILD) flags a self-built execution payload (SIP #94 §4).
const BuilderIndexSelfBuild = BuilderIndex(^uint64(0))

// MaxProposerPreferencesDistinctRoots bounds the distinct ProposerPreferences signing roots one
// signer may put on the wire per proposal slot — SIP #94 §7's normative cap of 4: the extra roots
// come from preference-input changes between emissions (notably a dependent_root shift under
// reorg), and the cap is policy headroom. Message validation enforces it world-wide per
// (slot, signer); the §5 dispatcher sizes its pending stash from it.
const MaxProposerPreferencesDistinctRoots = 4

// Blinded converts a full execution-payload envelope into the §6 blinded envelope, swapping the
// execution payload for its hash_tree_root. The request lists are re-typed into ssv-spec's
// ExecutionRequests, the same Gloas five-list container with an identical SSZ layout, so the blinded
// envelope's requests root equals the full envelope's and the two hash to the same root.
func Blinded(e *ExecutionPayloadEnvelope) (*BlindedExecutionPayloadEnvelope, error) {
	if e == nil || e.Payload == nil {
		return nil, fmt.Errorf("nil execution payload envelope")
	}
	payloadRoot, err := e.Payload.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of execution payload: %w", err)
	}
	requests, err := specExecutionRequests(e.ExecutionRequests)
	if err != nil {
		return nil, err
	}
	return &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           payloadRoot,
		ExecutionRequests:     requests,
		BuilderIndex:          specgloas.BuilderIndex(e.BuilderIndex),
		BeaconBlockRoot:       e.BeaconBlockRoot,
		ParentBeaconBlockRoot: e.ParentBeaconBlockRoot,
	}, nil
}

// specExecutionRequests re-types go-eth2-client's Gloas ExecutionRequests as ssv-spec's. The three
// Electra request lists share their element types; the EIP-8282 builder lists are copied field by
// field (ssv-spec fixes the withdrawal credentials at 32 bytes where go-eth2-client keeps a slice).
func specExecutionRequests(r *ExecutionRequests) (*specgloas.ExecutionRequests, error) {
	if r == nil {
		return nil, fmt.Errorf("nil execution requests")
	}
	out := &specgloas.ExecutionRequests{
		Deposits:        r.Deposits,
		Withdrawals:     r.Withdrawals,
		Consolidations:  r.Consolidations,
		BuilderDeposits: make([]*specgloas.BuilderDepositRequest, 0, len(r.BuilderDeposits)),
		BuilderExits:    make([]*specgloas.BuilderExitRequest, 0, len(r.BuilderExits)),
	}
	for _, d := range r.BuilderDeposits {
		if d == nil {
			return nil, fmt.Errorf("nil builder deposit request")
		}
		sd := &specgloas.BuilderDepositRequest{Pubkey: d.Pubkey, Amount: d.Amount, Signature: d.Signature}
		if len(d.WithdrawalCredentials) != len(sd.WithdrawalCredentials) {
			return nil, fmt.Errorf("builder deposit withdrawal credentials: got %d bytes, want %d", len(d.WithdrawalCredentials), len(sd.WithdrawalCredentials))
		}
		copy(sd.WithdrawalCredentials[:], d.WithdrawalCredentials)
		out.BuilderDeposits = append(out.BuilderDeposits, sd)
	}
	for _, x := range r.BuilderExits {
		if x == nil {
			return nil, fmt.Errorf("nil builder exit request")
		}
		out.BuilderExits = append(out.BuilderExits, &specgloas.BuilderExitRequest{SourceAddress: x.SourceAddress, Pubkey: x.Pubkey})
	}
	return out, nil
}

// DecodeBeaconBlock unmarshals a Gloas BeaconBlock from QBFT consensus DataSSZ. It is the proposer
// path's node-side replacement for spectypes.ProposerConsensusData.GetBlockData, which has no Gloas
// version; the returned block doubles as the HashRoot the proposer signs.
func DecodeBeaconBlock(dataSSZ []byte) (*BeaconBlock, error) {
	b := &BeaconBlock{}
	if err := b.UnmarshalSSZ(dataSSZ); err != nil {
		return nil, err
	}
	return b, nil
}
