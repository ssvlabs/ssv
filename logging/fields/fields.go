package fields

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/dgraph-io/ristretto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/logging/fields/stringer"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/utils/format"
)

const (
	FieldABI                 = "abi"
	FieldABIVersion          = "abi_version"
	FieldAddress             = "address"
	FieldBindIP              = "bind_ip"
	FieldBlock               = "block"
	FieldBlockHash           = "block_hash"
	FieldBlockVersion        = "block_version"
	FieldBlockCacheMetrics   = "block_cache_metrics_field"
	FieldBuilderProposals    = "builder_proposals"
	FieldClusterIndex        = "cluster_index"
	FieldConfig              = "config"
	FieldConnectionID        = "connection_id"
	FieldConsensusTime       = "consensus_time"
	FieldCount               = "count"
	FieldTook                = "took"
	FieldCurrentSlot         = "current_slot"
	FieldDomain              = "domain"
	FieldDuration            = "duration"
	FieldDuties              = "duties"
	FieldDutyID              = "duty_id"
	FieldENR                 = "enr"
	FieldEpoch               = "epoch"
	FieldErrors              = "errors"
	FieldEvent               = "event"
	FieldEventID             = "event_id"
	FieldFeeRecipient        = "fee_recipient"
	FieldFromBlock           = "from_block"
	FieldHeight              = "height"
	FieldIndexCacheMetrics   = "index_cache_metrics"
	FieldMessageID           = "msg_id"
	FieldMessageType         = "msg_type"
	FieldName                = "name"
	FieldNetwork             = "network"
	FieldOperatorId          = "operator_id"
	FieldOperatorIDs         = "operator_ids"
	FieldOperatorPubKey      = "operator_pubkey"
	FieldOwnerAddress        = "owner_address"
	FieldPeerID              = "peer_id"
	FieldPeerScore           = "peer_score"
	FieldPrivKey             = "privkey"
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
	FieldType                = "type"
	FieldUpdatedENRLocalNode = "updated_enr"
	FieldValidator           = "validator"
	FieldValidatorMetadata   = "validator_metadata"
)

func FromBlock(val uint64) zapcore.Field {
	return zap.Uint64(FieldFromBlock, val)
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

func PrivKey(val []byte) zapcore.Field {
	return zap.Stringer(FieldPrivKey, stringer.HexStringer{Val: val})
}

func PubKey(pubKey []byte) zapcore.Field {
	return zap.Stringer(FieldPubKey, stringer.HexStringer{Val: pubKey})
}

func OperatorPubKey(pubKey []byte) zapcore.Field {
	return zap.String(FieldOperatorPubKey, string(pubKey))
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

func PeerScore(val float64) zapcore.Field {
	return zap.Stringer(FieldPeerScore, stringer.Float64Stringer{Val: val})
}

func BindIP(val net.IP) zapcore.Field {
	return zap.Stringer(FieldBindIP, val)
}

func Duration(val time.Time) zapcore.Field {
	return zap.Stringer(FieldDuration, stringer.Float64Stringer{Val: time.Since(val).Seconds()})
}

func CurrentSlot(slot phase0.Slot) zapcore.Field {
	return zap.Stringer(FieldCurrentSlot, stringer.Uint64Stringer{Val: uint64(slot)})
}

func StartTimeUnixMilli(time time.Time) zapcore.Field {
	return zap.Stringer(FieldStartTimeUnixMilli, stringer.FuncStringer{
		Fn: func() string {
			return strconv.Itoa(int(time.UnixMilli()))
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

func OperatorIDs(operatorIDs []spectypes.OperatorID) zap.Field {
	return zap.Uint64s(FieldOperatorIDs, operatorIDs)
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

func QBFTMessageType(val specqbft.MessageType) zap.Field {
	return zap.String(FieldMessageType, message.QBFTMsgTypeToString(val))
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

func BlockHash(val phase0.Hash32) zap.Field {
	return zap.Stringer(FieldBlockHash, val)
}

func BlockVersion(val spec.DataVersion) zap.Field {
	return zap.Stringer(FieldBlockVersion, val)
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

func Took(duration time.Duration) zap.Field {
	return zap.Duration(FieldTook, duration)
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

func Epoch(val phase0.Epoch) zap.Field {
	return zap.Uint64(FieldEpoch, uint64(val))
}

func Slot(val phase0.Slot) zap.Field {
	return zap.Uint64(FieldSlot, uint64(val))
}

func Domain(val spectypes.DomainType) zap.Field {
	return zap.Stringer(FieldDomain, format.DomainType(val))
}

func Network(val string) zap.Field {
	return zap.String(FieldNetwork, val)
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

func ToBlock(val uint64) zap.Field {
	return zap.Uint64(FieldToBlock, val)
}

func FeeRecipient(pubKey []byte) zap.Field {
	return zap.Stringer(FieldFeeRecipient, stringer.HexStringer{Val: pubKey})
}

func BuilderProposals(v bool) zap.Field {
	return zap.Bool(FieldBuilderProposals, v)
}

func FormatDutyID(epoch phase0.Epoch, duty *spectypes.Duty) string {
	return fmt.Sprintf("%v-e%v-s%v-v%v", duty.Type.String(), epoch, duty.Slot, duty.ValidatorIndex)
}

func Duties(epoch phase0.Epoch, duties []*spectypes.Duty) zap.Field {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(FormatDutyID(epoch, duty))
	}
	return zap.String(FieldDuties, b.String())
}

func Root(r [32]byte) zap.Field {
	return zap.String("root", hex.EncodeToString(r[:]))
}

func Config(val fmt.Stringer) zap.Field {
	return zap.Stringer(FieldConfig, val)
}

func ClusterIndex(cluster contract.ISSVNetworkCoreCluster) zap.Field {
	return zap.Uint64(FieldClusterIndex, cluster.Index)
}

func Owner(addr common.Address) zap.Field {
	return zap.String(FieldOwnerAddress, addr.Hex())
}

func Type(v any) zapcore.Field {
	return zap.String(FieldType, fmt.Sprintf("%T", v))
}
