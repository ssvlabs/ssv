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
	"github.com/dgraph-io/ristretto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/logging/fields/stringer"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/utils/format"
)

const (
	FieldABI                 = "abi"
	FieldABIVersion          = "abi_version"
	FieldAddress             = "address"
	FieldBeaconRole          = "beacon_role"
	FieldBindIP              = "bind_ip"
	FieldBlock               = "block"
	FieldBlockHash           = "block_hash"
	FieldBlockCacheMetrics   = "block_cache_metrics_field"
	FieldBlockVersion        = "block_version"
	FieldClusterIndex        = "cluster_index"
	FieldCommitteeID         = "committee_id"
	FieldCommitteeIndex      = "committee_index"
	FieldConfig              = "config"
	FieldConnectionID        = "connection_id"
	FieldPreConsensusTime    = "pre_consensus_time"
	FieldPostConsensusTime   = "post_consensus_time"
	FieldConsensusTime       = "consensus_time"
	FieldBlockTime           = "block_time"
	FieldBeaconDataTime      = "beacon_data_time"
	FieldCount               = "count"
	FieldCurrentSlot         = "current_slot"
	FieldDomain              = "domain"
	FieldDuration            = "duration"
	FieldDuties              = "duties"
	FieldDutyExecutorID      = "duty_executor_id"
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
	FieldProtocolID          = "protocol_id"
	FieldPubKey              = "pubkey"
	FieldQuorumTime          = "quorum_time"
	FieldRole                = "role"
	FieldRound               = "round"
	FieldSlot                = "slot"
	FieldStartTimeUnixMilli  = "start_time_unix_milli"
	FieldSubmissionTime      = "submission_time"
	FieldTotalConsensusTime  = "total_consensus_time"
	FieldSubnets             = "subnets"
	FieldSyncOffset          = "sync_offset"
	FieldSyncResults         = "sync_results"
	FieldTargetNodeENR       = "target_node_enr"
	FieldToBlock             = "to_block"
	FieldTook                = "took"
	FieldTopic               = "topic"
	FieldTxHash              = "tx_hash"
	FieldType                = "type"
	FieldUpdatedENRLocalNode = "updated_enr"
	FieldValidator           = "validator"
	FieldValidatorIndex      = "validator_index"
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

func ValidatorIndex(index phase0.ValidatorIndex) zapcore.Field {
	return zap.Uint64(FieldValidatorIndex, uint64(index))
}

func DutyExecutorID(senderID []byte) zapcore.Field {
	return zap.Stringer(FieldDutyExecutorID, stringer.HexStringer{Val: senderID})
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

func Subnets(val commons.Subnets) zapcore.Field {
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

func BeaconRole(val spectypes.BeaconRole) zap.Field {
	return zap.Stringer(FieldBeaconRole, val)
}

func Role(val spectypes.RunnerRole) zap.Field {
	return zap.Stringer(FieldRole, val)
}
func ExporterRole(val spectypes.BeaconRole) zap.Field {
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

func PreConsensusTime(val time.Duration) zap.Field {
	return zap.String(FieldPreConsensusTime, FormatDuration(val))
}

func ConsensusTime(val time.Duration) zap.Field {
	return zap.String(FieldConsensusTime, strconv.FormatFloat(val.Seconds(), 'f', 5, 64))
}

func PostConsensusTime(val time.Duration) zap.Field {
	return zap.String(FieldPostConsensusTime, FormatDuration(val))
}

func BlockTime(val time.Duration) zap.Field {
	return zap.String(FieldBlockTime, FormatDuration(val))
}

func BeaconDataTime(val time.Duration) zap.Field {
	return zap.String(FieldBeaconDataTime, FormatDuration(val))
}

func SubmissionTime(val time.Duration) zap.Field {
	return zap.String(FieldSubmissionTime, FormatDuration(val))
}
func TotalConsensusTime(val time.Duration) zap.Field {
	return zap.String(FieldTotalConsensusTime, FormatDuration(val))
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

func ProtocolID(val [6]byte) zap.Field {
	return zap.String(FieldProtocolID, hex.EncodeToString(val[:]))
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

func FormatDutyID(epoch phase0.Epoch, slot phase0.Slot, role string, index phase0.ValidatorIndex) string {
	return fmt.Sprintf("%v-e%v-s%v-v%v", role, epoch, slot, index)
}

func FormatCommittee(operators []spectypes.OperatorID) string {
	var opids []string
	for _, op := range operators {
		opids = append(opids, fmt.Sprint(op))
	}
	return strings.Join(opids, "_")
}

func FormatCommitteeDutyID(operators []spectypes.OperatorID, epoch phase0.Epoch, slot phase0.Slot) string {
	return fmt.Sprintf("COMMITTEE-%s-e%d-s%d", FormatCommittee(operators), epoch, slot)
}

func Duties(epoch phase0.Epoch, duties []*spectypes.ValidatorDuty) zap.Field {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(FormatDutyID(epoch, duty.Slot, duty.Type.String(), duty.ValidatorIndex))
	}
	return zap.String(FieldDuties, b.String())
}

func Root(r [32]byte) zap.Field {
	return zap.String("root", hex.EncodeToString(r[:]))
}
func BlockRoot(r [32]byte) zap.Field {
	return zap.String("block_root", hex.EncodeToString(r[:]))
}

func Config(val fmt.Stringer) zap.Field {
	return zap.Stringer(FieldConfig, val)
}

func ClusterIndex(cluster contract.ISSVNetworkCoreCluster) zap.Field {
	return zap.Uint64(FieldClusterIndex, cluster.Index)
}

func CommitteeID(val spectypes.CommitteeID) zap.Field {
	return zap.String(FieldCommitteeID, hex.EncodeToString(val[:]))
}

func Owner(addr common.Address) zap.Field {
	return zap.String(FieldOwnerAddress, addr.Hex())
}

func Type(v any) zapcore.Field {
	return zap.String(FieldType, fmt.Sprintf("%T", v))
}

func FormatDuration(val time.Duration) string {
	return strconv.FormatFloat(val.Seconds(), 'f', 5, 64)
}
