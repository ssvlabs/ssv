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
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sourcegraph/conc/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

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

	ctx, cancel := context.WithCancel(t.Context())
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
		messageValidators[i] = &MockMessageValidator{
			Accepted: make([]int, nodeCount),
			Ignored:  make([]int, nodeCount),
			Rejected: make([]int, nodeCount),
		}
		messageValidators[i].ValidateFunc = func(ctx context.Context, peerID peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
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

			p := vNet.NodeByPeerID(peerID)
			if p == nil {
				panic(fmt.Sprintf("unknown peer: %s", peerID))
			}

			mtx.Lock()

			// Validation according to role.
			var validation pubsub.ValidationResult
			switch ssvMessage.MsgID.GetRoleType() {
			case acceptedRole:
				messageValidators[i].Accepted[p.Index]++
				validation = pubsub.ValidationAccept
			case ignoredRole:
				messageValidators[i].Ignored[p.Index]++
				validation = pubsub.ValidationIgnore
			case rejectedRole:
				messageValidators[i].Rejected[p.Index]++
				validation = pubsub.ValidationReject
			default:
				panic("unsupported role")
			}
			mtx.Unlock()

			// Always accept messages from self to make libp2p propagate them,
			// while still counting them by their role.
			if peerID == vNet.Nodes[i].Network.Host().ID() {
				return pubsub.ValidationAccept
			}

			return validation
		}
	}

	// Create a VirtualNet with 4 nodes.
	vNet = createVirtualNet(t, ctx, 4, shares, func(nodeIndex uint64) validation.MessageValidator {
		return messageValidators[nodeIndex]
	})

	defer func() {
		require.NoError(t, vNet.Close())
	}()

	time.Sleep(1 * time.Second)

	// Prepare a pool of broadcasters.
	height := atomic.Int64{}
	broadcasters := pool.New().WithErrors().WithContext(ctx)
	broadcaster := func(node *VirtualNode, roles ...spectypes.RunnerRole) {
		broadcasters.Go(func(ctx context.Context) error {
			for i := 0; i < 12; i++ {
				role := roles[i%len(roles)]

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

	// Compute each broadcaster's fraction of rejected-role messages. A higher
	// rate should drive that broadcaster's score lower on every observer.
	rejectedRate := map[NodeIndex]float64{}
	for nodeIdx, roles := range messageTypesByNodeIndex {
		rejected := 0
		for _, r := range roles {
			if r == rejectedRole {
				rejected++
			}
		}
		rejectedRate[NodeIndex(nodeIdx)] = float64(rejected) / float64(len(roles))
	}

	type peerScore struct {
		index NodeIndex
		score float64
	}

	// Derive the single-role node indices from messageTypesByNodeIndex so
	// reordering the map can't silently desync the bucket invariant below.
	indexByRole := map[spectypes.RunnerRole]NodeIndex{}
	for nodeIdx, roles := range messageTypesByNodeIndex {
		if len(roles) == 1 {
			indexByRole[roles[0]] = NodeIndex(nodeIdx)
		}
	}
	require.Contains(t, indexByRole, acceptedRole)
	require.Contains(t, indexByRole, ignoredRole)
	require.Contains(t, indexByRole, rejectedRole)
	acceptedOnlyIdx := indexByRole[acceptedRole]
	ignoredOnlyIdx := indexByRole[ignoredRole]
	rejectedOnlyIdx := indexByRole[rejectedRole]

	snapshotForNode := func(node *VirtualNode) []peerScore {
		ptr := node.PeerScores.Load()
		if ptr == nil {
			return nil
		}
		peers := make([]peerScore, 0, len(*ptr))
		for index, s := range *ptr {
			peers = append(peers, peerScore{index, s.Score})
		}
		sort.Slice(peers, func(i, j int) bool { return peers[i].score > peers[j].score })
		return peers
	}

	// rateInvariantViolation: for any two peers where one has a strictly
	// lower rejected-rate than the other, the lower-rate peer's score
	// must be >= the higher-rate peer's. Equal rates carry no ordering
	// constraint; equal scores between different-rate peers are also fine
	// (they're tied, neither is "above"). Comparing scores directly is
	// what makes the latter true — the input peers slice is sorted by
	// score for the diagnostic table, but sort.Slice is not stable, so
	// slice position alone can't stand in for rank when scores tie.
	// Returns "" if the invariant holds.
	rateInvariantViolation := func(peers []peerScore, observer NodeIndex) string {
		for i, a := range peers {
			for j, b := range peers {
				if i == j {
					continue
				}
				rA := rejectedRate[a.index]
				rB := rejectedRate[b.index]
				if rA < rB && a.score < b.score {
					return fmt.Sprintf(
						"observer %d: peer %d (rejected-rate=%.2f, score=%.2f) scored below peer %d (rejected-rate=%.2f, score=%.2f) despite a lower rejected-rate",
						observer,
						a.index, rA, a.score,
						b.index, rB, b.score)
				}
			}
		}
		return ""
	}

	// bucketInvariantViolation: accepted-only ≥ ignored-only ≥ rejected-only.
	// This guards regressions the rate check can't catch — nodes 0 and 1 both
	// have rejected-rate=0, so a regression that stops rewarding accepts or
	// starts rewarding ignores would otherwise slip through. Returns "" if
	// the invariant holds.
	bucketInvariantViolation := func(peers []peerScore, observer NodeIndex) string {
		scoreByIdx := map[NodeIndex]float64{}
		for _, p := range peers {
			scoreByIdx[p.index] = p.score
		}
		check := func(higher, lower NodeIndex) string {
			if observer == higher || observer == lower {
				return ""
			}
			if scoreByIdx[higher] < scoreByIdx[lower] {
				return fmt.Sprintf(
					"observer %d: peer %d (score=%.2f) ranked below peer %d (score=%.2f)",
					observer, higher, scoreByIdx[higher], lower, scoreByIdx[lower])
			}
			return ""
		}
		if msg := check(acceptedOnlyIdx, ignoredOnlyIdx); msg != "" {
			return msg
		}
		return check(ignoredOnlyIdx, rejectedOnlyIdx)
	}

	// settled is the snapshot Eventually verified; falls back to the latest
	// snapshot if Eventually times out so the diagnostic asserts and the
	// table-printing cleanup still have something useful to show.
	var settled map[NodeIndex][]peerScore

	// Print a pretty table of each node's peers and their scores. Registered
	// before the wait so it fires on failure too.
	t.Cleanup(func() {
		snapshots := settled
		if snapshots == nil {
			snapshots = make(map[NodeIndex][]peerScore, len(vNet.Nodes))
			for _, node := range vNet.Nodes {
				snapshots[node.Index] = snapshotForNode(node)
			}
		}
		for _, node := range vNet.Nodes {
			peers := snapshots[node.Index]
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
		}
	})

	// Wait for the rate + bucket invariants to hold on every observer in one
	// consistent snapshot. Returns quickly on a fast machine; times out at
	// 15s if scores never settle.
	ok := assert.Eventually(t, func() bool {
		snapshot := make(map[NodeIndex][]peerScore, len(vNet.Nodes))
		for _, node := range vNet.Nodes {
			peers := snapshotForNode(node)
			// Require a fully-populated snapshot before evaluating the
			// invariants — bucketInvariantViolation reads scoreByIdx[idx]
			// which returns 0 for absent peers, so a partial snapshot can
			// satisfy the bucket check spuriously (a positive accepted-only
			// score "wins" against a missing rejected-only at score=0).
			if peers == nil || len(peers) != len(vNet.Nodes)-1 {
				return false
			}
			if rateInvariantViolation(peers, node.Index) != "" ||
				bucketInvariantViolation(peers, node.Index) != "" {
				return false
			}
			snapshot[node.Index] = peers
		}
		settled = snapshot
		return true
	}, 15*time.Second, 100*time.Millisecond, "peer scores never satisfied rate + bucket invariants")

	if !ok {
		// Eventually marked the test as failed but didn't FailNow (assert.*
		// not require.*). Grab the latest snapshot so the diagnostic asserts
		// below can surface which invariant on which observer failed.
		settled = make(map[NodeIndex][]peerScore, len(vNet.Nodes))
		for _, node := range vNet.Nodes {
			settled[node.Index] = snapshotForNode(node)
		}
	}

	// settled was either verified by Eventually (happy path — these are
	// redundant) or freshly grabbed (sad path — they surface specifics on
	// top of Eventually's generic message).
	for _, node := range vNet.Nodes {
		peers := settled[node.Index]
		require.NotNilf(t, peers, "node %d: PeerScoreInspector never ran", node.Index)
		require.Equalf(t, len(vNet.Nodes)-1, len(peers), "node %d", node.Index)
		if msg := rateInvariantViolation(peers, node.Index); msg != "" {
			require.Failf(t, "invalid rate ordering", "%s", msg)
		}
		if msg := bucketInvariantViolation(peers, node.Index); msg != "" {
			require.Failf(t, "invalid bucket ordering", "%s", msg)
		}
	}
}

type MockMessageValidator struct {
	Accepted []int
	Ignored  []int
	Rejected []int

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

func createVirtualNet(
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
