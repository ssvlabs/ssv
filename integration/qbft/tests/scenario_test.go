package tests

import (
	"context"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/validator"
	protocolbeacon "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

var (
	KeySet4Committee  = spectestingutils.Testing4SharesSet()
	KeySet7Committee  = spectestingutils.Testing7SharesSet()
	KeySet10Committee = spectestingutils.Testing10SharesSet()
	KeySet13Committee = spectestingutils.Testing13SharesSet()
)

type Scenario struct {
	Committee           int
	ExpectedHeight      int
	Duties              map[spectypes.OperatorID]DutyProperties
	ValidationFunctions map[spectypes.OperatorID]func(t *testing.T, committee int, actual *protocolstorage.StoredInstance)
	shared              SharedData
	validators          map[spectypes.OperatorID]*protocolvalidator.Validator
}

func (s *Scenario) Run(t *testing.T, role spectypes.BeaconRole) {
	t.Run(role.String(), func(t *testing.T) {
		//preparing resources
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s.validators = map[spectypes.OperatorID]*protocolvalidator.Validator{} //initiating map

		s.shared = GetSharedData(t)

		logger := logging.TestLogger(t)

		//initiating validators
		for id := 1; id <= s.Committee; id++ {
			id := spectypes.OperatorID(id)
			s.validators[id] = createValidator(t, ctx, id, getKeySet(s.Committee), logger, s.shared.Nodes[id])
		}

		//invoking duties
		for id, dutyProp := range s.Duties {
			go func(id spectypes.OperatorID, dutyProp DutyProperties) { //launching goroutine for every validator
				time.Sleep(dutyProp.Delay)

				duty := createDuty(getKeySet(s.Committee).ValidatorPK.Serialize(), dutyProp.Slot, dutyProp.ValidatorIndex, role)
				var pk spec.BLSPubKey
				copy(pk[:], getKeySet(s.Committee).ValidatorPK.Serialize())
				ssvMsg, err := validator.CreateDutyExecuteMsg(duty, pk, networkconfig.TestNetwork.Domain)
				require.NoError(t, err)
				dec, err := queue.DecodeSSVMessage(ssvMsg)
				require.NoError(t, err)

				s.validators[id].Queues[role].Q.Push(dec)
			}(id, dutyProp)
		}

		//validating state of validator after invoking duties
		for id, validationFunc := range s.ValidationFunctions {
			identifier := spectypes.NewMsgID(types.GetDefaultDomain(), getKeySet(s.Committee).ValidatorPK.Serialize(), role)
			//getting stored state of validator
			var storedInstance *protocolstorage.StoredInstance
			for {
				var err error
				storedInstance, err = s.validators[id].Storage.Get(spectypes.MessageIDFromBytes(identifier[:]).GetRoleType()).GetHighestInstance(identifier[:])
				require.NoError(t, err)

				if storedInstance != nil {
					break
				}

				time.Sleep(500 * time.Millisecond) // waiting for duty will be done and storedInstance would be saved
			}

			//validating stored state of validator
			validationFunc(t, s.Committee, storedInstance)
		}

		// teardown
		for _, val := range s.validators {
			val.Stop()
		}

		// HACK: sleep to wait for function calls to github.com/herumi/bls-eth-go-binary
		// to return. When val.Stop() is called, the context.Context that controls the procedure to
		// pop & process messages by the validator from its queue will stop running new iterations.
		// But if a procedure to pop & process a message is in progress when val.Stop() is called, the
		// popped message will still be processed. When a message is processed the github.com/herumi/bls-eth-go-binary
		// library is used. When this test function returns, the validator and all of its resources are
		// garbage collected by the Go runtime. Because the bls-eth-go-binary library is a cgo wrapper of a C/C++ library,
		// the C/C++ runtime will continue to try to access the signature data of the message even though it has been garbage
		// collected already by the Go runtime. This causes the C code to receive a SIGSEGV (SIGnal SEGmentation Violation)
		// which crashes the Go runtime in a way that is not recoverable. A long term fix would involve signaling
		// when the validator ConsumeQueue() function has returned, as its processing is synchronous.
		time.Sleep(time.Millisecond * 1000)
	})
}

// getKeySet returns the keyset for a given committee size. Some tests have a
// committee size smaller than 3f+1 in order to simulate cases where operators are offline
func getKeySet(committee int) *spectestingutils.TestKeySet {
	switch committee {
	case 1, 2, 3, 4:
		return KeySet4Committee
	case 5, 6, 7:
		return KeySet7Committee
	case 8, 9, 10:
		return KeySet10Committee
	case 11, 12, 13:
		return KeySet13Committee
	default:
		panic("unsupported committee size")

	}
}

func testingShare(keySet *spectestingutils.TestKeySet, id spectypes.OperatorID) *spectypes.Share { //TODO: check dead-locks
	return &spectypes.Share{
		OperatorID:      id,
		ValidatorPubKey: keySet.ValidatorPK.Serialize(),
		SharePubKey:     keySet.Shares[id].GetPublicKey().Serialize(),
		DomainType:      testingutils.TestingSSVDomainType,
		Quorum:          keySet.Threshold,
		PartialQuorum:   keySet.PartialThreshold,
		Committee:       keySet.Committee(),
	}
}

func quorum(committee int) int {
	return (committee*2 + 1) / 3 // committee = 3f+1; quorum = 2f+1
}

func newStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		panic(err)
	}

	storageMap := qbftstorage.NewStores()

	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRoleValidatorRegistration,
	}
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(db, role.String()))
	}

	return storageMap
}

func createValidator(t *testing.T, pCtx context.Context, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet, pLogger *zap.Logger, node network.P2PNetwork) *protocolvalidator.Validator {
	ctx, cancel := context.WithCancel(pCtx)
	validatorPubKey := keySet.Shares[id].GetPublicKey().Serialize()

	logger := pLogger.With(fields.OperatorID(id), fields.Validator(validatorPubKey))

	km := spectestingutils.NewTestingKeyManager()
	err := km.AddShare(keySet.Shares[id])
	require.NoError(t, err)

	options := protocolvalidator.Options{
		Storage:       newStores(logger),
		Network:       node,
		BeaconNetwork: networkconfig.TestNetwork.Beacon,
		SSVShare: &types.SSVShare{
			Share: *testingShare(keySet, id),
			Metadata: types.Metadata{
				BeaconMetadata: &protocolbeacon.ValidatorMetadata{
					Index: spec.ValidatorIndex(1),
				},
				OwnerAddress: common.HexToAddress("0x0"),
				Liquidated:   false,
			},
		},
		Beacon: spectestingutils.NewTestingBeaconNode(),
		Signer: km,
	}

	options.DutyRunners = validator.SetupRunners(ctx, logger, options)
	val := protocolvalidator.NewValidator(ctx, cancel, options)
	node.UseMessageRouter(newMsgRouter(logger, val))
	started, err := val.Start(logger)
	require.NoError(t, err)
	require.True(t, started)

	return val
}
