package p2pv1

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/monitoring/metricsreporter"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	p2pcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	ssv_testing "github.com/ssvlabs/ssv/network/testing"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/storage"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/format"
)

// TODO: (Alan) might have to rename this file back to test_utils.go if non-test files require it.

// LocalNet holds the nodes in the local network
type LocalNet struct {
	NodeKeys []ssv_testing.NodeKeys
	Bootnode *discovery.Bootnode
	Nodes    []network.P2PNetwork

	udpRand ssv_testing.UDPPortsRandomizer
}

// WithBootnode adds a bootnode to the network
func (ln *LocalNet) WithBootnode(ctx context.Context, logger *zap.Logger) error {
	bnSk, err := commons.GenNetworkKey()
	if err != nil {
		return err
	}
	isk, err := commons.ECDSAPrivToInterface(bnSk)
	if err != nil {
		return err
	}
	b, err := isk.Raw()
	if err != nil {
		return err
	}
	bn, err := discovery.NewBootnode(ctx, logger, &discovery.BootnodeOptions{
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       ln.udpRand.Next(13001, 13999),
	})
	if err != nil {
		return err
	}
	ln.Bootnode = bn
	return nil
}

// CreateAndStartLocalNet creates a new local network and starts it
// if any errors occurs during starting local network CreateAndStartLocalNet trying
// to create and start local net one more time until pCtx is not Done()
func CreateAndStartLocalNetFromKeySet(t *testing.T, pCtx context.Context, logger *zap.Logger, options LocalNetOptions, ks *spectestingutils.TestKeySet) (*LocalNet, error) {
	attempt := func(pCtx context.Context) (*LocalNet, error) {
		ln, err := NewLocalNetFromKeySet(t, pCtx, logger, options, ks)
		if err != nil {
			return nil, err
		}

		eg, ctx := errgroup.WithContext(pCtx)
		for i, node := range ln.Nodes {
			i, node := i, node //hack to avoid closures. price of using error groups

			eg.Go(func() error { //if replace EG to regular goroutines round don't change to second in test
				if err := node.Start(logger); err != nil {
					return fmt.Errorf("could not start node %d: %w", i, err)
				}
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				var peers []peer.ID
				for len(peers) < options.MinConnected && ctx.Err() == nil {
					peers = node.(HostProvider).Host().Network().Peers()
					time.Sleep(time.Millisecond * 100)
				}
				if ctx.Err() != nil {
					return fmt.Errorf("could not find enough peers for node %d, nodes quantity = %d, found = %d", i, options.Nodes, len(peers))
				}
				logger.Debug("found enough peers", zap.Int("for node", i), zap.Int("nodesQuantity", options.Nodes), zap.String("found", fmt.Sprintf("%+v", peers)))
				return nil
			})
		}

		return ln, eg.Wait()
	}

	for {
		select {
		case <-pCtx.Done():
			return nil, fmt.Errorf("context is done, network didn't start on time")
		default:
			ln, err := attempt(pCtx)
			if err != nil {
				for _, node := range ln.Nodes {
					_ = node.Close()
				}

				logger.Debug("trying to relaunch local network", zap.Error(err))
				continue
			}

			return ln, nil
		}
	}
}

