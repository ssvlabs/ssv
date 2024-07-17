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
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/sourcegraph/conc/pool"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/message/validation"
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
	var mtx sync.Mutex
	// Create a MessageValidator to accept/reject/ignore messages according to their role type.
	const (
		acceptedRole = spectypes.RoleProposer
		ignoredRole  = spectypes.RoleAggregator
		rejectedRole = spectypes.RoleSyncCommitteeContribution
	)
	messageValidators := CreateMsgValidators(&mtx, nodeCount, vNet)
	// Create a VirtualNet with 4 nodes.
	ks := spectestingutils.Testing4SharesSet()
	vNet = CreateVirtualNet(t, ctx, 4, validators, func(nodeIndex int) validation.MessageValidator {
		return messageValidators[nodeIndex]
	}, ks)
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
