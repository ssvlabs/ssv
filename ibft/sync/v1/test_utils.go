package v1

import (
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

// NewTestIbftStorage returns a testing storage
func NewTestIbftStorage(logger *zap.Logger, prefix string) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger.With(zap.String("who", "badger")),
		Path:   "",
	})
	if err != nil {
		return nil, err
	}
	return ibftstorage.New(db, logger.With(zap.String("who", "ibftStorage")), prefix), nil
}

// GenerateNodes generates randomly nodes
func GenerateNodes(oids ...message.OperatorID) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*message.Node) {
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

//
//// DecidedArr returns an array of signed decided msgs for the given identifier
//func DecidedArr(maxSeq uint64, sks map[uint64]*bls.SecretKey, identifier []byte) []*proto.SignedMessage {
//	ret := make([]*proto.SignedMessage, 0)
//	for i := uint64(0); i <= maxSeq; i++ {
//		ret = append(ret, MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
//			Type:      proto.RoundState_Decided,
//			Round:     1,
//			Lambda:    identifier[:],
//			SeqNumber: i,
//		}))
//	}
//	return ret
//}
//

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(sks map[message.OperatorID]*bls.SecretKey, msg *message.ConsensusMessage) (*message.SignedMessage, error) {
	bls.Init(bls.BLS12_381)

	var operators = make([]message.OperatorID, 0)
	var agg *bls.Sign
	for oid, sk := range sks {
		signature, err := msg.Sign(sk)
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