// NewTestP2pNetwork creates a new network.P2PNetwork instance
func (ln *LocalNet) NewTestP2pNetworkFromKeySet(t *testing.T, ctx context.Context, nodeIndex int, keys ssv_testing.NodeKeys, logger *zap.Logger, options LocalNetOptions, ks *spectestingutils.TestKeySet) (network.P2PNetwork, error) {
	operatorPubkey, err := keys.OperatorKey.Public().Base64()
	if err != nil {
		return nil, err
	}
	cfg := NewNetConfig(keys, format.OperatorID(operatorPubkey), ln.Bootnode, ssv_testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), options.Nodes)
	cfg.Ctx = ctx
	cfg.Subnets = "00000000000000000000020000000000" //PAY ATTENTION for future test scenarios which use more than one eth-validator we need to make this field dynamically changing
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, err
	}
	ns, err := storage.NewNodeStorage(logger, db)
	if err != nil {
		return nil, err
	}

	shares := generateShares(t, ks, ns, networkconfig.TestNetwork)

	cfg.NodeStorage = ns
	cfg.Metrics = nil

	ctrl := gomock.NewController(t)
	cfg.MessageValidator = newMockMessageValidator(ctrl, networkconfig.TestNetwork, ks, shares)

	cfg.Network = networkconfig.TestNetwork
	if options.TotalValidators > 0 {
		cfg.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return uint64(options.TotalValidators), uint64(options.ActiveValidators), uint64(options.MyValidators), nil
		}
	}

	pubKey, err := p2pcommons.ECDSAPrivToInterface(keys.NetKey)
	if err != nil {
		panic(err)
	}
	selfPeerID, err := peer.IDFromPublicKey(pubKey.GetPublic())
	if err != nil {
		panic(err)
	}

	if options.MessageValidatorProvider != nil {
		cfg.MessageValidator = options.MessageValidatorProvider(nodeIndex)
	} else {
		cfg.MessageValidator = nil //validation.NewMessageValidator(networkconfig.TestNetwork, validation.WithSelfAccept(selfPeerID, true))
	}

	if options.PeerScoreInspector != nil && options.PeerScoreInspectorInterval > 0 {
		cfg.PeerScoreInspector = func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
			options.PeerScoreInspector(selfPeerID, peerMap)
		}
		cfg.PeerScoreInspectorInterval = options.PeerScoreInspectorInterval
	}

	cfg.OperatorDataStore = operatordatastore.New(&registrystorage.OperatorData{ID: spectypes.OperatorID(nodeIndex + 1)})

	mr := metricsreporter.New()
	fmt.Printf("Domain Type %v", 	cfg.Network.DomainType())
	p := New(logger, cfg, mr)
	err = p.Setup(logger)
	if err != nil {
		return nil, err
	}
	return p, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(int) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	UseDiscv5                                       bool
	TotalValidators, ActiveValidators, MyValidators int
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
}

// NewLocalNet creates a new mdns network
func NewLocalNetFromKeySet(t *testing.T, ctx context.Context, logger *zap.Logger, options LocalNetOptions, ks *spectestingutils.TestKeySet) (*LocalNet, error) {
	ln := &LocalNet{}
	ln.udpRand = make(ssv_testing.UDPPortsRandomizer)
	if options.UseDiscv5 {
		if err := ln.WithBootnode(ctx, logger); err != nil {
			return nil, err
		}
	}
	nodes, keys, err := ssv_testing.NewLocalTestnetFromKeySet(ctx, func(pctx context.Context, nodeIndex int, keys ssv_testing.NodeKeys) network.P2PNetwork {
		logger := logger.Named(fmt.Sprintf("node-%d", nodeIndex))
		p, err := ln.NewTestP2pNetworkFromKeySet(t, pctx, nodeIndex, keys, logger, options, ks)
		if err != nil {
			logger.Error("could not setup network", zap.Error(err))
		}
		return p
	}, ks)
	if err != nil {
		return nil, err
	}
	ln.NodeKeys = keys
	ln.Nodes = nodes

	return ln, nil
}

// NewNetConfig creates a new config for tests
func NewNetConfig(keys ssv_testing.NodeKeys, operatorPubKeyHash string, bn *discovery.Bootnode, tcpPort, udpPort, maxPeers int) *Config {
	bns := ""
	discT := "discv5"
	if bn != nil {
		bns = bn.ENR
	} else {
		discT = "mdns"
	}
	ua := ""
	return &Config{
		Bootnodes:          bns,
		TCPPort:            tcpPort,
		UDPPort:            udpPort,
		HostAddress:        "",
		HostDNS:            "",
		RequestTimeout:     10 * time.Second,
		MaxBatchResponse:   25,
		MaxPeers:           maxPeers,
		PubSubTrace:        false,
		PubSubScoring:      true,
		NetworkPrivateKey:  keys.NetKey,
		OperatorSigner:     keys.OperatorKey,
		OperatorPubKeyHash: operatorPubKeyHash,
		UserAgent:          ua,
		Discovery:          discT,
	}
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
	ks *spectestingutils.TestKeySet,
) *VirtualNet {
	var doneSetup atomic.Bool
	vn := &VirtualNet{}
	ln, routers, err := CreateNetworkAndSubscribeFromKeySet(t, ctx, LocalNetOptions{
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
	}, ks, validatorPubKeys...)

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

func registerHandler(logger *zap.Logger, node network.P2PNetwork, mid spectypes.MessageID, height specqbft.Height, round specqbft.Round, counter *int64, errors chan<- error) {
	node.RegisterHandlers(logger, &p2pprotocol.SyncHandler{
		Protocol: p2pprotocol.LastDecidedProtocol,
		Handler: func(message *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
			atomic.AddInt64(counter, 1)
			qbftMessage := specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     height,
				Round:      round,
				Identifier: mid[:],
				Root:       [32]byte{1, 2, 3},
			}
			data, err := qbftMessage.Encode()
			if err != nil {
				errors <- err
				return nil, err
			}
			return &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   mid,
				Data:    data,
			}, nil
		},
	})
}

