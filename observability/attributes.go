package observability

import (
	"encoding/hex"
	"math"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/network"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/utils"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	protocolTypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	RunnerRoleAttrKey = "ssv.runner.role"
)

func BeaconRoleAttribute(role spectypes.BeaconRole) attribute.KeyValue {
	return attribute.String("ssv.beacon.role", role.String())
}

func BeaconEpochAttribute(epoch phase0.Epoch) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.beacon.epoch",
		Value: Uint64AttributeValue(uint64(epoch)),
	}
}

func BeaconVersionAttribute(version spec.DataVersion) attribute.KeyValue {
	return attribute.String("ssv.beacon.version", version.String())
}

func RunnerRoleAttribute(role spectypes.RunnerRole) attribute.KeyValue {
	return attribute.String(RunnerRoleAttrKey, utils.FormatRunnerRole(role))
}

func BeaconSlotAttribute(slot phase0.Slot) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.beacon.slot",
		Value: Uint64AttributeValue(uint64(slot)),
	}
}

func DutyRoundAttribute(round specqbft.Round) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.round",
		Value: Uint64AttributeValue(uint64(round)),
	}
}

func DutyIDAttribute(id string) attribute.KeyValue {
	return attribute.String("ssv.validator.duty.id", id)
}

func BeaconPeriodAttribute(period uint64) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.beacon.period",
		Value: Uint64AttributeValue(period),
	}
}

func BeaconBlockRootAttribute(root [32]byte) attribute.KeyValue {
	return attribute.String("ssv.beacon.block.root", hex.EncodeToString(root[:]))
}

func CommitteeIndexAttribute(index phase0.CommitteeIndex) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.committee.index",
		Value: Uint64AttributeValue(uint64(index)),
	}
}

func CommitteeIDAttribute(id spectypes.CommitteeID) attribute.KeyValue {
	return attribute.String("ssv.validator.duty.committee.id", hex.EncodeToString(id[:]))
}

func DutyCountAttribute(count int) attribute.KeyValue {
	return attribute.Int("ssv.validator.duty.count", count)
}

func ValidatorCountAttribute(count int) attribute.KeyValue {
	return attribute.Int("ssv.validator.count", count)
}

func BeaconBlockHashAttribute(hash phase0.Hash32) attribute.KeyValue {
	return attribute.String("ssv.beacon.block.hash", hash.String())
}

func BeaconBlockIsBlindedAttribute(isBlinded bool) attribute.KeyValue {
	return attribute.Bool("ssv.beacon.block.is_blinded", isBlinded)
}

func BeaconBlockRootCountAttribute(count int) attribute.KeyValue {
	return attribute.Int("ssv.beacon.block.root.count", count)
}

func ValidatorHasQuorumAttribute(hasQuorum bool) attribute.KeyValue {
	return attribute.Bool("ssv.validator.has_quorum", hasQuorum)
}

func ValidatorMsgTypeAttribute(msgType spectypes.MsgType) attribute.KeyValue {
	const attrKey = "ssv.validator.msg.type"
	switch msgType {
	case spectypes.SSVConsensusMsgType:
		return attribute.String(attrKey, "SSVConsensusMsgType")
	case spectypes.SSVPartialSignatureMsgType:
		return attribute.String(attrKey, "SSVPartialSignatureMsgType")
	case message.SSVEventMsgType:
		return attribute.String(attrKey, "SSVEventMsgType")
	default:
		return attribute.String(attrKey, "UnknownMsgType")
	}
}

func ValidatorMsgIDAttribute(msgID spectypes.MessageID) attribute.KeyValue {
	return attribute.String("ssv.validator.msg.id", msgID.String())
}

func ValidatorIndexAttribute(index phase0.ValidatorIndex) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.index",
		Value: Uint64AttributeValue(uint64(index)),
	}
}

func ValidatorSignerAttribute(signer spectypes.OperatorID) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.signer",
		Value: Uint64AttributeValue(signer),
	}
}

func ValidatorProposerAttribute(proposer spectypes.OperatorID) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.proposer",
		Value: Uint64AttributeValue(proposer),
	}
}

func ValidatorPublicKeyAttribute(pubKey phase0.BLSPubKey) attribute.KeyValue {
	return attribute.String("ssv.validator.pubkey", pubKey.String())
}

func ValidatorEventTypeAttribute(t protocolTypes.EventType) attribute.KeyValue {
	return attribute.String("ssv.validator.event.type", t.String())
}

func RoundChangeReasonAttribute(reason string) attribute.KeyValue {
	return attribute.String("ssv.validator.duty.round.change_reason", reason)
}

func NetworkDirectionAttribute(direction network.Direction) attribute.KeyValue {
	return attribute.String("ssv.p2p.connection.direction", strings.ToLower(direction.String()))
}

func Uint64AttributeValue(value uint64) attribute.Value {
	if value <= math.MaxInt64 {
		return attribute.Int64Value(int64(value))
	}
	return attribute.StringValue(strconv.FormatUint(value, 10))
}
