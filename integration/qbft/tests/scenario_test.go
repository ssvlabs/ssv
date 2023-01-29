package tests

import (
	"context"
	"encoding/json"
	"fmt"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/operator/duties"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/sync/handlers"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

type Scenario struct {
	Committee           int
	ExpectedHeight      qbft.Height
	InitialInstances    map[spectypes.OperatorID]*StoredInstanceProperties
	Duties              map[spectypes.OperatorID][]DutyProperties
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
			func(id spectypes.OperatorID) {
				storage := newStores(s.shared.Logger)

				var initialInstance *protocolstorage.StoredInstance
				if s.InitialInstances != nil && s.InitialInstances[id] != nil {
					initialInstance = createInstance(t, getKeySet(s.Committee), id, s.InitialInstances[id].Height, role)
					require.NoError(t, storage.Get(role).SaveHighestInstance(initialInstance))
				}

				s.validators[id] = createValidator(t, ctx, id, getKeySet(s.Committee), s.shared.Logger, storage, s.shared.Nodes[id])

				s.shared.Nodes[id].RegisterHandlers(protocolp2p.WithHandler(
					protocolp2p.LastDecidedProtocol,
					handlers.LastDecidedHandler(s.shared.Logger.With(zap.String("who", fmt.Sprintf("decided-handler-%d", id))), storage, s.shared.Nodes[id]),
				), protocolp2p.WithHandler(
					protocolp2p.DecidedHistoryProtocol,
					handlers.HistoryHandler(s.shared.Logger.With(zap.String("who", fmt.Sprintf("history-handler-%d", id))), storage, s.shared.Nodes[id], 25),
				))

				for {
					peers, err := s.shared.Nodes[id].Peers(getKeySet(s.Committee).ValidatorPK.Serialize())
					require.NoError(t, err)
					if len(peers) >= 1 { // TODO: make sure that this check is not redundant. if really - remove all goroutine stuff related to creating validator
						break
					}

					s.shared.Logger.Debug("didn't connect any peers, trying again")
					time.Sleep(200 * time.Millisecond)
				}
			}(spectypes.OperatorID(id))
		}

		//invoking duties
		for id, dutiesForOneValidator := range s.Duties {
			func(id spectypes.OperatorID, dutiesForOneValidator []DutyProperties) { //launching goroutine for every validator
				for _, dutyProp := range dutiesForOneValidator {
					time.Sleep(time.Duration(dutyProp.Delay))

					duty := createDuty(getKeySet(s.Committee).ValidatorPK.Serialize(), spec.Slot(dutyProp.Slot), dutyProp.Idx, role)
					ssvMsg, err := duties.CreateDutyExecuteMsg(duty, getKeySet(s.Committee).ValidatorPK)
					require.NoError(t, err)
					dec, err := queue.DecodeSSVMessage(ssvMsg)
					require.NoError(t, err)

					s.validators[id].Queues[role].Q.Push(dec)
				}
			}(id, dutiesForOneValidator)
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

				if storedInstance != nil && storedInstance.State.Height == s.ExpectedHeight {
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
