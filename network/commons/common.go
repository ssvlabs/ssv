package commons

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/protocol"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

const (
	lastDecidedProtocol = "/ssv/sync/decided/last/0.0.1"
	historyProtocol     = "/ssv/sync/decided/history/0.0.1"

	peersForSync = 10

	// SubnetsCount returns the subnet count for genesis
	SubnetsCount uint64 = 128

	UnknownSubnetId = math.MaxUint64

	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"

	topicPrefix = "ssv.v2"
)

const (
	signatureSize    = 256
	signatureOffset  = 0
	operatorIDSize   = 8
	operatorIDOffset = signatureOffset + signatureSize
	MessageOffset    = operatorIDOffset + operatorIDSize
)

// EncodeGenesisSignedSSVMessage serializes the message, op id and signature into bytes
// DEPRECATED, TODO: remove post-fork
func EncodeGenesisSignedSSVMessage(message []byte, operatorID genesisspectypes.OperatorID, signature []byte) []byte {
	b := make([]byte, signatureSize+operatorIDSize+len(message))
	copy(b[signatureOffset:], signature)
	binary.LittleEndian.PutUint64(b[operatorIDOffset:], operatorID)
	copy(b[MessageOffset:], message)
	return b
}

// DecodeGenesisSignedSSVMessage deserializes signed message bytes messsage, op id and a signature
// DEPRECATED, TODO: remove post-fork
func DecodeGenesisSignedSSVMessage(encoded []byte) ([]byte, genesisspectypes.OperatorID, []byte, error) {
	if len(encoded) < MessageOffset {
		return nil, 0, nil, fmt.Errorf("unexpected encoded message size of %d", len(encoded))
	}

	message := encoded[MessageOffset:]
	operatorID := binary.LittleEndian.Uint64(encoded[operatorIDOffset : operatorIDOffset+operatorIDSize])
	signature := encoded[signatureOffset : signatureOffset+signatureSize]
	return message, operatorID, signature, nil
}

// SubnetTopicID returns the topic to use for the given subnet
func SubnetTopicID(subnet uint64) string {
	if subnet == UnknownSubnetId {
		return UnknownSubnet
	}
	return fmt.Sprintf("%d", subnet)
}

// ValidatorTopicID returns the topic to use for the given validator
func ValidatorTopicID(pkByts []byte) []string {
	pkHex := hex.EncodeToString(pkByts)
	subnet := ValidatorSubnet(pkHex)
	return []string{SubnetTopicID(subnet)}
}

func CommitteeTopicID(cid spectypes.CommitteeID) []string {
	return []string{fmt.Sprintf("%d", CommitteeSubnet(cid))}
}

// GetTopicFullName returns the topic full name, including prefix
func GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// ValidatorSubnet returns the subnet for the given validator
func ValidatorSubnet(validatorPKHex string) uint64 {
	if len(validatorPKHex) < 10 {
		return UnknownSubnetId
	}
	val := hexToUint64(validatorPKHex[:10])

	return val % SubnetsCount
}

// CommitteeSubnet returns the subnet for the given committee
func CommitteeSubnet(cid spectypes.CommitteeID) uint64 {
	subnet := new(big.Int).Mod(new(big.Int).SetBytes(cid[:]), new(big.Int).SetUint64(SubnetsCount))
	return subnet.Uint64()
}

// MsgIDFunc is the function that maps a message to a msg_id
type MsgIDFunc func(msg []byte) string

// MsgID returns msg_id for the given message
func MsgID() MsgIDFunc {
	return func(msg []byte) string {
		if len(msg) == 0 {
			return ""
		}

		b := make([]byte, 12)
		binary.LittleEndian.PutUint64(b, xxhash.Sum64(msg))
		return string(b)
	}
}

// Subnets returns the subnets count for this fork
func Subnets() int {
	return int(SubnetsCount)
}

// Topics returns the available topics for this fork.
func Topics() []string {
	topics := make([]string, Subnets())
	for i := uint64(0); i < SubnetsCount; i++ {
		topics[i] = GetTopicFullName(SubnetTopicID(i))
	}
	return topics
}

// AddOptions implementation
func AddOptions(opts []libp2p.Option) []libp2p.Option {
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
	// opts = append(opts, libp2p.DisableRelay())
	return opts
}

// EncodeGenesisNetworkMsg encodes network message
// TODO: DEPRECATED, remove post-fork
func EncodeGenesisNetworkMsg(msg *genesisspectypes.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeGenesisNetworkMsg decodes network message
// TODO: DEPRECATED, remove post-fork
func DecodeGenesisNetworkMsg(data []byte) (*genesisspectypes.SSVMessage, error) {
	msg := genesisspectypes.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// EncodeNetworkMsg encodes network message
func EncodeNetworkMsg(msg *spectypes.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func DecodeNetworkMsg(data []byte) (*spectypes.SSVMessage, error) {
	msg := spectypes.SSVMessage{}
	if err := msg.Decode(data); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ProtocolID returns the protocol id of the given protocol,
// and the amount of peers for distribution
func ProtocolID(prot p2pprotocol.SyncProtocol) (protocol.ID, int) {
	switch prot {
	case p2pprotocol.LastDecidedProtocol:
		return lastDecidedProtocol, peersForSync
	case p2pprotocol.DecidedHistoryProtocol:
		return historyProtocol, peersForSync
	}
	return "", 0
}

func hexToUint64(hexStr string) uint64 {
	result, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return uint64(0)
	}
	return result
}
