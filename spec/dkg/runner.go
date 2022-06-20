package dkg

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// Runner manages the execution of a DKG, start to finish.
type Runner struct {
	Operator *Operator
	// InitMsg holds the init method which started this runner
	InitMsg *Init
	// Identifier unique for DKG session
	Identifier RequestID
	// ProtocolOutput holds the protocol output once it finishes
	ProtocolOutput *ProtocolOutput
	// DepositDataSignatures holds partial sigs on deposit data
	DepositDataSignatures map[types.OperatorID]*PartialDepositData

	protocol Protocol
	config   *Config
}

// ProcessMsg processes a DKG signed message and returns true and signed output if finished
func (r *Runner) ProcessMsg(msg *SignedMessage) (bool, *SignedOutput, error) {
	// TODO - validate message

	switch msg.Message.MsgType {
	case ProtocolMsgType:
		finished, o, err := r.protocol.ProcessMsg(msg)
		if err != nil {
			return false, nil, errors.Wrap(err, "failed to process dkg msg")
		}

		if finished {
			r.ProtocolOutput = o
		}

		// TODO broadcast partial deposit data
	case DepositDataMsgType:
		// TODO validate (including which Operator it is)

		depSig := &PartialDepositData{}
		if err := depSig.Decode(msg.Message.Data); err != nil {
			return false, nil, errors.Wrap(err, "could not decode PartialDepositData")
		}

		r.DepositDataSignatures[msg.Signer] = depSig
		if len(r.DepositDataSignatures) >= int(r.InitMsg.Threshold) {
			// reconstruct deposit data sig
			depositSig, err := r.reconstructDepositDataSignature()
			if err != nil {
				return false, nil, errors.Wrap(err, "could not reconstruct deposit data sig")
			}

			// encrypt Operator's share
			encryptedShare, err := r.config.Signer.Encrypt(r.Operator.EncryptionPubKey, r.ProtocolOutput.Share.Serialize())
			if err != nil {
				return false, nil, errors.Wrap(err, "could not encrypt share")
			}

			ret, err := r.generateSignedOutput(&Output{
				Identifier:            r.Identifier,
				EncryptedShare:        encryptedShare,
				DKGSetSize:            uint16(len(r.InitMsg.OperatorIDs)),
				ValidatorPubKey:       r.ProtocolOutput.ValidatorPK,
				WithdrawalCredentials: r.InitMsg.WithdrawalCredentials,
				SignedDepositData:     depositSig,
			})
			if err != nil {
				return false, nil, errors.Wrap(err, "could not generate dkg SignedOutput")
			}
			return true, ret, nil
		}
	default:
		return false, nil, errors.New("msg type invalid")
	}

	return false, nil, nil
}

func (r *Runner) reconstructDepositDataSignature() (types.Signature, error) {
	panic("implement")
}

func (r *Runner) generateSignedOutput(o *Output) (*SignedOutput, error) {
	sig, err := r.config.Signer.SignDKGOutput(o, r.Operator.ETHAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign output")
	}

	return &SignedOutput{
		Data:      o,
		Signer:    r.Operator.OperatorID,
		Signature: sig,
	}, nil
}
