package scenarios

import (
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// RegularScenario is the regular scenario name
const RegularScenario = "regular"

// regularScenario is the most basic scenario where 4 operators starts qbft for a single validator
type regularScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *ssvtypes.SSVShare
	validators []*validator.Validator
}

// newRegularScenario creates a regular scenario instance
func newRegularScenario(logger *zap.Logger) runner.Scenario {
	return &regularScenario{logger: logger}
}

func (r *regularScenario) NumOfOperators() int {
	return 4
}

func (r *regularScenario) NumOfBootnodes() int {
	return 0
}

func (r *regularScenario) NumOfFullNodes() int {
	return 0
}

func (r *regularScenario) Name() string {
	return RegularScenario
}

type msgRouter struct {
	validator *validator.Validator
}

func (m *msgRouter) Route(message spectypes.SSVMessage) {
	m.validator.HandleMessage(&message)
}

func newMsgRouter(v *validator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}

func (r *regularScenario) PreExecution(ctx *runner.ScenarioContext) error {
	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, r.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}

	for i, v := range validators {
		ctx.LocalNet.Nodes[i].UseMessageRouter(newMsgRouter(v))
	}

	r.validators = validators
	r.sks = sks
	r.share = share

	return nil
}

func (r *regularScenario) Execute(_ *runner.ScenarioContext) error {
	if len(r.sks) == 0 || r.share == nil {
		return errors.New("pre-execution failed")
	}

	var wg sync.WaitGroup
	var startErr error
	for _, val := range r.validators {
		wg.Add(1)
		go func(val *validator.Validator) {
			defer wg.Done()
			if err := val.Start(); err != nil {
				startErr = errors.Wrap(err, "could not start validator")
			}
			<-time.After(time.Second * 3)
		}(val)
	}
	wg.Wait()

	return startErr
}

func (r *regularScenario) PostExecution(ctx *runner.ScenarioContext) error {
	for i, v := range r.validators {
		var pk [48]byte
		copy(pk[:], v.Share.ValidatorPubKey)

		if err := v.StartDuty(&spectypes.Duty{
			Type:                    spectypes.BNRoleAttester,
			PubKey:                  pk,
			Slot:                    spectestingutils.TestingDutySlot,
			ValidatorIndex:          spec.ValidatorIndex(i),
			CommitteeIndex:          spectestingutils.TestingAttesterDuty.CommitteeIndex,
			CommitteesAtSlot:        spectestingutils.TestingAttesterDuty.CommitteesAtSlot,
			CommitteeLength:         spectestingutils.TestingAttesterDuty.CommitteeLength,
			ValidatorCommitteeIndex: spectestingutils.TestingAttesterDuty.ValidatorCommitteeIndex,
		}); err != nil {
			return err
		}
	}

	return nil
}
