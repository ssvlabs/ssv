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
	data, err := signedMessage.Message.GetRoundChangeData()
	if err != nil {
		return errors.Wrap(err, "failed to get round change data")
	}
	if data == nil {
		return errors.New("change round data is nil")
	}
	if data.GetPreparedValue() == nil { // no justification
		return nil
	}
	roundChangeJust := data.GetRoundChangeJustification()
	if roundChangeJust == nil {
		return errors.New("change round justification is nil")
	}
	if len(roundChangeJust) == 0 {
		return errors.New("change round justification msg array is empty")
	}
	if roundChangeJust[0].Message == nil {
		return errors.New("change round justification msg is nil")
	}
	if roundChangeJust[0].Message.MsgType != message.PrepareMsgType {
		return errors.Errorf("change round justification msg type not Prepare (%d)", roundChangeJust[0].Message.MsgType)
	}
	if signedMessage.Message.Height != roundChangeJust[0].Message.Height {
		return errors.New("change round justification sequence is wrong")
	}
	if signedMessage.Message.Round <= roundChangeJust[0].Message.Round {
		return errors.New("change round justification round lower or equal to message round")
	}
	if data.Round != roundChangeJust[0].Message.Round {
		return errors.New("change round prepared round not equal to justification msg round")
	}
	if !bytes.Equal(signedMessage.Message.Identifier, roundChangeJust[0].Message.Identifier) {
		return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
	}
	prepareMsg, err := roundChangeJust[0].Message.GetPrepareData()
	if err != nil {
		return errors.Wrap(err, "failed to get prepare data")
	}
	if !bytes.Equal(data.PreparedValue, prepareMsg.Data) {
		return errors.New("change round prepared value not equal to justification msg value")
	}
	if len(roundChangeJust[0].GetSigners()) < p.share.ThresholdSize() {
		return errors.New("change round justification does not constitute a quorum")
	}

	// validateJustification justification signature
	pksMap, err := p.share.PubKeysByID(data.GetRoundChangeJustification()[0].GetSigners())
	var pks beacon.PubKeys
	for _, v := range pksMap {
		pks = append(pks, v)
	}

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
