package qbftstorage

import (
	"encoding/binary"
	"encoding/json"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	testing2 "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	highestKey         = "highest"
	decidedKey         = "decided"
	currentKey         = "current"
	lastChangeRoundKey = "last_change_round"
)

// ibftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type ibftStorage struct {
	prefix []byte
	db     basedb.IDb
	logger *zap.Logger
}

// NewQBFTStore create new ibft storage
func NewQBFTStore(db basedb.IDb, logger *zap.Logger, instanceType string) QBFTStore {
	ibft := &ibftStorage{
		prefix: []byte(instanceType),
		db:     db,
		logger: logger,
	}
	return ibft
}

// GetLastDecided gets a signed message for an ibft instance which is the highest
func (i *ibftStorage) GetLastDecided(identifier []byte) (*specqbft.SignedMessage, error) {
	val, found, err := i.get(highestKey, identifier[:])
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ret := &specqbft.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

// SaveLastDecided saves a signed message for an ibft instance which is currently highest
func (i *ibftStorage) SaveLastDecided(signedMsgs ...*specqbft.SignedMessage) error {
	for _, signedMsg := range signedMsgs {
		value, err := json.Marshal(signedMsg)
		if err != nil {
			return errors.Wrap(err, "marshaling error")
		}
		if err = i.save(value, highestKey, signedMsg.Message.Identifier); err != nil {
			return err
		}
	}

	return nil
}

func (i *ibftStorage) GetDecided(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*specqbft.SignedMessage, error) {
	prefix := make([]byte, len(i.prefix))
	copy(prefix, i.prefix)
	prefix = append(prefix, identifier[:]...)

	var sequences [][]byte
	for seq := from; seq <= to; seq++ {
		sequences = append(sequences, i.key(decidedKey, uInt64ToByteSlice(uint64(seq))))
	}
	msgs := make([]*specqbft.SignedMessage, 0)
	err := i.db.GetMany(prefix, sequences, func(obj basedb.Obj) error {
		msg := specqbft.SignedMessage{}
		if err := json.Unmarshal(obj.Value, &msg); err != nil {
			return errors.Wrap(err, "un-marshaling error")
		}
		msgs = append(msgs, &msg)
		return nil
	})
	if err != nil {
		return []*specqbft.SignedMessage{}, err
	}
	return msgs, nil
}

func (i *ibftStorage) SaveDecided(signedMsg ...*specqbft.SignedMessage) error {
	return i.db.SetMany(i.prefix, len(signedMsg), func(j int) (basedb.Obj, error) {
		msg := signedMsg[j]
		k := i.key(decidedKey, uInt64ToByteSlice(uint64(msg.Message.Height)))
		key := append(msg.Message.Identifier, k...)
		value, err := json.Marshal(msg)
		if err != nil {
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: key, Value: value}, nil
	})
}

func (i *ibftStorage) CleanAllDecided(msgID []byte) error {
	//TODO implement me
	panic("implement me")
}

func (i *ibftStorage) SaveCurrentInstance(identifier []byte, state *qbft.State) error {
	value, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, currentKey, identifier[:])
}

func (i *ibftStorage) GetCurrentInstance(identifier []byte) (*qbft.State, bool, error) {
	val, found, err := i.get(currentKey, identifier[:])
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, false, err
	}
	ret := &qbft.State{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, false, nil
}

// SaveLastChangeRoundMsg updates last change round message
// TODO
func (i *ibftStorage) SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}

	var signers [][]byte
	for _, s := range msg.GetSigners() {
		signers = append(signers, uInt64ToByteSlice(uint64(s)))
	}

	return i.save(value, lastChangeRoundKey, msg.Message.Identifier, signers...)
}

// GetLastChangeRoundMsg returns last known change round message
// TODO
func (i *ibftStorage) GetLastChangeRoundMsg(identifier []byte, signers ...spectypes.OperatorID) ([]*specqbft.SignedMessage, error) {
	res, err := i.getAll(lastChangeRoundKey, identifier[:])
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (i *ibftStorage) CleanLastChangeRound(identifier []byte) error {
	prefix := i.prefix
	prefix = append(prefix, identifier[:]...)
	prefix = append(prefix, []byte(lastChangeRoundKey)...)
	if err := i.db.RemoveAllByCollection(prefix); err != nil {
		return errors.Wrap(err, "failed to remove decided")
	}
	return nil
}

func (i *ibftStorage) save(value []byte, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Set(prefix, key, value)
}

func (i *ibftStorage) get(id string, pk []byte, keyParams ...[]byte) ([]byte, bool, error) {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	obj, found, err := i.db.Get(prefix, key)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	return obj.Value, found, nil
}

func (i *ibftStorage) getAll(id string, pk []byte) ([]*specqbft.SignedMessage, error) {
	prefix := append(i.prefix, pk...)
	prefix = append(prefix, id...)

	var res []*specqbft.SignedMessage
	err := i.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		msg := new(specqbft.SignedMessage)
		if err := msg.Decode(obj.Value); err != nil {
			return err
		}
		res = append(res, msg)
		return nil
	})

	return res, err
}

/*func (i *ibftStorage) delete(id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Delete(prefix, key)
}*/

func (i *ibftStorage) key(id string, params ...[]byte) []byte {
	ret := []byte(id)
	for _, p := range params {
		ret = append(ret, p...)
	}
	return ret
}

func uInt64ToByteSlice(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}

// PopulatedStorage create new QBFTStore instance, save the highest height and then populated from 0 to highestHeight
func PopulatedStorage(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, round specqbft.Round, highestHeight specqbft.Height) QBFTStore {
	s := NewQBFTStore(testing2.NewInMemDb(), zap.L(), "test-qbft-storage")

	signers := make([]spectypes.OperatorID, 0, len(sks))
	for k := range sks {
		signers = append(signers, k)
	}

	identifier := spectypes.NewMsgID([]byte("Identifier_11"), spectypes.BNRoleAttester)
	for i := 0; i <= int(highestHeight); i++ {
		signedMsg := testing2.AggregateSign(t, sks, signers, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     specqbft.Height(i),
			Round:      round,
			Identifier: identifier[:],
			Data:       testing2.CommitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
		})
		require.NoError(t, s.SaveDecided(signedMsg))
		if i == int(highestHeight) {
			require.NoError(t, s.SaveLastDecided(signedMsg))
		}
	}
	return s
}