func CreateNetworkAndSubscribeFromKeySet(t *testing.T, ctx context.Context, options LocalNetOptions, ks *spectestingutils.TestKeySet, pks ...string) (*LocalNet, []*dummyRouter, error) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	ln, err := CreateAndStartLocalNetFromKeySet(t, ctx, logger.Named("createNetworkAndSubscribe"), options, ks)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != options.Nodes {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), options.Nodes)
	}

	logger.Debug("created local network")

	routers := make([]*dummyRouter, options.Nodes)
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{
			i: i,
		}
		node.UseMessageRouter(routers[i])
	}

	logger.Debug("subscribing to topics")

	var wg sync.WaitGroup
	for _, pk := range pks {
		vpkBytes, err := hex.DecodeString(pk)
		vpk := spectypes.ValidatorPK(vpkBytes)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			wg.Add(1)
			go func(node network.P2PNetwork, vpk spectypes.ValidatorPK) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, vpk)
		}
	}
	wg.Wait()
	// let the nodes subscribe
	<-time.After(time.Second)
	for _, pk := range pks {
		vpkBytes, err := hex.DecodeString(pk)
		vpk := spectypes.ValidatorPK(vpkBytes)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			peers := make([]peer.ID, 0)
			for len(peers) < 2 {
				peers, err = node.Peers(vpk)
				if err != nil {
					return nil, nil, err
				}
				time.Sleep(time.Millisecond * 100)
			}
		}
	}

	return ln, routers, nil
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ *queue.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

func dummyMsg(t *testing.T, pkHex string, height int, role spectypes.RunnerRole) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)

	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), pk, role)
	qbftMsg := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      2,
		Identifier: id[:],
		Height:     specqbft.Height(height),
		Root:       [32]byte{0x1, 0x2, 0x3},
	}
	data, err := qbftMsg.Encode()
	require.NoError(t, err)

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    data,
	}

	signedSSVMsg, err := spectypes.SSVMessageToSignedSSVMessage(ssvMsg, 1, dummySignSSVMessage)
	require.NoError(t, err)

	return id, signedSSVMsg
}

func dummyMsgCommittee(t *testing.T, pkHex string, height int) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	return dummyMsg(t, pkHex, height, spectypes.RoleCommittee)
}

func dummySignSSVMessage(msg *spectypes.SSVMessage) ([]byte, error) {
	return bytes.Repeat([]byte{}, 256), nil
}

