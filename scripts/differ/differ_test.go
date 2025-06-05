package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffer(t *testing.T) {
	input := `package main

		func (r *ProposerRunner) ProcessPostConsensus(signedMsg *types.SignedPartialSignatureMessage) error {
			quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(r, signedMsg)
			if err != nil {
				return errors.Wrap(err, "failed processing post consensus message")
			}
		
			if !quorum {
				return nil
			}
		
			for _, root := range roots {
				sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
				if err != nil {
					// If the reconstructed signature verification failed, fall back to verifying each partial signature
					for _, root := range roots {
						r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root)
					}
					return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
				}
				specSig := phase0.BLSSignature{}
				copy(specSig[:], sig)
		
				if r.decidedBlindedBlock() {
					vBlindedBlk, _, err := r.GetState().DecidedValue.GetBlindedBlockData()
					if err != nil {
						return errors.Wrap(err, "could not get blinded block")
					}
		
					if err := r.GetBeaconNode().SubmitBlindedBeaconBlock(vBlindedBlk, specSig); err != nil {
						return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed blinded Beacon block")
					}
				} else {
					vBlk, _, err := r.GetState().DecidedValue.GetBlockData()
					if err != nil {
						return errors.Wrap(err, "could not get block")
					}
		
					if err := r.GetBeaconNode().SubmitBeaconBlock(vBlk, specSig); err != nil {
						return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed Beacon block")
					}
				}
			}
			r.GetState().Finished = true
			return nil
		}`

	expectedOutput := `func (r *ProposerRunner) ProcessPostConsensus(signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}
	if !quorum {
		return nil
	}
	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)
		if r.decidedBlindedBlock() {
			vBlindedBlk, _, err := r.GetState().DecidedValue.GetBlindedBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get blinded block")
			}
			if err := r.GetBeaconNode().SubmitBlindedBeaconBlock(vBlindedBlk, specSig); err != nil {
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed blinded Beacon block")
			}
		} else {
			vBlk, _, err := r.GetState().DecidedValue.GetBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get block")
			}
			if err := r.GetBeaconNode().SubmitBeaconBlock(vBlk, specSig); err != nil {
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed Beacon block")
			}
		}
	}
	r.GetState().Finished = true
	return nil
}`

	// Create a temporary directory with this code in it.
	dir, err := os.MkdirTemp("", "differ-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(input), 0644)
	require.NoError(t, err)

	// Run the differ on the file.
	elems, err := NewParser(dir, []string{"./"}, []string{"logger"}).Parse()
	require.NoError(t, err)
	require.NotNil(t, elems)
	require.Equal(t, 1, len(elems))
	require.Equal(t, []string{"ProposerRunner.ProcessPostConsensus"}, slices.Collect(maps.Keys(elems)))

	elem := elems["ProposerRunner.ProcessPostConsensus"]
	require.Equal(t, "ProposerRunner.ProcessPostConsensus", elem.Name)

	transformers := Transformers{
		NoPackageNames([]string{"types", "qbft"}),
		NoComments(),
		NoEmptyLines(),
	}
	code := transformers.Transform(elem.Code)
	require.Equal(t, expectedOutput, code)
}
