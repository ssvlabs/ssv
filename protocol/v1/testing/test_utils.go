package testing

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/herumi/bls-eth-go-binary/bls"
)

// GenerateBLSKeys generates randomly nodes
func GenerateBLSKeys(oids ...message.OperatorID) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*message.Node) {
	_ = bls.Init(bls.BLS12_381)

	nodes := make(map[message.OperatorID]*message.Node)
	sks := make(map[message.OperatorID]*bls.SecretKey)

	for i, oid := range oids {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[oid] = &message.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[oid] = sk
	}

	return sks, nodes
}

// CreateMultipleSignedMessages enables to create multiple decided messages
func CreateMultipleSignedMessages(sks map[message.OperatorID]*bls.SecretKey, start message.Height, end message.Height,
	iterator func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage)) ([]*message.SignedMessage, error) {
	results := make([]*message.SignedMessage, 0)
	for i := start; i <= end; i++ {
		signers, msg := iterator(i)
		if msg == nil {
			break
		}
		sm, err := MultiSignMsg(sks, signers, msg)
		if err != nil {
			return nil, err
		}
		results = append(results, sm)
	}
	return results, nil
}

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(sks map[message.OperatorID]*bls.SecretKey, signers []message.OperatorID, msg *message.ConsensusMessage) (*message.SignedMessage, error) {
	_ = bls.Init(bls.BLS12_381)

	var operators = make([]message.OperatorID, 0)
	var agg *bls.Sign
	for _, oid := range signers {
		signature, err := msg.Sign(sks[oid])
		if err != nil {
			return nil, err
		}
		operators = append(operators, oid)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	return &message.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		Signers:   operators,
	}, nil
}
