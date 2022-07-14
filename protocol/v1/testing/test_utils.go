package testing

import (
	"encoding/json"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

// GenerateBLSKeys generates randomly nodes
func GenerateBLSKeys(oids ...spectypes.OperatorID) (map[spectypes.OperatorID]*bls.SecretKey, map[spectypes.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)

	nodes := make(map[spectypes.OperatorID]*beacon.Node)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)

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

// MsgGenerator represents a message generator
type MsgGenerator func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message)

// CreateMultipleSignedMessages enables to create multiple decided messages
func CreateMultipleSignedMessages(sks map[spectypes.OperatorID]*bls.SecretKey, start specqbft.Height, end specqbft.Height,
	generator MsgGenerator) ([]*specqbft.SignedMessage, error) {
	results := make([]*specqbft.SignedMessage, 0)
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

func signMessage(msg *specqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := spectypes.ComputeSignatureDomain(spectypes.PrimusTestnet, spectypes.QBFTSignatureType)
	root, err := spectypes.ComputeSigningRoot(msg, signatureDomain)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root), nil
}

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, msg *specqbft.Message) (*specqbft.SignedMessage, error) {
	_ = bls.Init(bls.BLS12_381)

	var operators = make([]spectypes.OperatorID, 0)
	var agg *bls.Sign
	for _, oid := range signers {
		signature, err := signMessage(msg, sks[oid])
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

	return &specqbft.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		Signers:   operators,
	}, nil
}

// SignMsg handle MultiSignMsg error and return just specqbft.SignedMessage
func SignMsg(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, msg *specqbft.Message) *specqbft.SignedMessage {
	res, err := MultiSignMsg(sks, signers, msg)
	require.NoError(t, err)
	return res
}

// AggregateSign sign specqbft.Message and then aggregate
func AggregateSign(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, consensusMessage *specqbft.Message) *specqbft.SignedMessage {
	signedMsg := SignMsg(t, sks, signers, consensusMessage)
	// TODO: use SignMsg instead of AggregateSign
	//require.NoError(t, signedMsg.Aggregate(signedMsg))
	return signedMsg
}

// AggregateInvalidSign sign specqbft.Message and then change the signer id to mock invalid sig
func AggregateInvalidSign(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, consensusMessage *specqbft.Message) *specqbft.SignedMessage {
	sigend := SignMsg(t, sks, []spectypes.OperatorID{1}, consensusMessage)
	sigend.Signers = []spectypes.OperatorID{2}
	return sigend
}

// PopulatedStorage create new QBFTStore instance, save the highest height and then populated from 0 to highestHeight
func PopulatedStorage(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, round specqbft.Round, highestHeight specqbft.Height) qbftstorage.QBFTStore {
	s := qbftstorage.NewQBFTStore(NewInMemDb(), zap.L(), "test-qbft-storage")

	signers := make([]spectypes.OperatorID, 0, len(sks))
	for k := range sks {
		signers = append(signers, k)
	}

	identifier := spectypes.NewMsgID([]byte("Identifier_11"), spectypes.BNRoleAttester)
	for i := 0; i <= int(highestHeight); i++ {
		signedMsg := AggregateSign(t, sks, signers, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     specqbft.Height(i),
			Round:      round,
			Identifier: identifier[:],
			Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
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

func commitDataToBytes(t *testing.T, input *specqbft.CommitData) []byte {
	ret, err := json.Marshal(input)
	require.NoError(t, err)
	return ret
}
