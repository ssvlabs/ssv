package commons

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/libp2p/go-libp2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// SubnetsCount returns the subnet count for genesis
	SubnetsCount uint64 = 128

	UnknownSubnetId = math.MaxUint64

	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"

	topicPrefix = "ssv.v2"
)

// BigIntSubnetsCount is the big.Int representation of SubnetsCount
var bigIntSubnetsCount *big.Int

func init() {
	bigIntSubnetsCount = new(big.Int).SetUint64(SubnetsCount)
}

const (
	signatureSize    = 256
	signatureOffset  = 0
	operatorIDSize   = 8
	operatorIDOffset = signatureOffset + signatureSize
	MessageOffset    = operatorIDOffset + operatorIDSize
)

// SubnetTopicID returns the topic to use for the given subnet
func SubnetTopicID(subnet uint64) string {
	if subnet == UnknownSubnetId {
		return UnknownSubnet
	}
	return fmt.Sprintf("%d", subnet)
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

// CommitteeSubnet returns the subnet for the given committee
func CommitteeSubnet(cid spectypes.CommitteeID) uint64 {
	subnet := new(big.Int).Mod(new(big.Int).SetBytes(cid[:]), bigIntSubnetsCount)
	return subnet.Uint64()
}

// SetCommitteeSubnet returns the subnet for the given committee, it doesn't allocate memory but uses the passed in big.Int
func SetCommitteeSubnet(bigInt *big.Int, cid spectypes.CommitteeID) {
	bigInt.SetBytes(cid[:])
	bigInt.Mod(bigInt, bigIntSubnetsCount)
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
