package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
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
	Name               string
	OperatorIDs        []spectypes.OperatorID
	Identifier         spectypes.MessageID
	ValidatorDelays    map[spectypes.OperatorID]time.Duration
	InitialInstances   map[spectypes.OperatorID][]*protocolstorage.StoredInstance
	Duties             map[spectypes.OperatorID][]scheduledDuty
	ExpectedInstances  map[spectypes.OperatorID][]*protocolstorage.StoredInstance
	InstanceValidators map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error
	StartDutyErrors    map[spectypes.OperatorID]error
}

type scheduledDuty struct {
	Duty  *spectypes.Duty
	Delay time.Duration
}

type scenarioContext struct {
	ctx         context.Context
	logger      *zap.Logger
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

func (it *IntegrationTest) bootstrap(ctx context.Context) (*scenarioContext, error) {
	l := logex.Build("simulation", zapcore.DebugLevel, nil)
	loggerFactory := func(s string) *zap.Logger {
		return l.With(zap.String("who", s))
	}
	logger := loggerFactory(fmt.Sprintf("Bootstrap/%s", it.Name))
	logger.Info("creating resources")

	types.SetDefaultDomain(spectypes.PrimusTestnet)

	dbs := make(map[spectypes.OperatorID]basedb.IDb)
	for _, operatorID := range it.OperatorIDs {
		db, err := storage.GetStorageFactory(basedb.Options{
			Type:   "badger-memory",
			Path:   "",
			Logger: zap.L(),
		})
		if err != nil {
			return nil, err
		}

		dbs[operatorID] = db
	}

	forkVersion := protocolforks.GenesisForkVersion

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, forkVersion, len(it.OperatorIDs), len(it.OperatorIDs)/2, false)
	if err != nil {
		return nil, err
	}

	nodes := make(map[spectypes.OperatorID]network.P2PNetwork)
	nodeKeys := make(map[spectypes.OperatorID]testing.NodeKeys)

	for i, operatorID := range it.OperatorIDs {
		nodes[operatorID] = ln.Nodes[i]
		nodeKeys[operatorID] = ln.NodeKeys[i]
	}

	stores := make(map[spectypes.OperatorID]*qbftstorage.QBFTStores)
	kms := make(map[spectypes.OperatorID]spectypes.KeyManager)
	for _, operatorID := range it.OperatorIDs {
		store := qbftstorage.New(dbs[operatorID], loggerFactory(fmt.Sprintf("qbft-store-%d", operatorID)), "attestations", forkVersion)

		storageMap := qbftstorage.NewStores()
		storageMap.Add(spectypes.BNRoleAttester, store)
		storageMap.Add(spectypes.BNRoleProposer, store)
		storageMap.Add(spectypes.BNRoleAggregator, store)
		storageMap.Add(spectypes.BNRoleSyncCommittee, store)
		storageMap.Add(spectypes.BNRoleSyncCommitteeContribution, store)

		stores[operatorID] = storageMap
		km := spectestingutils.NewTestingKeyManager()
		kms[operatorID] = km
		nodes[operatorID].RegisterHandlers(protocolp2p.WithHandler(
			protocolp2p.LastDecidedProtocol,
			handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("decided-handler-%d", operatorID)), storageMap, nodes[operatorID]),
		), protocolp2p.WithHandler(
			protocolp2p.DecidedHistoryProtocol,
			handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-handler-%d", operatorID)), storageMap, nodes[operatorID], 25),
		))
	}

	sCtx := &scenarioContext{
		ctx:         ctx,
		logger:      logger,
		nodes:       nodes,
		nodeKeys:    nodeKeys,
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
	defer func() {
		_ = sCtx.Close()
		<-time.After(time.Second)
	}()

	validators, err := it.createValidators(sCtx)
	if err != nil {
		return fmt.Errorf("could not create share: %w", err)
	}

	for _, operatorID := range it.OperatorIDs {
		sCtx.nodes[operatorID].UseMessageRouter(newMsgRouter(validators[operatorID]))
	}

	for operatorID, instances := range it.InitialInstances {
		for _, instance := range instances {
			mid := spectypes.MessageIDFromBytes(instance.State.ID)
			if err := sCtx.stores[operatorID].Get(mid.GetRoleType()).SaveHighestInstance(instance); err != nil {
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

			if err := val.Start(); err != nil {
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

			sCtx.logger.Debug("about to start duty", zap.Duration("delay", scheduledDuty.Delay))

			time.AfterFunc(scheduledDuty.Delay, func() {
				sCtx.logger.Info("starting duty")
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

	if it.ExpectedInstances != nil {
		for expectedOperatorID, expectedInstances := range it.ExpectedInstances {
			for i, expectedInstance := range expectedInstances {
				mid := spectypes.MessageIDFromBytes(expectedInstance.State.ID)
				storedInstance, err := sCtx.stores[expectedOperatorID].Get(mid.GetRoleType()).
					GetHighestInstance(expectedInstance.State.ID)
				if err != nil {
					return err
				}
				if storedInstance == nil {
					return fmt.Errorf("stored instance is nil")
				}

				if err := assertInstance(storedInstance, expectedInstance); err != nil {
					return fmt.Errorf("assert instance (oid %v, idx %v): %w", expectedOperatorID, i, err)
				}
			}
		}
	}

	if it.InstanceValidators != nil {
		for operatorID, instanceValidators := range it.InstanceValidators {
			for i, instanceValidator := range instanceValidators {
				mid := spectypes.MessageIDFromBytes(it.Identifier[:])
				storedInstance, err := sCtx.stores[operatorID].Get(mid.GetRoleType()).
					GetHighestInstance(it.Identifier[:])
				if err != nil {
					return err
				}
				if storedInstance == nil {
					return fmt.Errorf("stored instance is nil, operator ID %v, instance index %v", operatorID, i)
				}

				if err := instanceValidator(storedInstance); err != nil {
					return fmt.Errorf("validate instance %d of operator ID %d: %w", i, operatorID, err)
				}
			}
		}
	}

	return nil
}

func (it *IntegrationTest) createValidators(sCtx *scenarioContext) (map[spectypes.OperatorID]*protocolvalidator.Validator, error) {
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
		err := sCtx.keyManagers[operatorID].AddShare(spectestingutils.Testing4SharesSet().Shares[operatorID])
		if err != nil {
			return nil, err
		}

		options := protocolvalidator.Options{
			Storage: sCtx.stores[operatorID],
			Network: sCtx.nodes[operatorID],
			SSVShare: &types.SSVShare{
				Share: *testingShare(spectestingutils.Testing4SharesSet(), operatorID),
				Metadata: types.Metadata{
					BeaconMetadata: &protocolbeacon.ValidatorMetadata{
						Index: spec.ValidatorIndex(1),
					},
					OwnerAddress: "0x0",
					Operators:    operators,
					Liquidated:   false,
				},
			},
			Beacon: beaconNode{spectestingutils.NewTestingBeaconNode()},
			Signer: sCtx.keyManagers[operatorID],
		}

		l := sCtx.logger.With(zap.String("w", fmt.Sprintf("node-%d", operatorID)))

		options.DutyRunners = validator.SetupRunners(sCtx.ctx, l, options)
		val := protocolvalidator.NewValidator(sCtx.ctx, options)
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

func createDuty(pk []byte, slot spec.Slot, idx spec.ValidatorIndex, role spectypes.BeaconRole) *spectypes.Duty {
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
		Type:                    role,
		PubKey:                  pkBytes,
		Slot:                    slot,
		ValidatorIndex:          idx,
		CommitteeIndex:          testingDuty.CommitteeIndex,
		CommitteesAtSlot:        testingDuty.CommitteesAtSlot,
		CommitteeLength:         testingDuty.CommitteeLength,
		ValidatorCommitteeIndex: testingDuty.ValidatorCommitteeIndex,
	}
}

func createScheduledDuty(pk []byte, slot spec.Slot, idx spec.ValidatorIndex, role spectypes.BeaconRole, delay time.Duration) scheduledDuty {
	return scheduledDuty{
		Duty:  createDuty(pk, slot, idx, role),
		Delay: delay,
	}
}

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

func assertInstance(actual *protocolstorage.StoredInstance, expected *protocolstorage.StoredInstance) error {
	if actual == nil && expected == nil {
		return nil
	}

	if actual != nil && expected == nil {
		return fmt.Errorf("expected nil instance")
	}

	if actual == nil && expected != nil {
		return fmt.Errorf("expected non-nil instance")
	}

	if err := assertDecided(actual.DecidedMessage, expected.DecidedMessage); err != nil {
		return fmt.Errorf("decided: %w", err)
	}

	if err := assertState(actual.State, expected.State); err != nil {
		return fmt.Errorf("state: %w", err)
	}

	return nil
}

func assertDecided(actual *specqbft.SignedMessage, expected *specqbft.SignedMessage) error {
	if actual == nil && expected == nil {
		return fmt.Errorf("expected nil")
	}

	if actual == nil && expected != nil {
		return fmt.Errorf("expected non-nil")
	}

	actualRoot, err := actual.GetRoot()
	if err != nil {
		return fmt.Errorf("actual root: %w", err)
	}

	expectedRoot, err := expected.GetRoot()
	if err != nil {
		return fmt.Errorf("expected root: %w", err)
	}

	if !bytes.Equal(actualRoot, expectedRoot) {
		return fmt.Errorf("roots differ")
	}

	return nil
}

func assertState(actual *specqbft.State, expected *specqbft.State) error {
	if actual == nil && expected == nil {
		return fmt.Errorf("expected nil")
	}

	if actual == nil && expected != nil {
		return fmt.Errorf("expected non-nil")
	}

	actualCopy, expectedCopy := *actual, *expected

	// TODO: message checks became broken after https://github.com/bloxapp/ssv/pull/791, they need to be fixed after using validation functions
	//if want, got := len(expectedCopy.PrepareContainer.Msgs), len(actualCopy.PrepareContainer.Msgs); want != got {
	//	return fmt.Errorf("wrong prepare message count, want %d, got %d", want, got)
	//}
	//
	//for round, messages := range expectedCopy.PrepareContainer.Msgs {
	//	for i, message := range messages {
	//		expectedRoot, err := message.GetRoot()
	//		if err != nil {
	//			return fmt.Errorf("get expected prepare message root: %w", err)
	//		}
	//
	//		actualRoot, err := actualCopy.PrepareContainer.Msgs[round][i].GetRoot()
	//		if err != nil {
	//			return fmt.Errorf("get actual prepare message root: %w", err)
	//		}
	//
	//		if !bytes.Equal(expectedRoot, actualRoot) {
	//			return fmt.Errorf("expected and actual prepare roots differ")
	//		}
	//	}
	//}
	//
	//actualCopy.PrepareContainer = nil
	//expectedCopy.PrepareContainer = nil
	//
	// Since the signers are not deterministic, we need to do a simple assertion instead of checking the root of whole state.
	//if expected.Decided {
	//	if want, got := len(expectedCopy.CommitContainer.Msgs), len(actualCopy.CommitContainer.Msgs); want != got {
	//		return fmt.Errorf("wrong commit message count, want %d, got %d", want, got)
	//	}
	//
	//	for round, messages := range expectedCopy.CommitContainer.Msgs {
	//		for i, message := range messages {
	//			expectedRoot, err := message.GetRoot()
	//			if err != nil {
	//				return fmt.Errorf("get expected commit message root: %w", err)
	//			}
	//
	//			actualRoot, err := actualCopy.CommitContainer.Msgs[round][i].GetRoot()
	//			if err != nil {
	//				return fmt.Errorf("get actual commit message root: %w", err)
	//			}
	//
	//			if !bytes.Equal(expectedRoot, actualRoot) {
	//				return fmt.Errorf("expected and actual commit roots differ")
	//			}
	//		}
	//	}
	//
	//	actualCopy.CommitContainer = nil
	//	expectedCopy.CommitContainer = nil
	//}

	actualCopy.ProposeContainer = nil
	expectedCopy.ProposeContainer = nil
	actualCopy.PrepareContainer = nil
	expectedCopy.PrepareContainer = nil
	actualCopy.CommitContainer = nil
	expectedCopy.CommitContainer = nil
	actualCopy.RoundChangeContainer = nil
	expectedCopy.RoundChangeContainer = nil

	actualRoot, err := actualCopy.GetRoot()
	if err != nil {
		return fmt.Errorf("actual root: %w", err)
	}

	expectedRoot, err := expectedCopy.GetRoot()
	if err != nil {
		return fmt.Errorf("expected root: %w", err)
	}

	if !bytes.Equal(actualRoot, expectedRoot) {
		actualStateJSON, err := json.Marshal(actualCopy)
		if err != nil {
			return fmt.Errorf("marshal actual state")
		}

		expectedStateJSON, err := json.Marshal(expectedCopy)
		if err != nil {
			return fmt.Errorf("marshal expected state")
		}

		log.Printf("state: roots differ")
		log.Printf("actual state: %v", string(actualStateJSON))
		log.Printf("expected state: %v", string(expectedStateJSON))

		return fmt.Errorf("roots differ")
	}

	return nil
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

type beaconNode struct {
	*spectestingutils.TestingBeaconNode
}

// GetAttestationData returns attestation data by the given slot and committee index
func (bn beaconNode) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
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
