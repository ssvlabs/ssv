package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
)

func main() {
	logger := logex.Build("simulation", zapcore.DebugLevel, nil)
	runner.Start(logger, newRegularScenario(logger))
}

// regularScenario is the most basic scenario where 4 operators starts qbft for a single validator
type regularScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *beacon.Share
	validators []validator.IValidator
}

// newRegularScenario creates a regular scenario instance
func newRegularScenario(logger *zap.Logger) runner.Scenario {
	return &regularScenario{logger: logger}
}

func (r *regularScenario) NumOfOperators() int {
	return 4
}

func (r *regularScenario) NumOfExporters() int {
	return 0
}

func (r *regularScenario) Name() string {
	return "regular"
}

func (r *regularScenario) PreExecution(ctx *runner.ScenarioContext) error {
	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, r.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}
	// save all references
	r.validators = validators
	r.sks = sks
	r.share = share

	oids := make([]message.OperatorID, 0)
	keys := make(map[message.OperatorID]*bls.SecretKey)
	for oid := range share.Committee {
		keys[oid] = sks[uint64(oid)]
		oids = append(oids, oid)
	}

	msgs, err := testing.CreateMultipleSignedMessages(keys, message.Height(0), message.Height(4), func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
		commitData := message.CommitData{Data: []byte(fmt.Sprintf("msg-data-%d", height))}
		commitDataBytes, err := commitData.Encode()
		if err != nil {
			panic(err)
		}

		return oids, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester),
			Data:       commitDataBytes,
		}
	})
	if err != nil {
		return err
	}

	for i, store := range ctx.Stores {
		if i == 0 { // skip first store
			continue
		}
		if err := store.SaveDecided(msgs...); err != nil {
			return errors.Wrap(err, "could not save decided messages")
		}
		if err := store.SaveLastDecided(msgs[len(msgs)-1]); err != nil {
			return errors.Wrap(err, "could not save decided messages")
		}
	}

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
		go func(val validator.IValidator) {
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
	msgs, err := ctx.Stores[0].GetDecided(message.NewIdentifier(r.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(4))
	if err != nil {
		return err
	}
	if len(msgs) < 4 {
		return errors.New("node-0 didn't sync all messages")
	}
	r.logger.Debug("msgs", zap.Any("msgs", msgs))

	return nil
}
