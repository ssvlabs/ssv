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

func aggregateInvalidSign(t *testing.T, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	sigend := SignMsg(t, 1, sks[1], msg)
	sigend.SignerIds = []uint64{2}
	return sigend
}

func aggregateSign(t *testing.T, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	var aggSignedMsg *proto.SignedMessage
	for index, sk := range sks {
		sigend := SignMsg(t, index, sk, msg)

		if aggSignedMsg == nil {
			aggSignedMsg = sigend
		} else {
			require.NoError(t, aggSignedMsg.Aggregate(sigend))
		}
	}
	return aggSignedMsg
}

func populatedStorage(t *testing.T, sks map[uint64]*bls.SecretKey, highestSeq int) collections.Iibft {
	storage := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	for i := 0; i <= highestSeq; i++ {
		lambda := []byte(fmt.Sprintf("lambda_%d", i))
		aggSignedMsg := aggregateSign(t, sks, &proto.Message{
			Type:        proto.RoundState_Commit,
			Round:       3,
			SeqNumber:   uint64(i),
			ValidatorPk: validatorPK(sks).Serialize(),
			Lambda:      lambda,
			Value:       []byte("value"),
		})
		require.NoError(t, storage.SaveDecided(aggSignedMsg))
		if i == highestSeq {
			require.NoError(t, storage.SaveHighestDecidedInstance(aggSignedMsg))
		}
	}
	return &storage
}

func populatedIbft(
	nodeID uint64,
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
		NodeID:      nodeID,
		ValidatorPK: validatorPK(sks),
		ShareKey:    sks[nodeID],
		Committee:   nodes,
	}
	ret := New(
		zap.L(),
		storage,
		network.CopyWithLocalNodeID(peer.ID(fmt.Sprintf("%d", nodeID-1))),
		queue,
		params,
		share,
	)
	ret.(*ibftImpl).initFinished = true // as if they are already synced
	ret.(*ibftImpl).listenToNetworkMessages()
	ret.(*ibftImpl).listenToSyncMessages()
	return ret
}

func TestSyncFromScratch(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	i1 := populatedIbft(1, network, &s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks, 10), sks, nodes)

	i1.(*ibftImpl).SyncIBFT()
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}

func TestSyncFromMiddle(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := populatedStorage(t, sks, 4)
	i1 := populatedIbft(1, network, s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks, 10), sks, nodes)

	// test before sync
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.SeqNumber)

	i1.(*ibftImpl).SyncIBFT()

	// test after sync
	highest, err = i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}

func TestSyncFromScratch100Sequences(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	i1 := populatedIbft(1, network, &s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks, 100), sks, nodes)

	i1.(*ibftImpl).SyncIBFT()
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 100, highest.Message.SeqNumber)
}

func TestSyncFromScratch100SequencesWithDifferentPeers(t *testing.T) {
	t.Run("scenario 1", func(t *testing.T) {
		sks, nodes := GenerateNodes(4)
		network := local.NewLocalNetwork()

		s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
		i1 := populatedIbft(1, network, &s1, sks, nodes)

		_ = populatedIbft(2, network, populatedStorage(t, sks, 100), sks, nodes)
		_ = populatedIbft(3, network, populatedStorage(t, sks, 105), sks, nodes)
		_ = populatedIbft(4, network, populatedStorage(t, sks, 89), sks, nodes)

		// test before sync
		_, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
		require.EqualError(t, err, "EntryNotFoundError")

		i1.(*ibftImpl).SyncIBFT()

		// test after sync
		highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
		require.NoError(t, err)
		require.EqualValues(t, 105, highest.Message.SeqNumber)
	})

	t.Run("scenario 2", func(t *testing.T) {
		sks, nodes := GenerateNodes(4)
		network := local.NewLocalNetwork()

		s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
		i1 := populatedIbft(1, network, &s1, sks, nodes)

		_ = populatedIbft(2, network, populatedStorage(t, sks, 10), sks, nodes)
		_ = populatedIbft(3, network, populatedStorage(t, sks, 20), sks, nodes)
		_ = populatedIbft(4, network, populatedStorage(t, sks, 89), sks, nodes)

		// test before sync
		_, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
		require.EqualError(t, err, "EntryNotFoundError")

		i1.(*ibftImpl).SyncIBFT()

		// test after sync
		highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
		require.NoError(t, err)
		require.EqualValues(t, 89, highest.Message.SeqNumber)
	})

}
