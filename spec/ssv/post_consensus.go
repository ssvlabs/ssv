package ssv

import (
	"encoding/hex"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// ProcessPostConsensusMessage process post consensus msg, returns true if it has quorum for partial signatures.
// returns true only once (first time quorum achieved)
// returns signed message roots for which there is a quorum
func (dr *Runner) ProcessPostConsensusMessage(signedMsg *SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := dr.canProcessPostConsensusMsg(signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "can't process post consensus message")
	}

	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Messages {
		prevQuorum := dr.State.PostConsensusPartialSig.HasQuorum(msg.SigningRoot)

		if err := dr.State.PostConsensusPartialSig.AddSignature(msg); err != nil {
			return false, nil, errors.Wrap(err, "could not add partial post consensus signature")
		}

		if prevQuorum {
			continue
		}

		quorum := dr.State.PostConsensusPartialSig.HasQuorum(msg.SigningRoot)

		if quorum {
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

// SignDutyPostConsensus sets the Decided duty and partially signs the Decided data, returns a PartialSignatureMessage to be broadcasted or error
func (dr *Runner) SignDutyPostConsensus(decidedValue *types.ConsensusData, signer types.KeyManager) (PartialSignatureMessages, error) {
	dr.State.DecidedValue = decidedValue

	switch dr.BeaconRoleType {
	case types.BNRoleAttester:
		signedAttestation, r, err := signer.SignAttestation(decidedValue.AttestationData, decidedValue.Duty, dr.Share.SharePubKey)
		if err != nil {
			return nil, errors.Wrap(err, "failed to sign attestation")
		}

		dr.State.SignedAttestation = signedAttestation
		ret := &PartialSignatureMessage{
			SigningRoot:      r,
			PartialSignature: dr.State.SignedAttestation.Signature[:],
			Slot:             decidedValue.Duty.Slot,
			Signers:          []types.OperatorID{dr.Share.OperatorID},
		}
		dr.State.PostConsensusPartialSig.AddSignature(ret)
		return PartialSignatureMessages{
			ret,
		}, nil
	case types.BNRoleProposer:
		signedBlock, r, err := signer.SignBeaconBlock(decidedValue.BlockData, decidedValue.Duty, dr.Share.SharePubKey)
		if err != nil {
			return nil, errors.Wrap(err, "failed to sign block")
		}

		dr.State.SignedProposal = signedBlock
		ret := &PartialSignatureMessage{
			SigningRoot:      r,
			PartialSignature: dr.State.SignedProposal.Signature[:],
			Slot:             decidedValue.Duty.Slot,
			Signers:          []types.OperatorID{dr.Share.OperatorID},
		}
		dr.State.PostConsensusPartialSig.AddSignature(ret)
		return PartialSignatureMessages{
			ret,
		}, nil
	case types.BNRoleAggregator:
		signed, r, err := signer.SignAggregateAndProof(decidedValue.AggregateAndProof, decidedValue.Duty, dr.Share.SharePubKey)
		if err != nil {
			return nil, errors.Wrap(err, "failed to sign aggregate and proof")
		}

		dr.State.SignedAggregate = signed

		ret := &PartialSignatureMessage{
			SigningRoot:      r,
			PartialSignature: dr.State.SignedAggregate.Signature[:],
			Slot:             decidedValue.Duty.Slot,
			Signers:          []types.OperatorID{dr.Share.OperatorID},
		}
		dr.State.PostConsensusPartialSig.AddSignature(ret)
		return PartialSignatureMessages{
			ret,
		}, nil
	case types.BNRoleSyncCommittee:
		signed, r, err := signer.SignSyncCommitteeBlockRoot(decidedValue.Duty.Slot, decidedValue.SyncCommitteeBlockRoot, decidedValue.Duty.ValidatorIndex, dr.Share.SharePubKey)
		if err != nil {
			return nil, errors.Wrap(err, "failed to sign aggregate and proof")
		}

		dr.State.SignedSyncCommittee = signed

		ret := &PartialSignatureMessage{
			SigningRoot:      r,
			PartialSignature: dr.State.SignedSyncCommittee.Signature[:],
			Slot:             decidedValue.Duty.Slot,
			Signers:          []types.OperatorID{dr.Share.OperatorID},
		}
		dr.State.PostConsensusPartialSig.AddSignature(ret)
		return PartialSignatureMessages{
			ret,
		}, nil
	case types.BNRoleSyncCommitteeContribution:
		ret := PartialSignatureMessages{}
		dr.State.SignedContributions = make(map[string]*altair.SignedContributionAndProof)
		for proof, c := range decidedValue.SyncCommitteeContribution {
			contribAndProof := &altair.ContributionAndProof{
				AggregatorIndex: dr.CurrentDuty.ValidatorIndex,
				Contribution:    c,
				SelectionProof:  proof,
			}
			signed, r, err := signer.SignContribution(contribAndProof, dr.Share.SharePubKey)
			if err != nil {
				return nil, errors.Wrap(err, "failed to sign aggregate and proof")
			}

			dr.State.SignedContributions[hex.EncodeToString(r)] = signed

			m := &PartialSignatureMessage{
				SigningRoot:      r,
				PartialSignature: signed.Signature[:],
				Slot:             decidedValue.Duty.Slot,
				Signers:          []types.OperatorID{dr.Share.OperatorID},
			}
			dr.State.PostConsensusPartialSig.AddSignature(m)
			ret = append(ret, m)
		}
		return ret, nil
	default:
		return nil, errors.Errorf("unknown duty %s", decidedValue.Duty.Type.String())
	}
}

// canProcessPostConsensusMsg returns true if it can process post consensus message, false if not
func (dr *Runner) canProcessPostConsensusMsg(msg *SignedPartialSignatureMessage) error {
	if dr.State.RunningInstance == nil {
		return errors.New("no running instance")
	}

	if decided, _ := dr.State.RunningInstance.IsDecided(); !decided {
		return errors.New("consensus didn't decide")
	}

	if err := dr.validatePartialSigMsg(msg, dr.State.DecidedValue.Duty.Slot); err != nil {
		return errors.Wrap(err, "post consensus msg invalid")
	}

	return nil
}

func (dr *Runner) verifyBeaconPartialSignature(msg *PartialSignatureMessage) error {
	if len(msg.Signers) != 1 {
		return errors.New("PartialSignatureMessage allows 1 signer")
	}

	signer := msg.Signers[0]
	signature := msg.PartialSignature
	root := msg.SigningRoot

	for _, n := range dr.Share.Committee {
		if n.GetID() == signer {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(n.GetPublicKey()); err != nil {
				return errors.Wrap(err, "could not deserialized pk")
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return errors.Wrap(err, "could not deserialized Signature")
			}

			// protect nil root
			root = ensureRoot(root)
			// verify
			if !sig.VerifyByte(pk, root) {
				return errors.Errorf("could not verify Signature from iBFT member %d", signer)
			}
			return nil
		}
	}
	return errors.New("beacon partial Signature signer not found")
}

// ensureRoot ensures that SigningRoot will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root []byte) []byte {
	n := len(root)
	if n == 0 {
		n = 1
	}
	tmp := make([]byte, n)
	copy(tmp[:], root[:])
	return tmp[:]
}
