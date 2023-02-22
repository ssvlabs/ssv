package logging

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/dgraph-io/ristretto"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/bloxapp/ssv/network/records"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	AddressField             = "address"
	BindIPField              = "bindIP"
	BlockCacheMetricsField   = "blockCacheMetricsField"
	CurrentSlotField         = "currentSlot"
	DurationField            = "duration"
	EnrField                 = "enr"
	EventIDField             = "eventID"
	FromBlockField           = "fromBlock"
	IdentifierField          = "identifier"
	IndexCacheMetricsField   = "indexCacheMetrics"
	MessageIDField           = "messageID"
	PeerIDField              = "peerID"
	PrivateKeyField          = "privKey"
	PubKeyField              = "pubKey"
	ResultsField             = "results"
	StartTimeField           = "startTime"
	SubnetsField             = "subnets"
	SyncOffsetField          = "syncOffset"
	TargetNodeEnrField       = "targetNodeEnr"
	TxHashField              = "txHash"
	UpdatedEnrLocalNodeField = "updated_enr"
	ValidatorField           = "validator"
)

func FromBlock(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(FromBlockField, val)
}

func SyncOffset(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(SyncOffsetField, val)
}

func TxHash(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(TxHashField, val)
}

func EventID(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(EventIDField, val)
}

func Identifier(val fmt.Stringer) zapcore.Field {
	return zap.Stringer(EventIDField, val)
}

func PubKey(val []byte) zapcore.Field {
	return zap.Stringer(PubKeyField, hexStringer{val})
}

func PrivKey(val []byte) zapcore.Field {
	return zap.Stringer(PrivateKeyField, hexStringer{val})
}

func Validator(val []byte) zapcore.Field {
	return zap.Stringer(ValidatorField, hexStringer{val})
}

func Address(val url.URL) zapcore.Field {
	return zap.Stringer(AddressField, &val)
}

func Enr(val *enode.Node) zapcore.Field {
	return zap.Stringer(EnrField, val)
}

func TargetNodeEnr(val *enode.Node) zapcore.Field {
	return zap.Stringer(TargetNodeEnrField, val)
}

func EnrLocalNode(val *enode.LocalNode) zapcore.Field {
	return zap.Stringer(EnrField, val.Node())
}

func UpdatedEnrLocalNode(val *enode.LocalNode) zapcore.Field {
	return zap.Stringer(UpdatedEnrLocalNodeField, val.Node())
}

func Subnets(val records.Subnets) zapcore.Field {
	return zap.Stringer(SubnetsField, val)
}

func PeerID(val peer.ID) zapcore.Field {
	return zap.Stringer(PeerIDField, val)
}

func BindIP(val net.IP) zapcore.Field {
	return zap.Stringer(BindIPField, val)
}

func MessageID(val spectypes.MessageID) zapcore.Field {
	return zap.Stringer(MessageIDField, val)
}

func Duration(val time.Time) zapcore.Field {
	return zap.Stringer(DurationField, int64Stringer{time.Since(val).Nanoseconds()})
}

func CurrentSlot(network beacon.Network) zapcore.Field {
	return zap.Stringer(CurrentSlotField, uint64Stringer{uint64(network.EstimatedCurrentSlot())})
}

type funcStringer struct {
	fn func() string
}

func (s funcStringer) String() string {
	return s.fn()
}

func StartTime(network beacon.Network, slot spec.Slot) zapcore.Field {
	return zap.Stringer(StartTimeField, funcStringer{
		fn: func() string {
			return strconv.Itoa(int(network.GetSlotStartTime(slot).UnixNano()))
		},
	})
}

func BlockCacheMetrics(metrics *ristretto.Metrics) zapcore.Field {
	return zap.Stringer(BlockCacheMetricsField, metrics)
}

func IndexCacheMetrics(metrics *ristretto.Metrics) zapcore.Field {
	return zap.Stringer(IndexCacheMetricsField, metrics)
}

func Results(msgs []protocolp2p.SyncResult) zapcore.Field {
	return zap.Stringer(ResultsField, funcStringer{
		fn: func() string {
			var v []string
			for _, m := range msgs {
				var sm *specqbft.SignedMessage
				if m.Msg.MsgType == spectypes.SSVConsensusMsgType {
					sm = &specqbft.SignedMessage{}
					if err := sm.Decode(m.Msg.Data); err != nil {
						v = append(v, fmt.Sprintf("(%v)", err))
						continue
					}
					v = append(
						v,
						fmt.Sprintf(
							"(type=%d height=%d round=%d)",
							m.Msg.MsgType,
							sm.Message.Height,
							sm.Message.Round,
						),
					)
				}
				v = append(v, fmt.Sprintf("(type=%d)", m.Msg.MsgType))
			}

			return strings.Join(v, ", ")
		},
	})
}
