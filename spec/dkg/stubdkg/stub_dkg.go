package stubdkg

import (
	"github.com/bloxapp/ssv/spec/dkg"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// DKG is a stub dkg protocol simulating a real DKG protocol with 3 stages in it
type DKG struct {
	identifier dkg.RequestID
	network    dkg.Network
	operatorID types.OperatorID
	threshold  uint16

	//
	validatorPK    []byte
	operatorShares map[types.OperatorID]*bls.SecretKey

	msgs map[stage][]*protocolMsg
}

func New(network dkg.Network, operatorID types.OperatorID, identifier dkg.RequestID) dkg.Protocol {
	return &DKG{
		identifier: identifier,
		network:    network,
		operatorID: operatorID,
		msgs:       map[stage][]*protocolMsg{},
	}
}

func (s *DKG) SetOperators(validatorPK []byte, operatorShares map[types.OperatorID]*bls.SecretKey) {
	s.validatorPK = validatorPK
	s.operatorShares = operatorShares
}

func (s *DKG) Start(init *dkg.Init) error {
	s.threshold = init.Threshold
	// TODO send stage 1 msg
	return nil
}

func (s *DKG) ProcessMsg(msg *dkg.SignedMessage) (bool, *dkg.ProtocolOutput, error) {
	// TODO validate msg

	dataMsg := &protocolMsg{}
	if err := dataMsg.Decode(msg.Message.Data); err != nil {
		return false, nil, errors.Wrap(err, "could not decode protocol msg")
	}

	if s.msgs[dataMsg.Stage] == nil {
		s.msgs[dataMsg.Stage] = []*protocolMsg{}
	}
	s.msgs[dataMsg.Stage] = append(s.msgs[dataMsg.Stage], dataMsg)

	switch dataMsg.Stage {
	case stubStage1:
		if len(s.msgs[stubStage1]) >= int(s.threshold) {
			// TODO send stage 2 msg
		}
	case stubStage2:
		if len(s.msgs[stubStage2]) >= int(s.threshold) {
			// TODO send stage 3 msg
		}
	case stubStage3:
		if len(s.msgs[stubStage3]) >= int(s.threshold) {
			ret := &dkg.ProtocolOutput{
				Share:       s.operatorShares[s.operatorID],
				ValidatorPK: s.validatorPK,
				OperatorPubKeys: map[types.OperatorID]*bls.PublicKey{
					1: s.operatorShares[1].GetPublicKey(),
					2: s.operatorShares[2].GetPublicKey(),
					3: s.operatorShares[3].GetPublicKey(),
					4: s.operatorShares[4].GetPublicKey(),
				},
			}
			return true, ret, nil
		}
	}
	return false, nil, nil
}

func (s *DKG) signDKGMsg(data []byte) *dkg.SignedMessage {
	return &dkg.SignedMessage{
		Message: &dkg.Message{
			MsgType:    dkg.ProtocolMsgType,
			Identifier: s.identifier,
			Data:       data,
		},
		Signer: s.operatorID,
		// TODO - how do we sign?
	}
}
