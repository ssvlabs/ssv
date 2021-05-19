package ibft

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/inmem"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func validatorPK(sks map[uint64]*bls.SecretKey) *bls.PublicKey {
	return sks[1].GetPublicKey()
}

func populatedStorage(t *testing.T, sks map[uint64]*bls.SecretKey) collections.Iibft {
	storage := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	highest := 10
	for i := 0; i <= highest; i++ {
		var aggSignedMsg *proto.SignedMessage
		for index, sk := range sks {
			sigend := SignMsg(t, index, sk, &proto.Message{
				Type:        proto.RoundState_Decided,
				Round:       3,
				SeqNumber:   uint64(i),
				ValidatorPk: validatorPK(sks).Serialize(),
				Lambda:      []byte("Lambda"),
				Value:       []byte("value"),
			})

			if aggSignedMsg == nil {
				aggSignedMsg = sigend
			} else {
				require.NoError(t, aggSignedMsg.Aggregate(sigend))
			}
		}
		require.NoError(t, storage.SaveDecided(aggSignedMsg))
		if i == highest {
			require.NoError(t, storage.SaveHighestDecidedInstance(aggSignedMsg))
		}
	}
	return &storage
}

func populatedIbft(
	nodeId uint64,
	network *local.Local,
	storage collections.Iibft,
	sks map[uint64]*bls.SecretKey,
	nodes map[uint64]*proto.Node,
) IBFT {
	queue := msgqueue.New()
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	share := &collections.ValidatorShare{
		NodeID:      nodeId,
		ValidatorPK: validatorPK(sks),
		ShareKey:    sks[nodeId],
		Committee:   nodes,
	}
	return New(
		zap.L(),
		storage,
		network.CopyWithLocalNodeID(peer.ID(fmt.Sprintf("%d", nodeId-1))),
		queue,
		params,
		share,
	)
}

func TestSyncFromScratch(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	i1 := populatedIbft(1, network, &s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks), sks, nodes)
	//i3 := populatedIbft(3, network, populatedStorage(t, sks), sks, nodes)
	//i4 := populatedIbft(4, network, populatedStorage(t, sks), sks, nodes)

	i1.(*ibftImpl).SyncIBFT()
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}
