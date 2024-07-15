package p2pv1

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aquasecurity/table"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sourcegraph/conc/pool"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
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
	validators := make([]string, validatorCount)
	for i := 0; i < validatorCount; i++ {
		var validator [48]byte
		cryptorand.Read(validator[:])
		validators[i] = hex.EncodeToString(validator[:])
	}

	// Create a MessageValidator to accept/reject/ignore messages according to their role type.
	const (
		acceptedRole = spectypes.RoleProposer
		ignoredRole  = spectypes.RoleAggregator
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
			peer := vNet.NodeByPeerID(p)
			signedSSVMsg := &spectypes.SignedSSVMessage{}
			require.NoError(t, signedSSVMsg.Decode(pmsg.GetData()))

			decodedMsg, err := queue.DecodeSignedSSVMessage(signedSSVMsg)
			require.NoError(t, err)
			pmsg.ValidatorData = decodedMsg
			mtx.Lock()
			// Validation according to role.
			var validation pubsub.ValidationResult
			switch signedSSVMsg.SSVMessage.MsgID.GetRoleType() {
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
	vNet = CreateVirtualNet(t, ctx, 4, validators, func(nodeIndex int) validation.MessageValidator {
		return messageValidators[nodeIndex]
	})
	defer func() {
		require.NoError(t, vNet.Close())
	}()

	// Prepare a pool of broadcasters.
	mu := sync.Mutex{}
	height := atomic.Int64{}
	roleBroadcasts := map[spectypes.RunnerRole]int{}
	broadcasters := pool.New().WithErrors().WithContext(ctx)
	broadcaster := func(node *VirtualNode, roles ...spectypes.RunnerRole) {
		broadcasters.Go(func(ctx context.Context) error {
			for i := 0; i < 50; i++ {
				role := roles[i%len(roles)]

				mu.Lock()
				roleBroadcasts[role]++
				mu.Unlock()

				msgID, msg := dummyMsg(t, validators[rand.Intn(len(validators))], int(height.Add(1)), role)
				err := node.Broadcast(msgID, msg)
				if err != nil {
					return err
				}
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		})
	}

	// Broadcast the messages:
	// - node 0 broadcasts accepted messages.
	// - node 1 broadcasts ignored messages.
	// - node 2 broadcasts rejected messages.
	// - node 3 broadcasts all messages (equal distribution).
	broadcaster(vNet.Nodes[0], acceptedRole)
	broadcaster(vNet.Nodes[1], ignoredRole)
	broadcaster(vNet.Nodes[2], rejectedRole)
	broadcaster(vNet.Nodes[3], acceptedRole, ignoredRole, rejectedRole)

	// Wait for the broadcasters to finish.
	err := broadcasters.Wait()
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Assert that the messages were distributed as expected.
	deadline := time.Now().Add(5 * time.Second)
	interval := 100 * time.Millisecond
	for i := 0; i < nodeCount; i++ {
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
		if len(errors) == 0 {
			break
		}
		if time.Now().After(deadline) {
			require.Empty(t, errors)
		}
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
		peers := make([]peerScore, 0, node.PeerScores.Len())
		node.PeerScores.Range(func(index NodeIndex, snapshot *pubsub.PeerScoreSnapshot) bool {
			peers = append(peers, peerScore{index, snapshot.Score})
			return true
		})
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
				require.Fail(t, "invalid order", "node %d", node.Index)
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
	PeerScores *hashmap.Map[NodeIndex, *pubsub.PeerScoreSnapshot]
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
	validatorPubKeys []string,
	messageValidatorProvider func(int) validation.MessageValidator,
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
			}

			node.PeerScores.Range(func(index NodeIndex, snapshot *pubsub.PeerScoreSnapshot) bool {
				node.PeerScores.Del(index)
				return true
			})
			for peerID, peerScore := range peerMap {
				peerNode := vn.NodeByPeerID(peerID)
				if peerNode == nil {
					t.Fatalf("peer not found (%s)", peerID)
				}
				node.PeerScores.Set(peerNode.Index, peerScore)
			}

		},
		PeerScoreInspectorInterval: time.Millisecond * 5,
	}, validatorPubKeys...)

	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	for i, node := range ln.Nodes {
		vn.Nodes = append(vn.Nodes, &VirtualNode{
			Index:      NodeIndex(i),
			Network:    node.(*p2pNetwork),
			PeerScores: hashmap.New[NodeIndex, *pubsub.PeerScoreSnapshot](), //{}make(map[NodeIndex]*pubsub.PeerScoreSnapshot),
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
