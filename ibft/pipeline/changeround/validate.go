package changeround

import (
	"bytes"
	"encoding/json"
	"github.com/pkg/errors"

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
	if signedMessage.Message.Value == nil {
		return errors.New("change round justification msg is nil")
	}
	data := &proto.ChangeRoundData{}
	if err := json.Unmarshal(signedMessage.Message.Value, data); err != nil {
		return err
	}
	if data.PreparedValue == nil { // no justification
		return nil
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
		return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
	}
	if !bytes.Equal(data.PreparedValue, data.JustificationMsg.Value) {
		return errors.New("change round prepared value not equal to justification msg value")
	}
	if len(data.SignerIds) < p.params.ThresholdSize() {
		return errors.New("change round justification does not constitute a quorum")
	}

	// validate justification signature
	pks, err := p.params.PubKeysByID(data.SignerIds)
	if err != nil {
		return errors.Wrap(err, "change round could not get pubkey")
	}
	aggregated := pks.Aggregate()
	res, err := data.VerifySig(aggregated)
	if err != nil {
		return errors.Wrap(err, "change round could not verify signature")

	}
	if !res {
		return errors.New("change round justification signature doesn't verify")
	}

	return nil
}

// Name implements pipeline.Pipeline interface
func (p *validate) Name() string {
	return "validate msg"
}
