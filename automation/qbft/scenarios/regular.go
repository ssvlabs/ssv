package scenarios

import (
	"sync"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// RegularScenario is the regular scenario name
const RegularScenario = "regular"

// regularScenario is the most basic scenario where 4 operators starts qbft for a single validator
type regularScenario struct {
	sCtx       *runner.ScenarioContext
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *ssvtypes.SSVShare
	validators []*validator.Validator
}

// newRegularScenario creates a regular scenario instance
func newRegularScenario(logger *zap.Logger) runner.Scenario {
	return &regularScenario{
		logger: logger,
	}
}

func (r *regularScenario) Name() string {
	return RegularScenario
}

func (r *regularScenario) ApplyCtx(sCtx *runner.ScenarioContext) {
	r.sCtx = sCtx
}

func (r *regularScenario) Config() runner.ScenarioConfig {
	return runner.ScenarioConfig{
		Operators: 4,
		BootNodes: 0,
		FullNodes: 0,
		Roles:     []spectypes.BeaconRole{spectypes.BNRoleAttester},
	}
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

func (r *regularScenario) Run(t *testing.T) error {
	ctx := r.sCtx

	share, sks, validators, err := protocoltesting.CreateShareAndValidators(ctx.Ctx, r.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}

	for i, v := range validators {
		ctx.LocalNet.Nodes[i].UseMessageRouter(newMsgRouter(v))
	}

	r.validators = validators
	r.sks = sks
	r.share = share

	if len(r.sks) == 0 || r.share == nil {
		return errors.New("failed to create share and validators")
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

	if startErr != nil {
		return startErr
	}

	for i, v := range r.validators {
		var pk [48]byte
		copy(pk[:], v.Share.ValidatorPubKey)

		for _, role := range r.Config().Roles {
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

			if err := v.StartDuty(&spectypes.Duty{
				Type:                    role,
				PubKey:                  pk,
				Slot:                    spectestingutils.TestingDutySlot,
				ValidatorIndex:          spec.ValidatorIndex(i),
				CommitteeIndex:          testingDuty.CommitteeIndex,
				CommitteesAtSlot:        testingDuty.CommitteesAtSlot,
				CommitteeLength:         testingDuty.CommitteeLength,
				ValidatorCommitteeIndex: testingDuty.ValidatorCommitteeIndex,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}
