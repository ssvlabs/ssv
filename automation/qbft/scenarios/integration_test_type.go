package scenarios

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator/validator"
	protocolforks "github.com/bloxapp/ssv/protocol/forks"
	protocolbeacon "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/sync/handlers"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// IntegrationTest defines an integration test.
type IntegrationTest struct {
	Name              string
	Logger            *zap.Logger
	InitialInstances  map[spectypes.OperatorID][]*protocolstorage.StoredInstance
	Duties            map[spectypes.OperatorID][]*spectypes.Duty
	ExpectedInstances map[spectypes.OperatorID][]*protocolstorage.StoredInstance
	ExpectedErrors    map[spectypes.OperatorID]error // TODO: slice of errors?
	OutputMessages    map[spectypes.OperatorID]*specqbft.SignedMessage
}

type scenarioContext struct {
	ctx         context.Context
	localNet    *p2pv1.LocalNet
	stores      map[spectypes.OperatorID]*qbftstorage.QBFTStores
	keyManagers map[spectypes.OperatorID]spectypes.KeyManager
	dbs         map[spectypes.OperatorID]basedb.IDb
}

func (it *IntegrationTest) bootstrap(ctx context.Context) (*scenarioContext, error) {
	loggerFactory := func(s string) *zap.Logger {
		return logex.Build("simulation", zapcore.DebugLevel, nil).With(zap.String("who", s))
	}
	logger := loggerFactory(fmt.Sprintf("Bootstrap/%s", it.Name))
	logger.Info("creating resources")

	totalNodes := 0
	for _, instances := range it.InitialInstances {
		totalNodes += len(instances)
	}

	dbs := make(map[spectypes.OperatorID]basedb.IDb)
	for i := 0; i < totalNodes; i++ {
		db, err := storage.GetStorageFactory(basedb.Options{
			Type:   "badger-memory",
			Path:   "",
			Logger: zap.L(),
		})
		if err != nil {
			logger.Panic("could not setup storage", zap.Error(err))
		}

		dbs[spectypes.OperatorID(i+1)] = db
	}
	forkVersion := protocolforks.GenesisForkVersion

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, forkVersion, totalNodes, totalNodes/2, false)
	if err != nil {
		return nil, err
	}

	stores := make(map[spectypes.OperatorID]*qbftstorage.QBFTStores)
	kms := make(map[spectypes.OperatorID]spectypes.KeyManager)
	for i, node := range ln.Nodes {
		store := qbftstorage.New(dbs[spectypes.OperatorID(i)+1], loggerFactory(fmt.Sprintf("qbft-store-%d", i+1)), "attestations", forkVersion)

		storageMap := qbftstorage.NewStores()
		storageMap.Add(spectypes.BNRoleAttester, store)
		storageMap.Add(spectypes.BNRoleProposer, store)
		storageMap.Add(spectypes.BNRoleAggregator, store)
		storageMap.Add(spectypes.BNRoleSyncCommittee, store)
		storageMap.Add(spectypes.BNRoleSyncCommitteeContribution, store)

		stores[spectypes.OperatorID(i)+1] = storageMap
		kms[spectypes.OperatorID(i)+1] = spectestingutils.NewTestingKeyManager()

		node.RegisterHandlers(protocolp2p.WithHandler(
			protocolp2p.LastDecidedProtocol,
			handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("decided-handler-%d", i+1)), storageMap, node),
		), protocolp2p.WithHandler(
			protocolp2p.DecidedHistoryProtocol,
			handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-handler-%d", i+1)), storageMap, node, 25),
		))
	}

	sCtx := &scenarioContext{
		ctx:         ctx,
		localNet:    ln,
		stores:      stores,
		keyManagers: kms,
		dbs:         dbs,
	}
	return sCtx, nil
}

func (it *IntegrationTest) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sCtx, err := it.bootstrap(ctx)
	if err != nil {
		return err
	}

	validators, err := it.createValidators(sCtx.ctx, it.Logger, sCtx.localNet, sCtx.keyManagers, sCtx.stores)
	if err != nil {
		return fmt.Errorf("could not create share: %w", err)
	}

	for operatorID, instances := range it.InitialInstances {
		for _, instance := range instances {
			mid := spectypes.MessageIDFromBytes(instance.State.ID)
			if err := sCtx.stores[operatorID].Get(mid.GetRoleType()).SaveInstance(instance); err != nil {
				return err
			}
		}
	}

	for i, v := range validators {
		sCtx.localNet.Nodes[i].UseMessageRouter(newMsgRouter(v))
	}

	var wg sync.WaitGroup
	var startErr error
	for _, val := range validators {
		wg.Add(1)
		go func(val *protocolvalidator.Validator) {
			defer wg.Done()
			if err := val.Start(); err != nil {
				startErr = fmt.Errorf("could not start validator: %w", err)
			}
			<-time.After(time.Second * 3)
		}(val)
	}
	wg.Wait()

	if startErr != nil {
		return startErr
	}

	for _, v := range validators {
		for _, duties := range it.Duties[v.Share.OperatorID] {
			if err := v.StartDuty(duties); err != nil {
				return err
			}
		}
	}

	for operatorID, expectedOperatorInstances := range it.ExpectedInstances {
		for _, expectedInstance := range expectedOperatorInstances {
			mid := spectypes.MessageIDFromBytes(expectedInstance.State.ID)
			// TODO: check that we don't have more instances than we should have
			expectedHeight := expectedInstance.State.Height
			instancesInStore, err := sCtx.stores[operatorID].Get(mid.GetRoleType()).
				GetInstancesInRange(expectedInstance.State.ID, expectedHeight, expectedHeight)
			if err != nil {
				return err
			}

			if len(instancesInStore) == 0 {
				return fmt.Errorf("no instance found")
			}

			storeRoot, err := instancesInStore[0].State.GetRoot()
			if err != nil {
				return err
			}

			expectedRoot, err := expectedInstance.State.GetRoot()
			if err != nil {
				return err
			}

			if !bytes.Equal(storeRoot, expectedRoot) {
				return fmt.Errorf("roots are not equal")
			}
		}
	}

	return nil
}

