package p2pv1

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aquasecurity/table"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sourcegraph/conc/pool"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestP2pNetwork_MessageValidation tests p2pNetwork would score peers according
// to the validity of the messages they broadcast.
//
// This test creates 4 nodes, each fulfilling a different role by broadcasting
// messages that would be accepted, ignored or rejected by the other nodes,
// and finally asserts that each node scores it's peers according to their
// played role (accepted > ignored > rejected).
func TestP2pNetwork_MessageValidation(t *testing.T) {
	const (
		nodeCount      = 4
		validatorCount = 20
	)
	var vNet *VirtualNet

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create 20 fake validator public keys.
	shares := generateShares(t, validatorCount)

	// Create a MessageValidator to accept/reject/ignore messages according to their role type.
	const (
		acceptedRole = spectypes.RoleCommittee
		ignoredRole  = spectypes.RoleProposer
		rejectedRole = spectypes.RoleSyncCommitteeContribution
	)
	messageValidators := make([]*MockMessageValidator, nodeCount)
	var mtx sync.Mutex
	for i := 0; i < nodeCount; i++ {
		i := i
		messageValidators[i] = &MockMessageValidator{
			Accepted: make([]int, nodeCount),
			Ignored:  make([]int, nodeCount),
			Rejected: make([]int, nodeCount),
		}
		messageValidators[i].ValidateFunc = func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
			signedSSVMessage := &spectypes.SignedSSVMessage{}
			if err := signedSSVMessage.Decode(pmsg.GetData()); err != nil {
				return pubsub.ValidationReject
			}

			ssvMessage := signedSSVMessage.SSVMessage

			var body any

			switch ssvMessage.MsgType {
			case spectypes.SSVConsensusMsgType:
				var qbftMsg specqbft.Message
				if err := qbftMsg.Decode(ssvMessage.Data); err != nil {
					return pubsub.ValidationReject
				}

				body = qbftMsg

			case spectypes.SSVPartialSignatureMsgType:
				var psm spectypes.PartialSignatureMessages
				if err := psm.Decode(ssvMessage.Data); err != nil {
					return pubsub.ValidationReject
				}

				body = psm
			default:
				return pubsub.ValidationReject
			}

			pmsg.ValidatorData = &queue.SSVMessage{
				SignedSSVMessage: signedSSVMessage,
				SSVMessage:       ssvMessage,
				Body:             body,
			}

			peer := vNet.NodeByPeerID(p)

			mtx.Lock()
			// Validation according to role.
			var validation pubsub.ValidationResult
			switch ssvMessage.MsgID.GetRoleType() {
			case acceptedRole:
				messageValidators[i].Accepted[peer.Index]++
				messageValidators[i].TotalAccepted++
				validation = pubsub.ValidationAccept
			case ignoredRole:
				messageValidators[i].Ignored[peer.Index]++
				messageValidators[i].TotalIgnored++
				validation = pubsub.ValidationIgnore
			case rejectedRole:
				messageValidators[i].Rejected[peer.Index]++
				messageValidators[i].TotalRejected++
				validation = pubsub.ValidationReject
			default:
				panic("unsupported role")
			}
			mtx.Unlock()

			// Always accept messages from self to make libp2p propagate them,
			// while still counting them by their role.
			if p == vNet.Nodes[i].Network.Host().ID() {
				return pubsub.ValidationAccept
			}

			return validation
		}
	}

	// Create a VirtualNet with 4 nodes.
	vNet = CreateVirtualNet(t, ctx, 4, shares, func(nodeIndex uint64) validation.MessageValidator {
		return messageValidators[nodeIndex]
	})

	defer func() {
		require.NoError(t, vNet.Close())
	}()

	time.Sleep(1 * time.Second)

	// Prepare a pool of broadcasters.
	height := atomic.Int64{}
	roleBroadcasts := map[spectypes.RunnerRole]int{}
	broadcasters := pool.New().WithErrors().WithContext(ctx)
	broadcaster := func(node *VirtualNode, roles ...spectypes.RunnerRole) {
		broadcasters.Go(func(ctx context.Context) error {
			for i := 0; i < 12; i++ {
				role := roles[i%len(roles)]

				mtx.Lock()
				roleBroadcasts[role]++
				mtx.Unlock()

				msgID, msg := dummyMsg(t, hex.EncodeToString(shares[rand.Intn(len(shares))].ValidatorPubKey[:]), int(height.Add(1)), role)
				err := node.Broadcast(msgID, msg)
				if err != nil {
					return err
				}
				time.Sleep(100 * time.Millisecond)
			}
			return nil
		})
	}

	// Broadcast the messages:
	// - node 0 broadcasts accepted messages.
	// - node 1 broadcasts ignored messages.
	// - node 2 broadcasts rejected messages.
	// - node 3 broadcasts all messages (equal distribution).
	messageTypesByNodeIndex := map[int][]spectypes.RunnerRole{
		0: {acceptedRole},
		1: {ignoredRole},
		2: {rejectedRole},
		3: {acceptedRole, ignoredRole, rejectedRole},
	}

	for i := 0; i < nodeCount; i++ {
		broadcaster(vNet.Nodes[i], messageTypesByNodeIndex[i]...)
	}

	// Wait for the broadcasters to finish.
	err := broadcasters.Wait()
	require.NoError(t, err)

	// Assert that the messages were distributed as expected.
	time.Sleep(8 * time.Second)

	interval := 100 * time.Millisecond
	for i := 0; i < nodeCount; i++ {
		// Messages from nodes broadcasting rejected role become rejected once score threshold is reached
		if slices.Contains(messageTypesByNodeIndex[i], rejectedRole) {
			continue
		}

		// better lock inside loop than wait interval locked
		mtx.Lock()
		var errors []error
		if roleBroadcasts[acceptedRole] != messageValidators[i].TotalAccepted {
			errors = append(errors, fmt.Errorf("node %d accepted %d messages (expected %d)", i, messageValidators[i].TotalAccepted, roleBroadcasts[acceptedRole]))
		}
		if roleBroadcasts[ignoredRole] != messageValidators[i].TotalIgnored {
			errors = append(errors, fmt.Errorf("node %d ignored %d messages (expected %d)", i, messageValidators[i].TotalIgnored, roleBroadcasts[ignoredRole]))
		}
		if roleBroadcasts[rejectedRole] != messageValidators[i].TotalRejected {
			errors = append(errors, fmt.Errorf("node %d rejected %d messages (expected %d)", i, messageValidators[i].TotalRejected, roleBroadcasts[rejectedRole]))
		}
		mtx.Unlock()
		require.Empty(t, errors)
		time.Sleep(interval)
	}

	// Assert that each node scores it's peers according to the following order:
	// - node 0, (node 1 OR 3), (node 1 OR 3), node 2
	// (after excluding itself from this list)
	for _, node := range vNet.Nodes {
		node := node

		// Prepare the valid orders, excluding the node itself.
		validOrders := [][]NodeIndex{
			{0, 1, 3, 2},
			{0, 3, 1, 2},
		}
		for i, validOrder := range validOrders {
			for j, index := range validOrder {
				if index == node.Index {
					validOrders[i] = append(validOrders[i][:j], validOrders[i][j+1:]...)
					break
				}
			}
		}

		// Sort peers by their scores.
		type peerScore struct {
			index NodeIndex
			score float64
		}
		peers := make([]peerScore, 0)
		for index, snapshot := range *node.PeerScores.Load() {
			peers = append(peers, peerScore{index, snapshot.Score})
		}
		sort.Slice(peers, func(i, j int) bool {
			return peers[i].score > peers[j].score
		})

		// Print a pretty table of each node's peers and their scores.
		defer func() {
			tbl := table.New(os.Stdout)
			tbl.SetHeaders("Peer", "Score", "Accepted", "Ignored", "Rejected")
			mtx.Lock()
			for _, peer := range peers {
				tbl.AddRow(
					fmt.Sprintf("%d", peer.index),
					fmt.Sprintf("%.2f", peer.score),
					fmt.Sprintf("%d", messageValidators[node.Index].Accepted[peer.index]),
					fmt.Sprintf("%d", messageValidators[node.Index].Ignored[peer.index]),
					fmt.Sprintf("%d", messageValidators[node.Index].Rejected[peer.index]),
				)
			}
			mtx.Unlock()
			fmt.Println()
			fmt.Printf("Peer Scores (Node %d)\n", node.Index)
			tbl.Render()
		}()

		// Assert that the peers are in one of the valid orders.
		require.Equal(t, len(vNet.Nodes)-1, len(peers), "node %d", node.Index)
		for i, validOrder := range validOrders {
			valid := true
			for j, peer := range peers {
				if peer.index != validOrder[j] {
					valid = false
					break
				}
			}
			if valid {
				break
			}
			if i == len(validOrders)-1 {
				require.Fail(t, "invalid order", "node %d, peers %v", node.Index, peers)
			}
		}
	}

	defer fmt.Println()
}

