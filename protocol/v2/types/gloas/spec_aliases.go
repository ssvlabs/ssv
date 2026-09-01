package gloas

import (
	"fmt"

	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
)

// The Gloas beacon-chain types are sourced from go-eth2-client's spec/gloas — the canonical,
// fork-maintained Ethereum types (progressive-SSZ correct) — rather than hand-rolled here. Only the
// SSV-protocol types (GloasBeaconVote, EnvelopeConsensusData, the blinded envelope) and the SSV-node
// types (BuilderConfig, PTCDuty, BuilderRequestAuth, ...) live in this package.
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
)

// BuilderIndexSelfBuild (BUILDER_INDEX_SELF_BUILD) flags a self-built execution payload (SIP #94 §4).
const BuilderIndexSelfBuild = BuilderIndex(^uint64(0))

// MaxProposerPreferencesDistinctRoots bounds the distinct ProposerPreferences signing roots one
// signer may put on the wire per proposal slot — SIP #94 §7's normative cap of 4: the extra roots
// come from preference-input changes between emissions (notably a dependent_root shift under
// reorg), and the cap is policy headroom. Message validation enforces it world-wide per
// (slot, signer); the §5 dispatcher sizes its pending stash from it.
const MaxProposerPreferencesDistinctRoots = 4

// Blinded converts a full execution-payload envelope into SSV's blinded envelope (the §6 QBFT value),
// swapping the execution payload for its hash_tree_root.
func Blinded(e *ExecutionPayloadEnvelope) (*BlindedExecutionPayloadEnvelope, error) {
	payloadRoot, err := e.Payload.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of execution payload: %w", err)
	}
	return &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           payloadRoot,
		ExecutionRequests:     e.ExecutionRequests,
		BuilderIndex:          e.BuilderIndex,
		BeaconBlockRoot:       e.BeaconBlockRoot,
		ParentBeaconBlockRoot: e.ParentBeaconBlockRoot,
	}, nil
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
