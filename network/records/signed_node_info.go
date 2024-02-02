package records

import (
	"github.com/libp2p/go-libp2p/core/crypto"
)

type AnyNodeInfo interface {
	Codec() []byte
	Domain() string
	MarshalRecord() ([]byte, error)
	UnmarshalRecord(data []byte) error
	Seal(netPrivateKey crypto.PrivKey) ([]byte, error)
	Consume(data []byte) error
	GetNodeInfo() *NodeInfo
}
