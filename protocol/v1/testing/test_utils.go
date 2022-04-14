package testing

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

// GenerateBLSKeys generates randomly nodes
func GenerateBLSKeys(oids ...message.OperatorID) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)

	nodes := make(map[message.OperatorID]*beacon.Node)
	sks := make(map[message.OperatorID]*bls.SecretKey)

	for i, oid := range oids {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[oid] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[oid] = sk
	}

	return sks, nodes
}

type MsgGenerator func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage)

// CreateMultipleSignedMessages enables to create multiple decided messages
func CreateMultipleSignedMessages(sks map[message.OperatorID]*bls.SecretKey, start message.Height, end message.Height,
	generator MsgGenerator) ([]*message.SignedMessage, error) {
	results := make([]*message.SignedMessage, 0)
	for i := start; i <= end; i++ {
		signers, msg := generator(i)
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

// SignMsg handle MultiSignMsg error and return just message.SignedMessage
func SignMsg(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, signers []message.OperatorID, msg *message.ConsensusMessage) *message.SignedMessage {
	res, err := MultiSignMsg(sks, signers, msg)
	require.NoError(t, err)
	return res
}

// AggregateSign sign message.ConsensusMessage and then aggregate
func AggregateSign(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, signers []message.OperatorID, consensusMessage *message.ConsensusMessage) *message.SignedMessage {
	signedMsg := SignMsg(t, sks, signers, consensusMessage)
	require.NoError(t, signedMsg.Aggregate(signedMsg))
	return signedMsg
}

// AggregateInvalidSign sign message.ConsensusMessage and then change the signer id to mock invalid sig
func AggregateInvalidSign(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, consensusMessage *message.ConsensusMessage) *message.SignedMessage {
	sigend := SignMsg(t, sks, []message.OperatorID{1}, consensusMessage)
	sigend.Signers = []message.OperatorID{2}
	return sigend
}

// PopulatedStorage create new QBFTStore instance, save the highest height and then populated from 0 to highestHeight
func PopulatedStorage(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, round message.Round, highestHeight message.Height) qbftstorage.QBFTStore {
	s := qbftstorage.NewQBFTStore(newInMemDb(), logex.GetLogger(zap.String("who", "qbftStore")), "test-qbft-storage")

	signers := make([]message.OperatorID, len(sks))
	for k := range sks {
		signers = append(signers, k)
	}

	lambda := []byte("lambda_11")
	for i := 0; i <= int(highestHeight); i++ {
		signedMsg := AggregateSign(t, sks, signers, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     message.Height(i),
			Round:      round,
			Identifier: lambda,
			Data:       []byte("value"),
		})
		require.NoError(t, s.SaveDecided(signedMsg))
		if i == int(highestHeight) {
			require.NoError(t, s.SaveLastDecided(signedMsg))
		}
	}
	return s
}

// NewInMemDb returns basedb.IDb with in-memory type
func NewInMemDb() basedb.IDb {
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	return db
}
