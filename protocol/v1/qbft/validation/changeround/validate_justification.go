package changeround

import (
	"bytes"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/pkg/errors"
)

// validateJustification validates change round justifications
type validateJustification struct {
	share *beacon.Share
}

// Validate is the constructor of validateJustification
func Validate(share *beacon.Share) pipelines.SignedMessagePipeline {
	return &validateJustification{
		share: share,
	}
}

// Run implements pipeline.Pipeline interface
func (p *validateJustification) Run(signedMessage *message.SignedMessage) error {
	// TODO - change to normal prepare pipeline
	if signedMessage.Message.Data == nil {
		return errors.New("change round justification msg is nil")
	}
	data, err := signedMessage.Message.GetRoundChangeData()
	if err != nil {
		return errors.Wrap(err, "failed to get round change data")
	}

	if data.PreparedValue == nil { // no justification
		return nil
	}
	if data.GetRoundChangeJustification() == nil {
		return errors.New("change round justification msg is nil")
	}
	if data.GetRoundChangeJustification()[0].Message.MsgType != message.RoundChangeMsgType {
		return errors.New("change round justification msg type not Prepare")
	}
	if signedMessage.Message.Height != data.GetRoundChangeJustification()[0].Message.Height {
		return errors.New("change round justification sequence is wrong")
	}
	if signedMessage.Message.Round <= data.GetRoundChangeJustification()[0].Message.Round {
		return errors.New("change round justification round lower or equal to message round")
	}
	if data.Round != data.GetRoundChangeJustification()[0].Message.Round {
		return errors.New("change round prepared round not equal to justification msg round")
	}
	if !bytes.Equal(signedMessage.Message.Identifier, data.GetRoundChangeJustification()[0].Message.Identifier) {
		return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
	}
	if !bytes.Equal(data.PreparedValue, data.GetRoundChangeJustification()[0].Message.Data) {
		return errors.New("change round prepared value not equal to justification msg value")
	}
	if len(data.GetRoundChangeJustification()[0].GetSigners()) < p.share.ThresholdSize() {
		return errors.New("change round justification does not constitute a quorum")
	}

	// validateJustification justification signature
	pks, err := p.share.PubKeysByID(data.GetRoundChangeJustification()[0].GetSigners())
	if err != nil {
		return errors.Wrap(err, "change round could not get pubkey")
	}
	aggregated := pks.Aggregate()
	err = signedMessage.GetSignature().Verify(signedMessage, message.PrimusTestnet, message.QBFTSigType, aggregated.Serialize())
	if err != nil {
		return errors.Wrap(err, "change round could not verify signature")

	}
	return nil
}

// Name implements pipeline.Pipeline interface
func (p *validateJustification) Name() string {
	return "validateJustification msg"
}
