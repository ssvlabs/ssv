package tests

import (
	"context"
	"encoding/json"
	"fmt"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
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

				duty := createDuty(getKeySet(s.Committee).ValidatorPK.Serialize(), spec.Slot(dutyProp.Slot), dutyProp.Idx, role)
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
