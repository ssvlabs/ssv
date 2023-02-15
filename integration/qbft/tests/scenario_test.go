package tests

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/validator"
	protocolforks "github.com/bloxapp/ssv/protocol/forks"
	protocolbeacon "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
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

		//initiating validators
		for id := 1; id <= s.Committee; id++ {
			id := spectypes.OperatorID(id)
			s.validators[id] = createValidator(t, ctx, id, getKeySet(s.Committee), s.shared.Logger, s.shared.Nodes[id])
		}

		//invoking duties
		for id, dutyProp := range s.Duties {
			go func(id spectypes.OperatorID, dutyProp DutyProperties) { //launching goroutine for every validator
				time.Sleep(time.Duration(dutyProp.Delay))

				duty := createDuty(getKeySet(s.Committee).ValidatorPK.Serialize(), spec.Slot(dutyProp.Slot), dutyProp.ValidatorIndex, role)
				require.NoError(t, s.validators[id].StartDuty(duty))
			}(id, dutyProp)
		}

		//validating state of validator after invoking duties
		for id, validationFunc := range s.ValidationFunctions {
			identifier := spectypes.NewMsgID(getKeySet(s.Committee).ValidatorPK.Serialize(), role)
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

			// logging stored instance
			jsonInstance, err := json.Marshal(storedInstance)
			require.NoError(t, err)
			fmt.Println(string(jsonInstance))

			//validating stored state of validator
			validationFunc(t, s.Committee, storedInstance)
		}

		// teardown
		for _, val := range s.validators {
			require.NoError(t, val.Stop())
		}
	})
}

func getKeySet(committee int) *spectestingutils.TestKeySet {
	switch committee {
	case 1, 2, 3, 4, 5, 6:
		return KeySet4Committee
	case 7, 8, 9:
		return KeySet7Committee
	case 10, 11, 12:
		return KeySet10Committee
	case 13, 14, 15:
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
		DomainType:      spectypes.PrimusTestnet,
		Quorum:          keySet.Threshold,
		PartialQuorum:   keySet.PartialThreshold,
		Committee:       keySet.Committee(),
	}
}

func quorum(committee int) int {
	return (committee*2 + 1) / 3 // committee = 3f+1; quorum = 2f+1 // https://drive.google.com/file/d/1bP_MLq0MM7ZBSR0Ddh7HUPcc42vVUKwz/view?usp=share_link
}

func newStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: logger,
	})
	if err != nil {
		panic(err)
	}

	store := qbftstorage.New(db, logger, "integration-tests", protocolforks.GenesisForkVersion)

	stores := qbftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, store)
	stores.Add(spectypes.BNRoleProposer, store)
	stores.Add(spectypes.BNRoleAggregator, store)
	stores.Add(spectypes.BNRoleSyncCommittee, store)
	stores.Add(spectypes.BNRoleSyncCommitteeContribution, store)

	return stores
}

func createValidator(
	t *testing.T,
	pCtx context.Context,
	id spectypes.OperatorID,
	keySet *spectestingutils.TestKeySet,
	pLogger *zap.Logger,
	node network.P2PNetwork,
) *protocolvalidator.Validator {
	ctx, cancel := context.WithCancel(pCtx)
	validatorPubKey := keySet.Shares[id].GetPublicKey().Serialize()
	logger := pLogger.With(zap.Int("operator-id", int(id)), zap.String("validator", hex.EncodeToString(validatorPubKey)))
	km := spectestingutils.NewTestingKeyManager()
	err := km.AddShare(keySet.Shares[id])
	require.NoError(t, err)

	options := protocolvalidator.Options{
		Storage: newStores(logger),
		Network: node,
		SSVShare: &types.SSVShare{
			Share: *testingShare(keySet, id),
			Metadata: types.Metadata{
				BeaconMetadata: &protocolbeacon.ValidatorMetadata{
					Index: spec.ValidatorIndex(1),
				},
				OwnerAddress: "0x0",
				Liquidated:   false,
			},
		},
		Beacon: spectestingutils.NewTestingBeaconNode(),
		Signer: km,
	}

	options.DutyRunners = validator.SetupRunners(ctx, logger, options)
	val := protocolvalidator.NewValidator(ctx, cancel, options)
	node.UseMessageRouter(newMsgRouter(val))
	require.NoError(t, val.Start())

	return val
}
