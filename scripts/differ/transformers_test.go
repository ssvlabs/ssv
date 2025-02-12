package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoPackageNames(t *testing.T) {
	input :=
		`types.One
*qbft.Two
types.One, qbft.Two
(types.One, &qbft.Two)

mtypes.One
mqbft.Two
typesm.One
qbftm.Two
types, One, spectypes, Two
types+One, spectypes+Two
types.One qbft.Two`
	expected :=
		`One
*Two
One, Two
(One, &Two)

mtypes.One
mqbft.Two
typesm.One
qbftm.Two
types, One, spectypes, Two
types+One, spectypes+Two
One qbft.Two`
	actual := NoPackageNames([]string{"types", "qbft"})(input)
	require.Equal(t, expected, actual)

	input2 := `msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}`
	expected2 := `msgToBroadcast := &SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}`
	actual2 := NoPackageNames([]string{"spectypes"})(input2)
	require.Equal(t, expected2, actual2)
}

func TestAll(t *testing.T) {
	input := `func (r *ProposerRunner) ProcessPreConsensus(signedMsg *types.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	// randao is relevant only for block proposals, no need to check type
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	duty := r.GetState().StartingDuty

	var ver spec.DataVersion
	var obj ssz.Marshaler
	if r.ProducesBlindedBlocks {
		// get block data
		obj, ver, err = r.GetBeaconNode().GetBlindedBeaconBlock(duty.Slot, r.GetShare().Graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get Beacon block")
		}
	} else {
		// get block data
		obj, ver, err = r.GetBeaconNode().GetBeaconBlock(duty.Slot, r.GetShare().Graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get Beacon block")
		}
	}

	byts, err := obj.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal beacon block")
	}

	input := &types.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}`

	expected := `func (r *ProposerRunner) ProcessPreConsensus(signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}
	if !quorum {
		return nil
	}
	root := roots[0]
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}
	duty := r.GetState().StartingDuty
	var ver spec.DataVersion
	var obj ssz.Marshaler
	if r.ProducesBlindedBlocks {
		obj, ver, err = r.GetBeaconNode().GetBlindedBeaconBlock(duty.Slot, r.GetShare().Graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get Beacon block")
		}
	} else {
		obj, ver, err = r.GetBeaconNode().GetBeaconBlock(duty.Slot, r.GetShare().Graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get Beacon block")
		}
	}
	byts, err := obj.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal beacon block")
	}
	input := &ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}
	if err := r.BaseRunner.decide(r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}`

	transformers := Transformers{
		NoComments(),
		NoPackageNames([]string{"spectypes", "specssv", "specqbft", "types", "ssv", "qbft", "instance", "controller"}),
		NoEmptyLines(),
	}
	actual := transformers.Transform(input)
	require.Equal(t, expected, actual)
}
