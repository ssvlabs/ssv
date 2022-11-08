package validator

import (
	"encoding/hex"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
)

func (v *Validator) comeToConsensusOnInputValue(logger *zap.Logger, duty *spectypes.Duty) (controller.IController, int, []byte, error) {
	var inputBytes []byte
	var err error

	qbftCtrl, ok := v.ibfts[duty.Type]
	if !ok {
		return nil, 0, nil, errors.Errorf("no ibft for this role [%s]", duty.Type.String())
	}

	switch duty.Type {
	case spectypes.BNRoleAttester:
		attestationDataStartTime := time.Now()
		attData, err := v.beacon.GetAttestationData(duty.Slot, duty.CommitteeIndex)
		if err != nil {
			return nil, 0, nil, errors.Wrap(err, "failed to get attestation data")
		}
		v.logger.Debug("attestation data", zap.Any("attData", attData))
		metricsDurationConsensus.WithLabelValues("attestation_data_request", v.Share.PublicKey.SerializeToHexStr()).
			Observe(time.Since(attestationDataStartTime).Seconds())

		// TODO(olegshmuelov): use SSZ encoding
		input := &spectypes.ConsensusData{
			Duty:            duty,
			AttestationData: attData,
		}
		inputBytes, err = input.Encode()
		if err != nil {
			return nil, 0, nil, errors.Wrap(err, "could not encode ConsensusData")
		}
		// TODO(olegshmuelov): validate the consensus data using the spec "BeaconAttestationValueCheck"
	default:
		return nil, 0, nil, errors.Errorf("unknown role: %s", duty.Type.String())
	}

	// calculate next seq
	height, err := qbftCtrl.NextHeightNumber()
	if err != nil {
		return nil, 0, nil, errors.Wrap(err, "failed to calculate next sequence number")
	}

	logger.Debug("start instance", zap.Int64("with height", int64(height)))
	result, err := qbftCtrl.StartInstance(instance.ControllerStartInstanceOptions{
		Logger:          logger,
		Height:          height,
		Value:           inputBytes,
		RequireMinPeers: true,
	}, nil)
	if err != nil {
		return nil, 0, nil, errors.Wrap(err, "could not start ibft instance")
	}
	if result == nil {
		return nil, 0, nil, errors.New("instance result returned nil")
	}
	if !result.Decided {
		return nil, 0, nil, errors.New("instance did not decide")
	}

	commitData, err := result.Msg.Message.GetCommitData()
	if err != nil {
		return nil, 0, nil, errors.Wrap(err, "could not get commit data")
	}

	return qbftCtrl, len(result.Msg.Signers), commitData.Data, nil
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

	consensusStartTime := time.Now()
	qbftCtrl, signaturesCount, decidedValue, err := v.comeToConsensusOnInputValue(logger, duty)
	if err != nil {
		logger.Warn("could not come to consensus", zap.Error(err))
		return
	}

	// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
	logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))
	metricsDurationConsensus.WithLabelValues("consensus", v.Share.PublicKey.SerializeToHexStr()).
		Observe(time.Since(consensusStartTime).Seconds())

	// Sign, aggregate and broadcast signature
	if err := qbftCtrl.PostConsensusDutyExecution(logger, decidedValue, signaturesCount, duty.Type); err != nil {
		logger.Error("could not execute post consensus duty", zap.Error(err))
		return
	}
}
