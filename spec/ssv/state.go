package ssv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// State holds all the relevant progress the duty execution progress
type State struct {
	// pre consensus signatures
	SelectionProofPartialSig        *PartialSigContainer
	RandaoPartialSig                *PartialSigContainer
	ContributionProofs              *PartialSigContainer
	ContributionSubCommitteeIndexes map[string]uint64 // maps contribution sig root to subcommittee index
	PostConsensusPartialSig         *PartialSigContainer

	// consensus
	RunningInstance *qbft.Instance
	DecidedValue    *types.ConsensusData

	// post consensus signed objects
	SignedAttestation   *spec.Attestation
	SignedProposal      *altair.SignedBeaconBlock
	SignedAggregate     *spec.SignedAggregateAndProof
	SignedSyncCommittee *altair.SyncCommitteeMessage
	SignedContributions map[string]*altair.SignedContributionAndProof // maps contribution root to signed contribution

	// flags
	Finished bool // Finished marked true when there is a full successful cycle (pre, consensus and post) with quorum
}

func NewDutyExecutionState(quorum uint64) *State {
	return &State{
		SelectionProofPartialSig:        NewPartialSigContainer(quorum),
		RandaoPartialSig:                NewPartialSigContainer(quorum),
		PostConsensusPartialSig:         NewPartialSigContainer(quorum),
		ContributionProofs:              NewPartialSigContainer(quorum),
		ContributionSubCommitteeIndexes: make(map[string]uint64),
		Finished:                        false,
	}
}

// ReconstructRandaoSig aggregates collected partial randao sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructRandaoSig(root, validatorPubKey []byte) ([]byte, error) {
	// Reconstruct signatures
	signature, err := pcs.RandaoPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct randao sig")
	}
	return signature, nil
}

// ReconstructSelectionProofSig aggregates collected partial selection proof sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructSelectionProofSig(root, validatorPubKey []byte) ([]byte, error) {
	// Reconstruct signatures
	signature, err := pcs.SelectionProofPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct selection proof sig")
	}
	return signature, nil
}

// ReconstructContributionProofSig aggregates collected partial contribution proof sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructContributionProofSig(root, validatorPubKey []byte) ([]byte, uint64, error) {
	// Reconstruct signatures
	signature, err := pcs.ContributionProofs.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, 0, errors.Wrap(err, "could not reconstruct contribution proof sig")
	}

	// find subcommittee index
	index, found := pcs.ContributionSubCommitteeIndexes[hex.EncodeToString(root)]
	if !found {
		return nil, 0, errors.New("could not find subcommittee index")
	}
	return signature, index, nil
}

// ReconstructContributionSig aggregates collected partial contribution sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructContributionSig(root, validatorPubKey []byte) (*altair.SignedContributionAndProof, error) {

	// Reconstruct signatures
	signature, err := pcs.PostConsensusPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct contribution sig")
	}

	// find SignedContributionAndProod
	contrib, found := pcs.SignedContributions[hex.EncodeToString(root)]
	if !found {
		return nil, errors.New("could not find SignedContributionAndProod")
	}

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature)
	contrib.Signature = blsSig
	return contrib, nil
}

// ReconstructAttestationSig aggregates collected partial sigs, reconstructs a valid sig and returns an attestation obj with the reconstructed sig
func (pcs *State) ReconstructAttestationSig(root, validatorPubKey []byte) (*spec.Attestation, error) {
	// Reconstruct signatures
	signature, err := pcs.PostConsensusPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct attestation sig")
	}

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature)
	pcs.SignedAttestation.Signature = blsSig
	return pcs.SignedAttestation, nil
}

// ReconstructBeaconBlockSig aggregates collected partial sigs, reconstructs a valid sig and returns a SignedBeaconBlock with the reconstructed sig
func (pcs *State) ReconstructBeaconBlockSig(root, validatorPubKey []byte) (*altair.SignedBeaconBlock, error) {
	// Reconstruct signatures
	signature, err := pcs.PostConsensusPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct attestation sig")
	}

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature)
	pcs.SignedProposal.Signature = blsSig
	return pcs.SignedProposal, nil
}

// ReconstructSignedAggregateSelectionProofSig aggregates collected partial signed aggregate selection proof sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructSignedAggregateSelectionProofSig(root, validatorPubKey []byte) (*spec.SignedAggregateAndProof, error) {
	// Reconstruct signatures
	signature, err := pcs.PostConsensusPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
	}
	return nil, errors.Wrap(err, "could not reconstruct SignedAggregateSelectionProofSig")

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature)
	pcs.SignedAggregate.Signature = blsSig
	return pcs.SignedAggregate, nil
}

// ReconstructSyncCommitteeSig aggregates collected partial sync committee sigs, reconstructs a valid sig and returns it
func (pcs *State) ReconstructSyncCommitteeSig(root, validatorPubKey []byte) (*altair.SyncCommitteeMessage, error) {
	// Reconstruct signatures
	signature, err := pcs.PostConsensusPartialSig.ReconstructSignature(root, validatorPubKey)
	if err != nil {
	}
	return nil, errors.Wrap(err, "could not reconstruct SignedAggregateSelectionProofSig")

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature)
	pcs.SignedSyncCommittee.Signature = blsSig
	return pcs.SignedSyncCommittee, nil
}

// GetRoot returns the root used for signing and verification
func (pcs *State) GetRoot() ([]byte, error) {
	marshaledRoot, err := pcs.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode State")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// Encode returns the encoded struct in bytes or error
func (pcs *State) Encode() ([]byte, error) {
	return json.Marshal(pcs)
}

// Decode returns error if decoding failed
func (pcs *State) Decode(data []byte) error {
	return json.Unmarshal(data, &pcs)
}
