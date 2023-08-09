package records

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/record"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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

type SignedNodeInfo struct {
	NodeInfo      *NodeInfo
	HandshakeData HandshakeData
	Signature     []byte
}

// GetNodeInfo returns inner representation of the info
func (sni *SignedNodeInfo) GetNodeInfo() *NodeInfo {
	return sni.NodeInfo
}

func (sni *SignedNodeInfo) Domain() string {
	return sni.NodeInfo.Domain()
}

func (sni *SignedNodeInfo) Codec() []byte {
	return sni.NodeInfo.Codec()
}

// MarshalRecord serializes SignedNodeInfo
// IMPORTANT: MarshalRecord rounds SignedNodeInfo.HandshakeData.Timestamp to seconds
func (sni *SignedNodeInfo) MarshalRecord() ([]byte, error) {
	parts := []string{
		base64.StdEncoding.EncodeToString([]byte(sni.HandshakeData.SenderPeerID)),
		base64.StdEncoding.EncodeToString([]byte(sni.HandshakeData.RecipientPeerID)),
		strconv.FormatInt(sni.HandshakeData.Timestamp.Unix(), 10),
		string(sni.HandshakeData.SenderPublicKey),
		base64.StdEncoding.EncodeToString(sni.Signature),
	}

	rawNi, err := sni.NodeInfo.MarshalRecord()
	if err != nil {
		return nil, err
	}
	parts = append(parts, string(rawNi))

	ser := newSerializable(parts...)
	return json.Marshal(ser)
}

func (sni *SignedNodeInfo) UnmarshalRecord(data []byte) error {
	ser := serializable{}
	if err := json.Unmarshal(data, &ser); err != nil {
		return err
	}

	if len(ser.Entries) < 6 {
		return errors.Errorf("unvalid message size")
	}

	senderPeerID, err := base64.StdEncoding.DecodeString(ser.Entries[0])
	if err != nil {
		return err
	}

	sni.HandshakeData.SenderPeerID = peer.ID(senderPeerID)

	recipientPeerID, err := base64.StdEncoding.DecodeString(ser.Entries[1])
	if err != nil {
		return err
	}

	sni.HandshakeData.RecipientPeerID = peer.ID(recipientPeerID)

	timeUnix, err := strconv.ParseInt(ser.Entries[2], 10, 64)
	if err != nil {
		return err
	}

	sni.HandshakeData.Timestamp = time.Unix(timeUnix, 0)

	sni.HandshakeData.SenderPublicKey = []byte(ser.Entries[3])

	signature, err := base64.StdEncoding.DecodeString(ser.Entries[4])
	if err != nil {
		return err
	}

	sni.Signature = signature

	ni := &NodeInfo{}
	if err := ni.UnmarshalRecord([]byte(ser.Entries[5])); err != nil {
		return err
	}

	sni.NodeInfo = ni

	return nil
}

// Seal seals and encodes the record to be sent to other peers
// IMPORTANT: Seal rounds sni.HandshakeData.Timestamp to seconds
func (sni *SignedNodeInfo) Seal(netPrivateKey crypto.PrivKey) ([]byte, error) {
	ev, err := record.Seal(sni, netPrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not seal record")
	}

	data, err := ev.Marshal()
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal envelope")
	}
	return data, nil
}

// Consume takes a raw envelope and extracts the parsed record
func (sni *SignedNodeInfo) Consume(data []byte) error {
	evParsed, err := record.ConsumeTypedEnvelope(data, &SignedNodeInfo{})
	if err != nil {
		return errors.Wrap(err, "could not consume envelope")
	}
	parsed, err := evParsed.Record()
	if err != nil {
		return errors.Wrap(err, "could not get record")
	}
	rec, ok := parsed.(*SignedNodeInfo)
	if !ok {
		return errors.New("could not convert to NodeRecord")
	}
	*sni = *rec
	zap.L().Debug("Consumed record", zap.Any("sni", sni))
	return nil
}
