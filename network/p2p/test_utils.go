package p2pv1

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

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
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/format"
)

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

	// hash, err := keys.OperatorKey.StorageHash()
	// if err != nil {
	// 	panic(err)
	// }

	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, err
	}

	nodeStorage, err := storage.NewNodeStorage(logger, db)
	if err != nil {
		return nil, err
	}

	dutyStore := dutystore.New()
	signatureVerifier := signatureverifier.NewSignatureVerifier(nodeStorage)

	cfg := NewNetConfig(keys, format.OperatorID(operatorPubkey), ln.Bootnode, ssv_testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), options.Nodes)
	cfg.Ctx = ctx
	cfg.Subnets = "00000000000000000000020000000000" //PAY ATTENTION for future test scenarios which use more than one eth-validator we need to make this field dynamically changing
	ns, err := storage.NewNodeStorage(logger, db)
	if err != nil {
		return nil, err
	}

	shares := generateShares(t, ks, ns, networkconfig.TestNetwork)

	cfg.NodeStorage = ns
	cfg.Metrics = nil
	// TODO: (Alan) decide if the code in this comment is needed, else remove
	// cfg.MessageValidator = nil //validation.New(
	//networkconfig.TestNetwork,
	//nodeStorage.ValidatorStore(),
	//dutyStore,
	//signatureVerifier,
	//validation.WithSelfAccept(selfPeerID, true),
	//)
	ctrl := gomock.NewController(t)
	newMockValidatorStore(ctrl, networkconfig.TestNetwork, ks, shares)
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
		cfg.MessageValidator = validation.New(
			networkconfig.TestNetwork,
			nodeStorage.ValidatorStore(),
			dutyStore,
			signatureVerifier,
			validation.WithSelfAccept(selfPeerID, true),
		)
	}

	if options.PeerScoreInspector != nil && options.PeerScoreInspectorInterval > 0 {
		cfg.PeerScoreInspector = func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
			options.PeerScoreInspector(selfPeerID, peerMap)
		}
		cfg.PeerScoreInspectorInterval = options.PeerScoreInspectorInterval
	}

	cfg.OperatorDataStore = operatordatastore.New(&registrystorage.OperatorData{ID: spectypes.OperatorID(nodeIndex + 1)})

	mr := metricsreporter.New()
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

func newMockValidatorStore(ctrl *gomock.Controller, netCfg networkconfig.NetworkConfig, ks *spectestingutils.TestKeySet, shares shareSet) {
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
