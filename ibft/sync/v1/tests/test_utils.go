package tests

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/storage"
	p2p "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

type validatorData struct {
	PK       string
	sks      map[message.OperatorID]*bls.SecretKey
	nodes    map[message.OperatorID]*message.Node
	messages []*message.SignedMessage
}

func changeRoundGenerator(rounder func() message.Round) syncMsgGenerator {
	return func(height message.Height, pk []byte, oids ...message.OperatorID) ([]message.OperatorID, *message.ConsensusMessage) {
		return oids[1:], &message.ConsensusMessage{
			MsgType:    message.RoundChangeMsgType,
			Height:     height,
			Round:      rounder(),
			Identifier: message.NewIdentifier(pk, beacon.RoleTypeAttester),
			Data:       []byte("data"),
		}
	}
}

func decidedGenerator(height message.Height, pk []byte, oids ...message.OperatorID) ([]message.OperatorID, *message.ConsensusMessage) {
	return oids[1:], &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     height,
		Round:      1,
		Identifier: message.NewIdentifier(pk, beacon.RoleTypeAttester),
		Data:       []byte("data"),
	}
}

type syncMsgGenerator func(message.Height, []byte, ...message.OperatorID) ([]message.OperatorID, *message.ConsensusMessage)

func createNetworkWithValidators(ctx context.Context, loggerFactory func(string) *zap.Logger, nNodes int, pks []string,
	generator syncMsgGenerator) (*p2p.LocalNet, []*validatorData, error) {
	ln, err := p2p.CreateAndStartLocalNet(ctx, loggerFactory, forksprotocol.V1ForkVersion, nNodes, nNodes/2, false)
	if err != nil {
		return nil, nil, err
	}

	oids := make([]message.OperatorID, 0)
	for i := 1; i <= len(ln.NodeKeys); i++ {
		oids = append(oids, message.OperatorID(i))
	}

	nShares := len(pks)

	validators := make([]*validatorData, 0)

	for i := 0; i < nShares; i++ {
		sks, nodes := testingprotocol.GenerateBLSKeys(oids...)
		pk, err := hex.DecodeString(pks[i])
		if err != nil {
			return nil, nil, err
		}
		messages, err := testingprotocol.CreateMultipleSignedMessages(sks, message.Height(0), message.Height(10),
			func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
				return generator(height, pk, oids...)
			})
		if err != nil {
			return nil, nil, err
		}
		validators = append(validators, &validatorData{
			PK:       pks[i],
			sks:      sks,
			nodes:    nodes,
			messages: messages,
		})
	}

	return ln, validators, nil
}

func newTestIbftStorage(logger *zap.Logger, prefix string) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger.With(zap.String("who", "badger")),
		Path:   "",
	})
	if err != nil {
		return nil, err
	}
	return storage.New(db, logger.With(zap.String("who", "ibftStorage")), prefix), nil
}