func (it *IntegrationTest) createValidators(
	ctx context.Context,
	logger *zap.Logger,
	net *p2pv1.LocalNet,
	kms map[spectypes.OperatorID]spectypes.KeyManager,
	stores map[spectypes.OperatorID]*qbftstorage.QBFTStores,
) (
	[]*protocolvalidator.Validator,
	error,
) {
	validators := make([]*protocolvalidator.Validator, 0)
	operators := make([][]byte, 0)
	for _, k := range net.NodeKeys {
		pub, err := rsaencryption.ExtractPublicKey(k.OperatorKey)
		if err != nil {
			return nil, err
		}
		operators = append(operators, []byte(pub))
	}

	// add to key-managers and subscribe to topic
	for i, km := range kms {
		err := km.AddShare(spectestingutils.Testing4SharesSet().Shares[i])
		if err != nil {
			return nil, err
		}

		options := protocolvalidator.Options{
			Storage: stores[i],
			Network: net.Nodes[i-1], // TODO: fix
			SSVShare: &types.SSVShare{
				Share: *it.InitialInstances[i][0].State.Share,
				Metadata: types.Metadata{
					BeaconMetadata: &protocolbeacon.ValidatorMetadata{
						Index: spec.ValidatorIndex(i - 1),
					},
					OwnerAddress: "0x0",
					Operators:    operators,
					Liquidated:   false,
				},
			},
			Beacon: spectestingutils.NewTestingBeaconNode(),
			Signer: km,
		}

		l := logger.With(zap.String("w", fmt.Sprintf("node-%d", i)))
		val := protocolvalidator.NewValidator(ctx, options)
		val.DutyRunners = validator.SetupRunners(ctx, l, options)
		validators = append(validators, val)
	}
	return validators, nil
}

var testingShare = func(keysSet *spectestingutils.TestKeySet, id spectypes.OperatorID) *spectypes.Share {
	return &spectypes.Share{
		OperatorID:      id,
		ValidatorPubKey: keysSet.ValidatorPK.Serialize(),
		SharePubKey:     keysSet.Shares[id].GetPublicKey().Serialize(),
		DomainType:      spectypes.PrimusTestnet,
		Quorum:          keysSet.Threshold,
		PartialQuorum:   keysSet.PartialThreshold,
		Committee:       keysSet.Committee(),
	}
}

// TODO: consider returning map
func createDuties(pk []byte, slot spec.Slot, idx spec.ValidatorIndex, roles ...spectypes.BeaconRole) []*spectypes.Duty {
	var pkBytes [48]byte
	copy(pkBytes[:], pk)

	duties := make([]*spectypes.Duty, 0, len(roles))
	for _, role := range roles {
		var testingDuty *spectypes.Duty
		switch role {
		case spectypes.BNRoleAttester:
			testingDuty = spectestingutils.TestingAttesterDuty
		case spectypes.BNRoleAggregator:
			testingDuty = spectestingutils.TestingAggregatorDuty
		case spectypes.BNRoleProposer:
			testingDuty = spectestingutils.TestingProposerDuty
		case spectypes.BNRoleSyncCommittee:
			testingDuty = spectestingutils.TestingSyncCommitteeDuty
		case spectypes.BNRoleSyncCommitteeContribution:
			testingDuty = spectestingutils.TestingSyncCommitteeContributionDuty
		}

		duties = append(duties, &spectypes.Duty{
			Type:                    role,
			PubKey:                  pkBytes,
			Slot:                    slot,
			ValidatorIndex:          idx,
			CommitteeIndex:          testingDuty.CommitteeIndex,
			CommitteesAtSlot:        testingDuty.CommitteesAtSlot,
			CommitteeLength:         testingDuty.CommitteeLength,
			ValidatorCommitteeIndex: testingDuty.ValidatorCommitteeIndex,
		})
	}

	return duties
}

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(message spectypes.SSVMessage) {
	m.validator.HandleMessage(&message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}
