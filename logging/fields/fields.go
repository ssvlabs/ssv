package fields

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/dgraph-io/ristretto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging/fields/stringer"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
)

const (
	FieldAddress             = "address"
	FieldBindIP              = "bindIP"
	FieldBlock               = "block"
	FieldBlockCacheMetrics   = "blockCacheMetricsField"
	FieldConnectionID        = "connectionID"
	FieldConsensusTime       = "ConsensusTime"
	FieldCount               = "count"
	FieldCurrentSlot         = "currentSlot"
	FieldDurationMilli       = "durationMilli"
	FieldENR                 = "ENR"
	FieldEvent               = "event"
	FieldEventID             = "eventID"
	FieldFromBlock           = "fromBlock"
	FieldHeight              = "height"
	FieldIndexCacheMetrics   = "indexCacheMetrics"
	FieldMessageID           = "messageID"
	FieldMessageType         = "messageType"
	FieldName                = "name"
	FieldOperatorId          = "operatorID"
	FieldPeerID              = "peerID"
	FieldPrivateKey          = "privKey"
	FieldPubKey              = "pubKey"
	FieldRole                = "role"
	FieldRound               = "round"
	FieldStartTimeUnixMilli  = "startTimeUnixMilli"
	FieldSubnets             = "subnets"
	FieldSyncOffset          = "syncOffset"
	FieldSyncResults         = "syncResults"
	FieldTargetNodeENR       = "targetNodeENR"
	FieldTopic               = "topic"
	FieldTxHash              = "txHash"
	FieldUpdatedENRLocalNode = "updatedENR"
	FieldValidator           = "validator"
	FieldValidatorMetadata   = "validatorMetadata"
)

func FromBlock(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(FieldFromBlock, val)
}

func SyncOffset(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(FieldSyncOffset, val)
}

func TxHash(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(FieldTxHash, val)
}

func EventID(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(FieldEventID, val)
}

func PubKey(pubKey []byte) zapcore.Field {
	return zap.Stringer(FieldPubKey, stringer.HexStringer{Val: pubKey})
}

func PrivKey(val []byte) zapcore.Field {
	return zap.Stringer(FieldPrivateKey, stringer.HexStringer{Val: val})
}

func Validator(pubKey []byte) zapcore.Field {
	return zap.Stringer(FieldValidator, stringer.HexStringer{Val: pubKey})
}

func AddressURL(val url.URL) zapcore.Field {
	return zap.Stringer(FieldAddress, &val)
}

func Address(val string) zapcore.Field {
	return zap.String(FieldAddress, val)
}

func ENR(val *enode.Node) zapcore.Field {
	return zap.Stringer(FieldENR, val)
}

func ENRStr(val string) zapcore.Field {
	return zap.String(FieldENR, val)
}

func TargetNodeENR(val *enode.Node) zapcore.Field {
	return zap.Stringer(FieldTargetNodeENR, val)
}

func ENRLocalNode(val *enode.LocalNode) zapcore.Field {
	return zap.Stringer(FieldENR, val.Node())
}

func UpdatedENRLocalNode(val *enode.LocalNode) zapcore.Field {
	return zap.Stringer(FieldUpdatedENRLocalNode, val.Node())
}

func Subnets(val records.Subnets) zapcore.Field {
	return zap.Stringer(FieldSubnets, val)
}

func PeerID(val peer.ID) zapcore.Field {
	return zap.Stringer(FieldPeerID, val)
}

func BindIP(val net.IP) zapcore.Field {
	return zap.Stringer(FieldBindIP, val)
}

func DurationMilli(val time.Time) zapcore.Field {
	return zap.Stringer(FieldDurationMilli, stringer.Int64Stringer{Val: time.Since(val).Milliseconds()})
}

func CurrentSlot(network beacon.Network) zapcore.Field {
	return zap.Stringer(FieldCurrentSlot, stringer.Uint64Stringer{Val: uint64(network.EstimatedCurrentSlot())})
}

func StartTimeUnixMilli(network beacon.Network, slot spec.Slot) zapcore.Field {
	return zap.Stringer(FieldStartTimeUnixMilli, stringer.FuncStringer{
		Fn: func() string {
			return strconv.Itoa(int(network.GetSlotStartTime(slot).UnixMilli()))
		},
	})
}

func BlockCacheMetrics(metrics *ristretto.Metrics) zapcore.Field {
	return zap.Stringer(FieldBlockCacheMetrics, metrics)
}

func IndexCacheMetrics(metrics *ristretto.Metrics) zapcore.Field {
	return zap.Stringer(FieldIndexCacheMetrics, metrics)
}

func SyncResults(msgs protocolp2p.SyncResults) zapcore.Field {
	return zap.Stringer(FieldSyncResults, msgs)
}

func OperatorID(operatorId spectypes.OperatorID) zap.Field {
	return zap.Uint64(FieldOperatorId, operatorId)
}

func OperatorIDStr(operatorId string) zap.Field {
	return zap.String(FieldOperatorId, operatorId)
}

func Height(height specqbft.Height) zap.Field {
	return zap.Uint64(FieldHeight, uint64(height))
}

func Round(round specqbft.Round) zap.Field {
	return zap.Uint64(FieldRound, uint64(round))
}

func Role(val spectypes.BeaconRole) zap.Field {
	return zap.Stringer(FieldRole, val)
}

func MessageID(val spectypes.MessageID) zap.Field {
	return zap.Stringer(FieldMessageID, val)
}

func MessageType(val spectypes.MsgType) zap.Field {
	return zap.String(FieldMessageType, message.MsgTypeToString(val))
}

func EventName(val string) zap.Field {
	return zap.String(FieldEvent, val)
}

func ValidatorMetadata(val *beacon.ValidatorMetadata) zap.Field {
	return zap.Any(FieldValidatorMetadata, val)
}

func BlockNumber(val uint64) zap.Field {
	return zap.Stringer(FieldBlock, stringer.Uint64Stringer{Val: val})
}

func Name(val string) zap.Field {
	return zap.String(FieldName, val)
}

func ConnectionID(val string) zap.Field {
	return zap.String(FieldConnectionID, val)
}

func Count(val int) zap.Field {
	return zap.Int(FieldCount, val)
}

func Topic(val string) zap.Field {
	return zap.String(FieldTopic, val)
}

func ConsensusTime(val time.Time) zap.Field {
	return zap.Stringer(FieldConsensusTime, stringer.SinceSeconds{Val: val})
}
