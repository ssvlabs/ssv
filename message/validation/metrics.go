package validation

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type metrics interface {
	MessageAccepted(role spectypes.BeaconRole, round specqbft.Round)
	MessageIgnored(reason string, role spectypes.BeaconRole, round specqbft.Round)
	MessageRejected(reason string, role spectypes.BeaconRole, round specqbft.Round)
	SSVMessageType(msgType spectypes.MsgType)
	ConsensusMsgType(msgType specqbft.MessageType, signers int)
	MessageValidationDuration(duration time.Duration, labels ...string)
	SignatureValidationDuration(duration time.Duration, labels ...string)
	MessageSize(size int)
	ActiveMsgValidation(topic string)
	ActiveMsgValidationDone(topic string)
	InCommitteeMessage(msgType spectypes.MsgType, decided bool)
	NonCommitteeMessage(msgType spectypes.MsgType, decided bool)
}

type nopMetrics struct{}

func (*nopMetrics) ConsensusMsgType(specqbft.MessageType, int)                   {}
func (*nopMetrics) MessageAccepted(spectypes.BeaconRole, specqbft.Round)         {}
func (*nopMetrics) MessageIgnored(string, spectypes.BeaconRole, specqbft.Round)  {}
func (*nopMetrics) MessageRejected(string, spectypes.BeaconRole, specqbft.Round) {}
func (*nopMetrics) SSVMessageType(spectypes.MsgType)                             {}
func (*nopMetrics) MessageValidationDuration(time.Duration, ...string)           {}
func (*nopMetrics) SignatureValidationDuration(time.Duration, ...string)         {}
func (*nopMetrics) MessageSize(int)                                              {}
func (*nopMetrics) ActiveMsgValidation(string)                                   {}
func (*nopMetrics) ActiveMsgValidationDone(string)                               {}
func (*nopMetrics) InCommitteeMessage(spectypes.MsgType, bool)                   {}
func (*nopMetrics) NonCommitteeMessage(spectypes.MsgType, bool)                  {}
