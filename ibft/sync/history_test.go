package sync

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func multiSignMsg(t *testing.T, ids []uint64, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	var agg *bls.Sign
	for _, id := range ids {
		signature, err := msg.Sign(sks[id])
		require.NoError(t, err)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	return &proto.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		SignerIds: ids,
	}
}

func TestFetchDecided(t *testing.T) {
	sks, _ := GenerateNodes(4)
	tests := []struct {
		name           string
		validatorPk    []byte
		identifier     []byte
		peers          []string
		fromPeer       string
		rangeParams    []uint64
		decidedArr     map[string][]*proto.SignedMessage
		expectedError  string
		forceError     error
		expectedResLen uint64
	}{
		{
			"valid fetch no pagination",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 3},
			map[string][]*proto.SignedMessage{
				"2": {
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 1,
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 2,
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 3,
					}),
				},
			},
			"",
			nil,
			3,
		},
		{
			"valid fetch with pagination",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 2},
			map[string][]*proto.SignedMessage{
				"2": {
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 1,
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 2,
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 3,
					}),
				},
			},
			"",
			nil,
			3,
		},
		{
			"force error",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 2},
			map[string][]*proto.SignedMessage{},
			"could not find highest",
			errors.New("error"),
			3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := zap.L()
			db, err := kv.New(basedb.Options{
				Type:   "badger-memory",
				Path:   "",
				Logger: logger,
			})
			require.NoError(t, err)
			storage := collections.NewIbft(db, logger, "attestation")
			network := newTestNetwork(t, test.peers, int(test.rangeParams[2]), nil, nil, test.decidedArr, nil)
			s := NewHistorySync(logger, test.validatorPk, test.identifier, network, &storage, func(msg *proto.SignedMessage) error {
				return nil
			})
			res, err := s.fetchValidateAndSaveInstances(test.fromPeer, test.rangeParams[0], test.rangeParams[1])

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, res.Message.SeqNumber, test.expectedResLen)
			}

		})
	}
}

func TestFindHighest(t *testing.T) {
	sks, _ := GenerateNodes(4)
	highest1 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 1,
	})
	highest2 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 1,
	})

	tests := []struct {
		name               string
		valdiatorPK        []byte
		identifier         []byte
		peers              []string
		highestMap         map[string]*proto.SignedMessage
		errorMap           map[string]error
		expectedHighestSeq int64
		expectedError      string
	}{
		{
			"valid",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": highest1,
			},
			nil,
			1,
			"",
		},
		{
			"all responses are empty",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"1": nil,
				"2": nil,
			},
			nil,
			-1,
			"",
		},
		{
			"all errors",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{},
			map[string]error{
				"1": errors.New("error"),
				"2": errors.New("error"),
			},
			-1,
			"could not fetch highest decided from peers",
		},
		{
			"some errors, some valid",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": highest1,
			},
			map[string]error{
				"1": errors.New("error"),
			},
			1,
			"",
		},
		{
			"valid multi responses",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": highest1,
				"3": highest2,
			},
			nil,
			1,
			"",
		},
		{
			"valid multi responses different seq",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": highest1,
				"3": multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
					Type:      proto.RoundState_Decided,
					Round:     1,
					Lambda:    []byte("lambda"),
					SeqNumber: 10,
				}),
			},
			nil,
			10,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := NewHistorySync(zap.L(), test.valdiatorPK, test.identifier, newTestNetwork(t, test.peers, 100, test.highestMap, test.errorMap, nil, nil), nil, func(msg *proto.SignedMessage) error {
				return nil
			})
			res, _, err := s.findHighestInstance()

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				if test.expectedHighestSeq == -1 {
					require.Nil(t, res)
				} else {
					require.EqualValues(t, test.expectedHighestSeq, res.Message.SeqNumber)
				}
			}
		})
	}
}

func ibftStorage(t *testing.T) collections.IbftStorage {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	})
	require.NoError(t, err)
	return collections.NewIbft(db, zap.L(), "attestation")
}

func TestSync(t *testing.T) {
	sks, _ := GenerateNodes(4)
	decided0 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 0,
	})
	decided1 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 1,
	})
	decided2 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 2,
	})

	tests := []struct {
		name               string
		valdiatorPK        []byte
		identifier         []byte
		peers              []string
		highestMap         map[string]*proto.SignedMessage
		decidedArrMap      map[string][]*proto.SignedMessage
		errorMap           map[string]error
		expectedHighestSeq int64
		expectedError      string
	}{
		{
			"sync to seq 0",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided0,
				"3": decided0,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0},
				"3": {decided0},
			},
			nil,
			0,
			"",
		},
		{
			"sync to seq 1",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided1,
				"3": decided1,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0, decided1},
			},
			nil,
			1,
			"",
		},
		{
			"sync to seq 2",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1, decided2},
				"3": {decided0, decided1, decided2},
			},
			nil,
			2,
			"",
		},
		{
			"try syncing to seq 2 with missing the last, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0, decided1},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 2",
		},
		{
			"try syncing to seq 2 with missing in middle, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided2},
				"3": {decided0, decided2},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 1",
		},
		{
			"try syncing to seq 2 with missing in start, error on history fetch",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided2,
				"3": decided2,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided1, decided2},
				"3": {decided1, decided2},
			},
			nil,
			2,
			"could not fetch decided by range during sync: returned decided by range messages miss sequence number 0",
		},
		{
			"try syncing with differ decided peers",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": decided1,
				"3": decided0,
			},
			map[string][]*proto.SignedMessage{
				"2": {decided0, decided1},
				"3": {decided0},
			},
			nil,
			1,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := ibftStorage(t)
			s := NewHistorySync(zap.L(), test.valdiatorPK, test.identifier, newTestNetwork(t, test.peers, 100, test.highestMap, test.errorMap, test.decidedArrMap, nil), &storage, func(msg *proto.SignedMessage) error {
				return nil
			})
			err := s.Start()

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				if test.expectedHighestSeq == -1 {
					//require.Nil(t, res)
				} else {
					// verify saved instances
					for i := int64(0); i <= test.expectedHighestSeq; i++ {
						decided, err := storage.GetDecided(test.identifier, uint64(i))
						require.NoError(t, err)
						require.EqualValues(t, uint64(i), decided.Message.SeqNumber)
					}
					// verify saved highest
					highest, err := storage.GetHighestDecidedInstance(test.identifier)
					require.NoError(t, err)
					require.EqualValues(t, test.expectedHighestSeq, highest.Message.SeqNumber)
				}
			}
		})
	}
}
