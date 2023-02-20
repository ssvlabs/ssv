package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/sync/handlers"
	"github.com/bloxapp/ssv/storage"
	"github.com/pkg/errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/network/testing"
	"github.com/bloxapp/ssv/operator/validator"
	protocolforks "github.com/bloxapp/ssv/protocol/forks"
	protocolbeacon "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// IntegrationTest defines an integration test.
type IntegrationTest struct {
	Name               string
	OperatorIDs        []spectypes.OperatorID
	Identifier         spectypes.MessageID
	ValidatorDelays    map[spectypes.OperatorID]time.Duration
	InitialInstances   map[spectypes.OperatorID][]*protocolstorage.StoredInstance
	Duties             map[spectypes.OperatorID][]scheduledDuty
	InstanceValidators map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error
	StartDutyErrors    map[spectypes.OperatorID]error
}

type scheduledDuty struct {
	Duty  *spectypes.Duty
	Delay time.Duration
}

type scenarioContext struct {
	ctx         context.Context
	ln          *p2pv1.LocalNet
	operatorIDs []spectypes.OperatorID
	nodes       map[spectypes.OperatorID]network.P2PNetwork      // 1 per operator, pass same to each instance
	nodeKeys    map[spectypes.OperatorID]testing.NodeKeys        // 1 per operator, pass same to each instance
	stores      map[spectypes.OperatorID]*qbftstorage.QBFTStores // 1 store per operator, pass same store to each instance
	keyManagers map[spectypes.OperatorID]spectypes.KeyManager    // 1 per operator, pass same to each instance
	dbs         map[spectypes.OperatorID]basedb.IDb              // 1 per operator, pass same to each instance
}

func (sctx *scenarioContext) Close() error {
	for _, n := range sctx.nodes {
		_ = n.Close()
	}
	for _, d := range sctx.dbs {
		d.Close()
	}
	return nil
}

func (it *IntegrationTest) Run(logger *zap.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sharesSet := getShareSetFromCommittee(len(it.OperatorIDs))

	sCtx, err := Bootstrap(ctx, it.OperatorIDs)
	if err != nil {
		return err
	}
	defer func() {
		_ = sCtx.Close()
		<-time.After(time.Second)
	}()

	validators, err := it.createValidators(logger, sCtx, sharesSet)
	if err != nil {
		return fmt.Errorf("could not create share: %w", err)
	}

	for _, operatorID := range it.OperatorIDs {
		sCtx.nodes[operatorID].UseMessageRouter(newMsgRouter(validators[operatorID]))
	}

	for operatorID, instances := range it.InitialInstances {
		for _, instance := range instances {
			mid := spectypes.MessageIDFromBytes(instance.State.ID)
			if err := sCtx.stores[operatorID].Get(mid.GetRoleType()).SaveInstance(instance); err != nil {
				return err
			}
		}
	}

	var startErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, val := range validators {
		// TODO: add logging for every node
		wg.Add(1)
		go func(val *protocolvalidator.Validator) {
			defer wg.Done()

			<-time.After(it.ValidatorDelays[val.Share.OperatorID])

			if err := val.Start(logger); err != nil {
				mu.Lock()
				startErr = fmt.Errorf("could not start validator: %w", err)
				mu.Unlock()
			}
			<-time.After(time.Second * 3)
		}(val)
	}

	wg.Wait()

	if startErr != nil {
		return startErr
	}

	var lastDutyTime time.Duration

	actualErrMap := sync.Map{}
	for _, val := range validators {
		for _, scheduledDuty := range it.Duties[val.Share.OperatorID] {
			val, scheduledDuty := val, scheduledDuty

			if lastDutyTime < scheduledDuty.Delay {
				lastDutyTime = scheduledDuty.Delay
			}

			logger.Debug("about to start duty", zap.Duration("delay", scheduledDuty.Delay))

			time.AfterFunc(scheduledDuty.Delay, func() {
				logger.Info("starting duty")
				startDutyErr := val.StartDuty(scheduledDuty.Duty)
				actualErrMap.Store(val.Share.OperatorID, startDutyErr)
			})
		}
	}

	<-time.After(lastDutyTime + (4 * time.Second))

	for operatorID, expectedErr := range it.StartDutyErrors {
		if actualErr, ok := actualErrMap.Load(operatorID); !ok {
			if expectedErr != nil {
				return fmt.Errorf("expected an error")
			}
		} else {
			aerr, ok := actualErr.(error)
			if !ok && expectedErr != nil {
				return fmt.Errorf("expected an error")
			}
			if !errors.Is(aerr, expectedErr) {
				return fmt.Errorf("got error different from expected (expected %v): %w", expectedErr, actualErr.(error))
			}
		}
	}

	if it.InstanceValidators != nil {
		if bytes.Equal(it.Identifier[:], bytes.Repeat([]byte{0}, len(it.Identifier))) {
			return fmt.Errorf("indentifier is not set")
		}
		errMap := make(map[spectypes.OperatorID]error)
		for operatorID, instanceValidators := range it.InstanceValidators {
			for i, instanceValidator := range instanceValidators {
				mid := spectypes.MessageIDFromBytes(it.Identifier[:])
				storedInstance, err := sCtx.stores[operatorID].Get(mid.GetRoleType()).
					GetHighestInstance(it.Identifier[:])
				if err != nil {
					if _, ok := errMap[operatorID]; !ok {
						errMap[operatorID] = err
					}
					continue
				}
				if storedInstance == nil {
					if _, ok := errMap[operatorID]; !ok {
						errMap[operatorID] = fmt.Errorf("stored instance is nil, operator ID %v, instance index %v", operatorID, i)
					}
					continue
				}

				jsonInstance, err := json.Marshal(storedInstance)
				if err != nil {
					if _, ok := errMap[operatorID]; !ok {
						errMap[operatorID] = fmt.Errorf("encode stored instance: %w", err)
					}
					continue
				}
				log.Printf("stored instance %d: %v\n", operatorID, string(jsonInstance))

				if err := instanceValidator(storedInstance); err != nil {
					if _, ok := errMap[operatorID]; !ok {
						errMap[operatorID] = fmt.Errorf("validate instance %d of operator ID %d: %w", i, operatorID, err)
					}
					continue
				}
			}
		}
		if len(errMap) > (len(it.OperatorIDs) / 3) { // (len(it.OperatorIDs) / 3) equals F equals (3F + 1) - (2F + 1) equals committeeNum - quorum
			return fmt.Errorf("errors validating instances: %+v", errMap)
		}
	}
	return nil
}

