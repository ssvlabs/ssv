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

	// BlindedExecutionPayloadEnvelope is the §6 signing input (SIP #94 §6): the envelope with the payload
	// and the execution requests replaced by their roots, whose progressive root equals the full
	// envelope's, so the threshold signature over it is valid for the full SignedExecutionPayloadEnvelope.
	// Every operator derives it from the §4-decided value (GloasProposalData.DeriveBlindedEnvelope); it is
	// ssv-spec's type so the node hashes exactly what the spec fixtures and Anchor hash.
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

// Blinded converts a full execution-payload envelope into the §6 blinded envelope: the payload and the
// execution requests are replaced by their roots. Every SSZ field subtree commits to hash_tree_root(field),
// so the blinded envelope's progressive root equals the full envelope's, and a signature over the blinded
// signing root is valid for the full SignedExecutionPayloadEnvelope. The builder operator compares this
// form of its own produced envelope with the one derived from the decided value (SIP #94 §6).
func Blinded(e *ExecutionPayloadEnvelope) (*BlindedExecutionPayloadEnvelope, error) {
	if e == nil || e.Payload == nil || e.ExecutionRequests == nil {
		return nil, fmt.Errorf("nil execution payload envelope")
	}
	payloadRoot, err := e.Payload.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of execution payload: %w", err)
	}
	requestsRoot, err := e.ExecutionRequests.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of execution requests: %w", err)
	}
	return &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           payloadRoot,
		ExecutionRequestsRoot: requestsRoot,
		BuilderIndex:          specgloas.BuilderIndex(e.BuilderIndex),
		BeaconBlockRoot:       e.BeaconBlockRoot,
		ParentBeaconBlockRoot: e.ParentBeaconBlockRoot,
	}, nil
}
