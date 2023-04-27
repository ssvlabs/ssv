package fields

import (
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/dgraph-io/ristretto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging/fields/stringer"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
)

const (
	FieldABI                 = "abi"
	FieldABIVersion          = "abi_version"
	FieldAddress             = "address"
	FieldBindIP              = "bind_ip"
	FieldBlock               = "block"
	FieldBlockCacheMetrics   = "block_cache_metrics_field"
	FieldConnectionID        = "connection_id"
	FieldConsensusTime       = "consensus_time"
	FieldCount               = "count"
	FieldCurrentSlot         = "current_slot"
	FieldDomain              = "domain"
	FieldDuration            = "duration"
	FieldDutyID              = "duty_id"
	FieldENR                 = "enr"
	FieldErrors              = "errors"
	FieldEvent               = "event"
	FieldEventID             = "event_id"
	FieldFork                = "fork"
	FieldFromBlock           = "from_block"
	FieldHeight              = "height"
	FieldIndexCacheMetrics   = "index_cache_metrics"
	FieldMessageID           = "message_id"
	FieldMessageType         = "message_type"
	FieldName                = "name"
	FieldNetwork             = "network"
	FieldNetworkID           = "network_id"
	FieldOperatorId          = "operator_id"
	FieldPeerID              = "peer_id"
	FieldPrivateKey          = "privkey"
	FieldPubKey              = "pubkey"
	FieldRole                = "role"
	FieldRound               = "round"
	FieldSlot                = "slot"
	FieldStartTimeUnixMilli  = "start_time_unix_milli"
	FieldSubnets             = "subnets"
	FieldSyncOffset          = "sync_offset"
	FieldSyncResults         = "sync_results"
	FieldTargetNodeENR       = "target_node_enr"
	FieldToBlock             = "to_block"
	FieldTopic               = "topic"
	FieldTxHash              = "tx_hash"
	FieldUpdatedENRLocalNode = "updated_enr"
	FieldValidator           = "validator"
	FieldValidatorMetadata   = "validator_metadata"
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

func Duration(val time.Time) zapcore.Field {
	return zap.Stringer(FieldDuration, stringer.Float64Stringer{Val: time.Since(val).Seconds()})
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

func ConsensusTime(val time.Duration) zap.Field {
	return zap.Float64(FieldConsensusTime, val.Seconds())
}

func DutyID(val string) zap.Field {
	return zap.String(FieldDutyID, val)
}

func Slot(val phase0.Slot) zap.Field {
	return zap.Uint64(FieldSlot, uint64(val))
}

func Network(val string) zap.Field {
	return zap.String(FieldNetwork, val)
}

func Domain(val spectypes.DomainType) zap.Field {
	return zap.Stringer(FieldDomain, format.DomainType(val))
}

func NetworkID(val string) zap.Field {
	return zap.String(FieldNetworkID, val)
}

func Fork(val forksprotocol.ForkVersion) zap.Field {
	return zap.String(FieldFork, string(val))
}

func ABIVersion(val string) zap.Field {
	return zap.String(FieldABIVersion, val)
}

func ABI(val string) zap.Field {
	return zap.String(FieldABI, val)
}

func Errors(val []error) zap.Field {
	return zap.Errors(FieldErrors, val)
}

func ToBlock(val *big.Int) zap.Field {
	return zap.Int64(FieldToBlock, val.Int64())
}

func FormatDutyID(epoch phase0.Epoch, duty *spectypes.Duty) string {
	return fmt.Sprintf("%v-e%v-s%v-v%v", duty.Type.String(), epoch, duty.Slot, duty.ValidatorIndex)
}