func (it *IntegrationTest) createValidators(logger *zap.Logger, sCtx *scenarioContext, sharesSet *spectestingutils.TestKeySet) (map[spectypes.OperatorID]*protocolvalidator.Validator, error) {
	validators := make(map[spectypes.OperatorID]*protocolvalidator.Validator)
	operators := make([][]byte, 0)
	for _, k := range sCtx.nodeKeys {
		pub, err := rsaencryption.ExtractPublicKey(k.OperatorKey)
		if err != nil {
			return nil, err
		}
		operators = append(operators, []byte(pub))
	}

	for _, operatorID := range it.OperatorIDs {
		err := sCtx.keyManagers[operatorID].AddShare(sharesSet.Shares[operatorID])
		if err != nil {
			return nil, err
		}

		options := protocolvalidator.Options{
			Storage: sCtx.stores[operatorID],
			Network: sCtx.nodes[operatorID],
			SSVShare: &types.SSVShare{
				Share: *testingShare(sharesSet, operatorID),
				Metadata: types.Metadata{
					BeaconMetadata: &protocolbeacon.ValidatorMetadata{
						Index: phase0.ValidatorIndex(1),
					},
					OwnerAddress: "0x0",
					Operators:    operators,
					Liquidated:   false,
				},
			},
			Beacon: beaconNode{spectestingutils.NewTestingBeaconNode()},
			Signer: sCtx.keyManagers[operatorID],
		}

		l := logger.With(zap.String("w", fmt.Sprintf("node-%d", operatorID)))

		ctx, cancel := context.WithCancel(sCtx.ctx)
		options.DutyRunners = validator.SetupRunners(ctx, l, options)
		val := protocolvalidator.NewValidator(ctx, cancel, options)
		validators[operatorID] = val
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

func createDuty(pk []byte, slot phase0.Slot, idx phase0.ValidatorIndex, role spectypes.BeaconRole) *spectypes.Duty {
	var pkBytes [48]byte
	copy(pkBytes[:], pk)

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

	return &spectypes.Duty{
		Type:                          role,
		PubKey:                        pkBytes,
		Slot:                          slot,
		ValidatorIndex:                idx,
		CommitteeIndex:                testingDuty.CommitteeIndex,
		CommitteesAtSlot:              testingDuty.CommitteesAtSlot,
		CommitteeLength:               testingDuty.CommitteeLength,
		ValidatorCommitteeIndex:       testingDuty.ValidatorCommitteeIndex,
		ValidatorSyncCommitteeIndices: testingDuty.ValidatorSyncCommitteeIndices,
	}
}

func createScheduledDuty(pk []byte, slot phase0.Slot, idx phase0.ValidatorIndex, role spectypes.BeaconRole, delay time.Duration) scheduledDuty {
	return scheduledDuty{
		Duty:  createDuty(pk, slot, idx, role),
		Delay: delay,
	}
}

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(logger *zap.Logger, message spectypes.SSVMessage) {
	m.validator.HandleMessage(logger, &message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}

type beaconNode struct {
	*spectestingutils.TestingBeaconNode
}

// GetAttestationData returns attestation data by the given slot and committee index
func (bn beaconNode) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, error) {
	data := spectestingutils.TestingAttestationData
	data.Slot = slot
	data.Index = committeeIndex

	return data, nil
}

func validateByRoot(expected, actual spectypes.Root) error {
	expectedRoot, err := expected.GetRoot()
	if err != nil {
		return fmt.Errorf("root getting error: %w", err)
	}

	actualRoot, err := actual.GetRoot()
	if err != nil {
		return fmt.Errorf("root getting error: %w", err)
	}

	if !bytes.Equal(expectedRoot, actualRoot) {
		return fmt.Errorf("expected and actual roots don't match") //create a breakpoint and check states
	}

	return nil
}

func validateSignedMessage(expected, actual *specqbft.SignedMessage) error {
	for i := range expected.Signers {
		//TODO: add also specqbft.SignedMessage.Signature check
		if expected.Signers[i] != actual.Signers[i] {
			return fmt.Errorf("signers not matching. expected = %+v, actual = %+v", expected.Signers, actual.Signers)
		}
	}

	if err := validateByRoot(expected, actual); err != nil {
		return err
	}

	return nil
}

func Bootstrap(ctx context.Context, operatorIDs []spectypes.OperatorID) (*scenarioContext, error) {
	logger := loggerFactory("Bootstrap")
	logger.Info("creating resources")

	types.SetDefaultDomain(spectypes.PrimusTestnet)

	dbs := make(map[spectypes.OperatorID]basedb.IDb)
	for _, operatorID := range operatorIDs {
		db, err := storage.GetStorageFactory(logger, basedb.Options{
			Type: "badger-memory",
			Path: "",
		})
		if err != nil {
			return nil, err
		}

		dbs[operatorID] = db
	}

	var localNet *p2pv1.LocalNet
	for {
		ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, protocolforks.GenesisForkVersion, len(operatorIDs), getQuorumFromCommittee(len(operatorIDs)), false)
		switch err {
		case p2pv1.CouldNotFindEnoughPeersErr:
			for _, n := range ln.Nodes {
				_ = n.Close()
			}
			continue
		case nil:
		default:
			return nil, err
		}
		localNet = ln
		break
	}

	nodes := make(map[spectypes.OperatorID]network.P2PNetwork)
	nodeKeys := make(map[spectypes.OperatorID]testing.NodeKeys)

	for i, operatorID := range operatorIDs {
		nodes[operatorID] = localNet.Nodes[i]
		nodeKeys[operatorID] = localNet.NodeKeys[i]
	}

	stores := make(map[spectypes.OperatorID]*qbftstorage.QBFTStores)
	kms := make(map[spectypes.OperatorID]spectypes.KeyManager)
	for _, operatorID := range operatorIDs {
		store := qbftstorage.New(dbs[operatorID], "attestations", protocolforks.GenesisForkVersion)

		storageMap := qbftstorage.NewStores()
		storageMap.Add(spectypes.BNRoleAttester, store)
		storageMap.Add(spectypes.BNRoleProposer, store)
		storageMap.Add(spectypes.BNRoleAggregator, store)
		storageMap.Add(spectypes.BNRoleSyncCommittee, store)
		storageMap.Add(spectypes.BNRoleSyncCommitteeContribution, store)

		stores[operatorID] = storageMap
		km := spectestingutils.NewTestingKeyManager()
		kms[operatorID] = km
		nodes[operatorID].RegisterHandlers(logger, protocolp2p.WithHandler(
			protocolp2p.LastDecidedProtocol,
			handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("decided-handler-%d", operatorID)), storageMap, nodes[operatorID]),
		), protocolp2p.WithHandler(
			protocolp2p.DecidedHistoryProtocol,
			handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-handler-%d", operatorID)), storageMap, nodes[operatorID], 25),
		))
	}

	return &scenarioContext{
		ctx:         ctx,
		ln:          localNet,
		operatorIDs: operatorIDs,
		nodes:       nodes,
		nodeKeys:    nodeKeys,
		stores:      stores,
		keyManagers: kms,
		dbs:         dbs,
	}, nil
}

func getShareSetFromCommittee(committee int) *spectestingutils.TestKeySet {
	switch committee {
	case 4:
		return spectestingutils.Testing4SharesSet()
	case 7:
		return spectestingutils.Testing7SharesSet()
	case 10:
		return spectestingutils.Testing10SharesSet()
	default:
		panic("unsupported committee num")
	}
}

func getCommitteeNumFromF(f int) int {
	return 3*f + 1
}

func getQuorumFromCommittee(committee int) int {
	return committee - (committee / 3)
}

func loggerFactory(s string) *zap.Logger {
	l := logex.Build("simulation", zapcore.DebugLevel, nil)
	return l.With(zap.String("who", s))
}
