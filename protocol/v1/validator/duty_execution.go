package validator

import (
	"encoding/hex"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
)

func (v *Validator) comeToConsensusOnInputValue(logger *zap.Logger, duty *spectypes.Duty) (controller.IController, int, []byte, specqbft.Height, error) {
	var inputByts []byte
	var err error

	qbftCtrl, ok := v.ibfts[duty.Type]
	if !ok {
		return nil, 0, nil, 0, errors.Errorf("no ibft for this role [%s]", duty.Type.String())
	}

	switch duty.Type {
	case spectypes.BNRoleAttester:
		attData, err := v.beacon.GetAttestationData(duty.Slot, duty.CommitteeIndex)
		if err != nil {
			return nil, 0, nil, 0, errors.Wrap(err, "failed to get attestation data")
		}
		v.logger.Debug("attestation data", zap.Any("attData", attData))
		// TODO(olegshmuelov): use SSZ encoding
		input := &spectypes.ConsensusData{
			Duty:            duty,
			AttestationData: attData,
		}
		inputByts, err = input.Encode()
		if err != nil {
			return nil, 0, nil, 0, errors.Wrap(err, "could not encode ConsensusData")
		}
		// TODO(olegshmuelov): validate the consensus data using the spec "BeaconAttestationValueCheck"
	default:
		return nil, 0, nil, 0, errors.Errorf("unknown role: %s", duty.Type.String())
	}

	// calculate next seq
	height, err := qbftCtrl.NextSeqNumber()
	if err != nil {
		return nil, 0, nil, 0, errors.Wrap(err, "failed to calculate next sequence number")
	}

	logger.Debug("start instance", zap.Int64("height", int64(height)))
	result, err := qbftCtrl.StartInstance(instance.ControllerStartInstanceOptions{
		Logger:          logger,
		SeqNumber:       height,
		Value:           inputByts,
		RequireMinPeers: true,
	})
	if err != nil {
		return nil, 0, nil, 0, errors.Wrap(err, "could not start ibft instance")
	}
	if result == nil {
		return nil, 0, nil, height, errors.New("instance result returned nil")
	}
	if !result.Decided {
		return nil, 0, nil, height, errors.New("instance did not decide")
	}

	commitData, err := result.Msg.Message.GetCommitData()
	if err != nil {
		return nil, 0, nil, 0, errors.Wrap(err, "could not get commit data")
	}

	return qbftCtrl, len(result.Msg.Signers), commitData.Data, height, nil
}

// StartDuty executes the given duty
func (v *Validator) StartDuty(duty *spectypes.Duty) {
	logger := v.logger.With(
		zap.Time("start_time", v.network.GetSlotStartTime(uint64(duty.Slot))),
		zap.Uint64("committee_index", uint64(duty.CommitteeIndex)),
		zap.Uint64("slot", uint64(duty.Slot)),
		zap.String("duty_type", duty.Type.String()))

	metricsCurrentSlot.WithLabelValues(v.Share.PublicKey.SerializeToHexStr()).Set(float64(duty.Slot))
	logger.Debug("executing duty")

	qbftCtrl, signaturesCount, decidedValue, seqNumber, err := v.comeToConsensusOnInputValue(logger, duty)
	if err != nil {
		logger.Warn("could not come to consensus", zap.Error(err))
		return
	}

	// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
	logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))

	// Sign, aggregate and broadcast signature
	if err := qbftCtrl.PostConsensusDutyExecution(logger, seqNumber, decidedValue, signaturesCount, duty); err != nil {
		logger.Error("could not execute post consensus duty", zap.Error(err))
		return
	}
}
