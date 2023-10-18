package commons

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cespare/xxhash/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/protocol"

	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

const (
	lastDecidedProtocol = "/ssv/sync/decided/last/0.0.1"
	historyProtocol     = "/ssv/sync/decided/history/0.0.1"

	peersForSync = 10

	// subnetsCount returns the subnet count for genesis
	subnetsCount uint64 = 128

	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"

	topicPrefix = "ssv.v2"
)

type SignedSSVMessage struct {
	Message   []byte
	Signature []byte
	PubKey    []byte
}

// MarshalSSZTo appends the serialized form of the SignedSSVMessage to the given byte slice.
func (msg *SignedSSVMessage) MarshalSSZTo(dst []byte) ([]byte, error) {
	encoded, err := msg.MarshalSSZ()
	if err != nil {
		return nil, err
	}
	return append(dst, encoded...), nil
}

// SizeSSZ returns the size in bytes that the SignedSSVMessage would take when serialized using SSZ.
func (msg *SignedSSVMessage) SizeSSZ() int {
	// Each length prefix is uint64, so 8 bytes for each of the 3 fields
	return 8 + len(msg.Message) + 8 + len(msg.Signature) + 8 + len(msg.PubKey)
}

// MarshalSSZ serializes the SignedSSVMessage into its SSZ form.
func (msg *SignedSSVMessage) MarshalSSZ() ([]byte, error) {
	var buf bytes.Buffer

	// Writing the Message
	messageLength := uint64(len(msg.Message))
	binary.Write(&buf, binary.LittleEndian, messageLength)
	buf.Write(msg.Message)

	// Writing the Signature
	signatureLength := uint64(len(msg.Signature))
	binary.Write(&buf, binary.LittleEndian, signatureLength)
	buf.Write(msg.Signature)

	// Writing the PubKey
	pubKeyLength := uint64(len(msg.PubKey))
	binary.Write(&buf, binary.LittleEndian, pubKeyLength)
	buf.Write(msg.PubKey)

	return buf.Bytes(), nil
}

// UnmarshalSSZ deserializes the given data into a SignedSSVMessage.
func (msg *SignedSSVMessage) UnmarshalSSZ(data []byte) error {
	buf := bytes.NewReader(data)

	// Reading the Message
	var messageLength uint64
	if err := binary.Read(buf, binary.LittleEndian, &messageLength); err != nil {
		return err
	}
	msg.Message = make([]byte, messageLength)
	if _, err := buf.Read(msg.Message); err != nil {
		return err
	}

	// Reading the Signature
	var signatureLength uint64
	if err := binary.Read(buf, binary.LittleEndian, &signatureLength); err != nil {
		return err
	}
	msg.Signature = make([]byte, signatureLength)
	if _, err := buf.Read(msg.Signature); err != nil {
		return err
	}

	// Reading the PubKey
	var pubKeyLength uint64
	if err := binary.Read(buf, binary.LittleEndian, &pubKeyLength); err != nil {
		return err
	}
	msg.PubKey = make([]byte, pubKeyLength)
	if _, err := buf.Read(msg.PubKey); err != nil {
		return err
	}

	if buf.Len() != 0 {
		return fmt.Errorf("extra data at the end of the buffer")
	}

	return nil
}

// SubnetTopicID returns the topic to use for the given subnet
func SubnetTopicID(subnet int) string {
	if subnet < 0 {
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

// GetTopicFullName returns the topic full name, including prefix
func GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// ValidatorSubnet returns the subnet for the given validator
func ValidatorSubnet(validatorPKHex string) int {
	if len(validatorPKHex) < 10 {
		return -1
	}
	val := hexToUint64(validatorPKHex[:10])
	return int(val % subnetsCount)
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
	return int(subnetsCount)
}

// Topics returns the available topics for this fork.
func Topics() []string {
	topics := make([]string, Subnets())
	for i := 0; i < Subnets(); i++ {
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

type EncryptedSSVMessage struct {
	SSVMessage *spectypes.SSVMessage
	Signature  []byte
}

// EncodeNetworkMsg encodes network message
func EncodeNetworkMsg(msg *spectypes.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func DecodeNetworkMsg(data []byte) (*spectypes.SSVMessage, error) {
	msg := spectypes.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
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