type MockMessageValidator struct {
	Accepted      []int
	Ignored       []int
	Rejected      []int
	TotalAccepted int
	TotalIgnored  int
	TotalRejected int

	ValidateFunc func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

func (v *MockMessageValidator) ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return v.Validate
}

func (v *MockMessageValidator) Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return v.ValidateFunc(ctx, p, pmsg)
}

type NodeIndex int

type VirtualNode struct {
	Index      NodeIndex
	Network    *p2pNetwork
	PeerScores atomic.Pointer[map[NodeIndex]*pubsub.PeerScoreSnapshot]
}

func (n *VirtualNode) Broadcast(msgID spectypes.MessageID, msg *spectypes.SignedSSVMessage) error {
	return n.Network.Broadcast(msgID, msg)
}

// VirtualNet is a utility to create & interact with a virtual network of nodes.
type VirtualNet struct {
	Nodes []*VirtualNode
}

func CreateVirtualNet(
	t *testing.T,
	ctx context.Context,
	nodes int,
	shares []*ssvtypes.SSVShare,
	messageValidatorProvider func(uint64) validation.MessageValidator,
) *VirtualNet {
	var doneSetup atomic.Bool
	vn := &VirtualNet{}
	ln, routers, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:                    nodes,
		MinConnected:             nodes - 1,
		UseDiscv5:                false,
		TotalValidators:          1000,
		ActiveValidators:         800,
		MyValidators:             300,
		MessageValidatorProvider: messageValidatorProvider,
		PeerScoreInspector: func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
			if !doneSetup.Load() {
				return
			}
			node := vn.NodeByPeerID(selfPeer)
			if node == nil {
				t.Fatalf("self peer not found (%s)", selfPeer)
				return
			}

			peerScoresUpdated := make(map[NodeIndex]*pubsub.PeerScoreSnapshot, len(peerMap))
			for peerID, peerScore := range peerMap {
				peerNode := vn.NodeByPeerID(peerID)
				if peerNode == nil {
					t.Fatalf("peer not found (%s)", peerID)
					return
				}
				peerScoresUpdated[peerNode.Index] = peerScore
			}
			node.PeerScores.Store(&peerScoresUpdated)
		},
		PeerScoreInspectorInterval: time.Millisecond * 5,
		Shares:                     shares,
	})

	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	for i, node := range ln.Nodes {
		vn.Nodes = append(vn.Nodes, &VirtualNode{
			Index:   NodeIndex(i),
			Network: node.(*p2pNetwork),
		})
	}
	doneSetup.Store(true)

	return vn
}

func (vn *VirtualNet) NodeByPeerID(peerID peer.ID) *VirtualNode {
	for _, node := range vn.Nodes {
		if node.Network.Host().ID() == peerID {
			return node
		}
	}
	return nil
}

func (vn *VirtualNet) Close() error {
	for _, node := range vn.Nodes {
		err := node.Network.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func generateShares(t *testing.T, count int) []*ssvtypes.SSVShare {
	var shares []*ssvtypes.SSVShare

	for i := 0; i < count; i++ {
		validatorIndex := phase0.ValidatorIndex(i)
		domainShare := *spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), validatorIndex)

		var pk spectypes.ValidatorPK
		_, err := cryptorand.Read(pk[:])
		require.NoError(t, err)

		domainShare.ValidatorPubKey = pk

		share := &ssvtypes.SSVShare{
			Share:      domainShare,
			Status:     eth2apiv1.ValidatorStateActiveOngoing,
			Liquidated: false,
		}

		shares = append(shares, share)
	}

	return shares
}
