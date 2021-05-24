package sync

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/inmem"
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
		valdiatorPK    []byte
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
			[]string{"2"},
			"2",
			[]uint64{1, 3, 3},
			map[string][]*proto.SignedMessage{
				"2": {
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   1,
						ValidatorPk: []byte{1, 2, 3, 4},
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   2,
						ValidatorPk: []byte{1, 2, 3, 4},
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   3,
						ValidatorPk: []byte{1, 2, 3, 4},
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
			[]string{"2"},
			"2",
			[]uint64{1, 3, 2},
			map[string][]*proto.SignedMessage{
				"2": {
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   1,
						ValidatorPk: []byte{1, 2, 3, 4},
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   2,
						ValidatorPk: []byte{1, 2, 3, 4},
					}),
					multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:        proto.RoundState_Decided,
						Round:       1,
						Lambda:      []byte("lambda"),
						SeqNumber:   3,
						ValidatorPk: []byte{1, 2, 3, 4},
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
			storage := collections.NewIbft(inmem.New(), zap.L(), "attestation")
			network := newTestNetwork(t, test.peers, int(test.rangeParams[2]), nil, test.decidedArr, nil)
			s := NewHistorySync(zap.L(), test.valdiatorPK, network, &storage, func(msg *proto.SignedMessage) error {
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
		Type:        proto.RoundState_Decided,
		Round:       1,
		Lambda:      []byte("lambda"),
		SeqNumber:   1,
		ValidatorPk: []byte{1, 2, 3, 4},
	})
	highest2 := multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		Type:        proto.RoundState_Decided,
		Round:       1,
		Lambda:      []byte("lambda"),
		SeqNumber:   1,
		ValidatorPk: []byte{1, 2, 3, 4},
	})

	tests := []struct {
		name               string
		valdiatorPK        []byte
		peers              []string
		highestMap         map[string]*proto.SignedMessage
		expectedHighestSeq uint64
		expectedError      string
	}{
		{
			"valid",
			[]byte{1, 2, 3, 4},
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": highest1,
			},
			1,
			"",
		},
		{
			"valid multi responses",
			[]byte{1, 2, 3, 4},
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": highest1,
				"3": highest2,
			},
			1,
			"",
		},
		{
			"valid multi responses different seq",
			[]byte{1, 2, 3, 4},
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": highest1,
				"3": multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
					Type:        proto.RoundState_Decided,
					Round:       1,
					Lambda:      []byte("lambda"),
					SeqNumber:   10,
					ValidatorPk: []byte{1, 2, 3, 4},
				}),
			},
			10,
			"",
		},
		{
			"invalid validator pk",
			[]byte{1, 1, 1, 1},
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": highest1,
			},
			1,
			"could not fetch highest decided from peers",
		},
		//
		// Msg not decided test is out of scope for history sync as msg validation is provided as a param
		//
		//{
		//	"no quorum msg",
		//	[]byte{1, 2, 3, 4},
		//	[]peer.ID{"2", "3"},
		//	map[peer.ID]*proto.SignedMessage{
		//		"2": multiSignMsg(t, []uint64{1, 2}, sks, &proto.Message{
		//			Type:        proto.RoundState_Decided,
		//			Round:       1,
		//			Lambda:      []byte("lambda"),
		//			SeqNumber:   1,
		//			ValidatorPk: []byte{1, 2, 3, 4},
		//		}),
		//	},
		//	1,
		//	"could not fetch highest decided from peers",
		//},
		{
			"wrong pk",
			[]byte{1, 1, 1, 1},
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
					Type:        proto.RoundState_Decided,
					Round:       1,
					Lambda:      []byte("lambda"),
					SeqNumber:   1,
					ValidatorPk: []byte{1, 2, 3, 4},
				}),
			},
			1,
			"could not fetch highest decided from peers",
		},
		//
		// Msg not decided test is out of scope for history sync as msg validation is provided as a param
		//
		//{
		//	"return not decided",
		//	[]byte{1, 2, 3, 4},
		//	[]peer.ID{"2", "3"},
		//	map[peer.ID]*proto.SignedMessage{
		//		"2": multiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
		//			Type:        proto.RoundState_Prepare,
		//			Round:       1,
		//			Lambda:      []byte("lambda"),
		//			SeqNumber:   1,
		//			ValidatorPk: []byte{1, 2, 3, 4},
		//		}),
		//	},
		//	1,
		//	"could not fetch highest decided from peers",
		//},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := NewHistorySync(zap.L(),
				test.valdiatorPK,
				newTestNetwork(t, test.peers, 100, test.highestMap, nil, nil),
				nil,
				func(msg *proto.SignedMessage) error {
					return nil
				})
			res, _, err := s.findHighestInstance()

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, test.expectedHighestSeq, res.Message.SeqNumber)
			}
		})
	}
}
