package changeround

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// validateJustification validates change round justifications
type validateJustification struct {
	share *message.Share
}

// Validate is the constructor of validateJustification
func Validate(share *message.Share) pipeline.Pipeline {
	return &validateJustification{
		share: share,
	}
}

// Run implements pipeline.Pipeline interface
func (p *validateJustification) Run(signedMessage *proto.SignedMessage) error {
	// TODO - change to normal prepare pipeline
	//if signedMessage.Message.Value == nil {
	//	return errors.New("change round justification msg is nil")
	//}
	//data := &message.RoundChangeData{}
	//if err := json.Unmarshal(signedMessage.Message.Value, data); err != nil {
	//	return err
	//}
	//if data.PreparedValue == nil { // no justification
	//	return nil
	//}
	//if data.JustificationMsg == nil {
	//	return errors.New("change round justification msg is nil")
	//}
	//if data.JustificationMsg.Type != proto.RoundState_Prepare {
	//	return errors.New("change round justification msg type not Prepare")
	//}
	//if signedMessage.Message.SeqNumber != data.JustificationMsg.SeqNumber {
	//	return errors.New("change round justification sequence is wrong")
	//}
	//if signedMessage.Message.Round <= data.JustificationMsg.Round {
	//	return errors.New("change round justification round lower or equal to message round")
	//}
	//if data.PreparedRound != data.JustificationMsg.Round {
	//	return errors.New("change round prepared round not equal to justification msg round")
	//}
	//if !bytes.Equal(signedMessage.Message.Lambda, data.JustificationMsg.Lambda) {
	//	return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
	//}
	//if !bytes.Equal(data.PreparedValue, data.JustificationMsg.Value) {
	//	return errors.New("change round prepared value not equal to justification msg value")
	//}
	//if len(data.SignerIds) < p.share.ThresholdSize() {
	//	return errors.New("change round justification does not constitute a quorum")
	//}
	//
	//// validateJustification justification signature
	//pks, err := p.share.PubKeysByID(data.SignerIds)
	//if err != nil {
	//	return errors.Wrap(err, "change round could not get pubkey")
	//}
	//aggregated := pks.Aggregate()
	//res, err := data.VerifySig(aggregated)
	//if err != nil {
	//	return errors.Wrap(err, "change round could not verify signature")
	//
	//}
	//if !res {
	//	return errors.New("change round justification signature doesn't verify")
	//}

	return nil
}

// Name implements pipeline.Pipeline interface
func (p *validateJustification) Name() string {
	return "validateJustification msg"
}
