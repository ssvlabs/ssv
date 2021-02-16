package changeround

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// validate implements pipeline.Pipeline interface
type validate struct {
	params *proto.InstanceParams
}

// Validate is the constructor of validate
func Validate(params *proto.InstanceParams) pipeline.Pipeline {
	return &validate{
		params: params,
	}
}

// Run implements pipeline.Pipeline interface
func (p *validate) Run(signedMessage *proto.SignedMessage) error {
	// msg.value holds the justification value in a change round message
	if signedMessage.Message.Value == nil {
		return nil
	}

	data := &proto.ChangeRoundData{}
	if err := json.Unmarshal(signedMessage.Message.Value, data); err != nil {
		return err
	}
	if data.JustificationMsg.Type != proto.RoundState_Prepare {
		return errors.New("change round justification msg type not Prepare")
	}
	if signedMessage.Message.Round <= data.JustificationMsg.Round {
		return errors.New("change round justification round lower or equal to message round")
	}
	if data.PreparedRound != data.JustificationMsg.Round {
		return errors.New("change round prepared round not equal to justification msg round")
	}
	if !bytes.Equal(signedMessage.Message.Lambda, data.JustificationMsg.Lambda) {
		return errors.New("change round justification msg Lambda not equal to msg Lambda")
	}
	if !bytes.Equal(data.PreparedValue, data.JustificationMsg.Value) {
		return errors.New("change round prepared value not equal to justification msg value")
	}

	// validate signature
	// TODO - validate signed ids are unique
	pks, err := p.params.PubKeysById(data.SignerIds)
	if err != nil {
		return err
	}

	aggregated := pks.Aggregate()
	res, err := data.VerifySig(aggregated)
	if err != nil {
		return err
	}
	if !res {
		return errors.New("change round justification signature doesn't verify")
	}

	return nil
}
