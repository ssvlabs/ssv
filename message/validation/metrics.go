package validation

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type metrics interface {
	MessageAccepted(validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	MessageIgnored(reason string, validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	MessageRejected(reason string, validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	SSVMessageType(msgType spectypes.MsgType)
	ConsensusMsgType(msgType specqbft.MessageType, signers int)
	MessageValidationDuration(duration time.Duration, labels ...string)
	SignatureValidationDuration(duration time.Duration, labels ...string)
	MessageSize(size int)
	ActiveMsgValidation(topic string)
	ActiveMsgValidationDone(topic string)
}

type nopMetrics struct{}

func (*nopMetrics) ConsensusMsgType(specqbft.MessageType, int) {}
func (*nopMetrics) MessageAccepted(spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (*nopMetrics) MessageIgnored(string, spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (*nopMetrics) MessageRejected(string, spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (*nopMetrics) SSVMessageType(spectypes.MsgType)                     {}
func (*nopMetrics) MessageValidationDuration(time.Duration, ...string)   {}
func (*nopMetrics) SignatureValidationDuration(time.Duration, ...string) {}
func (*nopMetrics) MessageSize(int)                                      {}
func (*nopMetrics) ActiveMsgValidation(string)                           {}
func (*nopMetrics) ActiveMsgValidationDone(string)                       {}