func (n *p2pNetwork) LastDecided(logger *zap.Logger, mid spectypes.MessageID) ([]p2pprotocol.SyncResult, error) {
	const (
		minPeers = 3
		waitTime = time.Second * 24
	)
	if !n.isReady() {
		return nil, p2pprotocol.ErrNetworkIsNotReady
	}
	pid, maxPeers := commons.ProtocolID(p2pprotocol.LastDecidedProtocol)
	peers, err := waitSubsetOfPeers(logger, n.getSubsetOfPeers, mid.GetDutyExecutorID(), minPeers, maxPeers, waitTime, allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(logger, peers, mid, pid, &message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
	})
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

func CreateMsgValidators(mtx *sync.Mutex, nodeCount int, vNet *VirtualNet) []*MockMessageValidator {
	// Create a MessageValidator to accept/reject/ignore messages according to their role type.
	const (
		acceptedRole = spectypes.RoleProposer
		ignoredRole  = spectypes.RoleAggregator
		rejectedRole = spectypes.RoleSyncCommitteeContribution
	)
	messageValidators := make([]*MockMessageValidator, nodeCount)

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
			if err := signedSSVMsg.Decode(pmsg.GetData()); err != nil {
				return 1
			}

			decodedMsg, err := queue.DecodeSignedSSVMessage(signedSSVMsg)
			if err != nil {
				return 1
			}
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
	return messageValidators
}

type shareSet struct {
	active                      *ssvtypes.SSVShare
	liquidated                  *ssvtypes.SSVShare
	inactive                    *ssvtypes.SSVShare
	nonUpdatedMetadata          *ssvtypes.SSVShare
	nonUpdatedMetadataNextEpoch *ssvtypes.SSVShare
	noMetadata                  *ssvtypes.SSVShare
}

func generateShares(t *testing.T, ks *spectestingutils.TestKeySet, ns storage.Storage, netCfg networkconfig.NetworkConfig) shareSet {
	activeShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex).ValidatorIndex,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, activeShare))

	liquidatedShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex).ValidatorIndex,
			},
			Liquidated: true,
		},
	}

	liquidatedSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(liquidatedShare.ValidatorPubKey[:], liquidatedSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, liquidatedShare))

	inactiveShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateUnknown,
			},
			Liquidated: false,
		},
	}

	inactiveSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(inactiveShare.ValidatorPubKey[:], inactiveSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, inactiveShare))

	slot := netCfg.Beacon.EstimatedCurrentSlot()
	epoch := netCfg.Beacon.EstimatedEpochAtSlot(slot)

	nonUpdatedMetadataShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: epoch,
			},
			Liquidated: false,
		},
	}

	nonUpdatedMetadataSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataShare.ValidatorPubKey[:], nonUpdatedMetadataSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataShare))

	nonUpdatedMetadataNextEpochShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: epoch + 1,
			},
			Liquidated: false,
		},
	}

	nonUpdatedMetadataNextEpochSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(nonUpdatedMetadataNextEpochShare.ValidatorPubKey[:], nonUpdatedMetadataNextEpochSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, nonUpdatedMetadataNextEpochShare))

	noMetadataShare := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: nil,
			Liquidated:     false,
		},
	}

	noMetadataShareSK, err := eth2types.GenerateBLSPrivateKey()
	require.NoError(t, err)

	copy(noMetadataShare.ValidatorPubKey[:], noMetadataShareSK.PublicKey().Marshal())
	require.NoError(t, ns.Shares().Save(nil, noMetadataShare))

	return shareSet{
		active:                      activeShare,
		liquidated:                  liquidatedShare,
		inactive:                    inactiveShare,
		nonUpdatedMetadata:          nonUpdatedMetadataShare,
		nonUpdatedMetadataNextEpoch: nonUpdatedMetadataNextEpochShare,
		noMetadata:                  noMetadataShare,
	}
}

func newMockMessageValidator(ctrl *gomock.Controller, netCfg networkconfig.NetworkConfig, ks *spectestingutils.TestKeySet, shares shareSet) validation.MessageValidator {
	dutyStore := dutystore.New()
	validatorStore := mocks.NewMockValidatorStore(ctrl)
	committee := maps.Keys(ks.Shares)
	slices.Sort(committee)

	committeeID := shares.active.CommitteeID()

	validatorStore.EXPECT().Committee(gomock.Any()).DoAndReturn(func(id spectypes.CommitteeID) *registrystorage.Committee {
		if id == committeeID {
			beaconMetadata1 := *shares.active.BeaconMetadata
			beaconMetadata2 := beaconMetadata1
			beaconMetadata2.Index = beaconMetadata1.Index + 1
			beaconMetadata3 := beaconMetadata2
			beaconMetadata3.Index = beaconMetadata2.Index + 1

			share1 := *shares.active
			share1.BeaconMetadata = &beaconMetadata1
			share2 := share1
			share2.ValidatorIndex = share1.ValidatorIndex + 1
			share2.BeaconMetadata = &beaconMetadata2
			share3 := share2
			share3.ValidatorIndex = share2.ValidatorIndex + 1
			share3.BeaconMetadata = &beaconMetadata3
			return &registrystorage.Committee{
				ID:        id,
				Operators: committee,
				Validators: []*ssvtypes.SSVShare{
					&share1,
					&share2,
					&share3,
				},
			}
		}

		return nil
	}).AnyTimes()

	validatorStore.EXPECT().Validator(gomock.Any()).DoAndReturn(func(pubKey []byte) *ssvtypes.SSVShare {
		for _, share := range []*ssvtypes.SSVShare{
			shares.active,
			shares.liquidated,
			shares.inactive,
			shares.nonUpdatedMetadata,
			shares.nonUpdatedMetadataNextEpoch,
			shares.noMetadata,
		} {
			if bytes.Equal(share.ValidatorPubKey[:], pubKey) {
				return share
			}
		}
		return nil
	}).AnyTimes()
	signatureVerifier := signatureverifier.NewMockSignatureVerifier(ctrl)
	return validation.New(netCfg, validatorStore, dutyStore, signatureVerifier)
}
