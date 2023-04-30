package records

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/record"
	"github.com/pkg/errors"
)

type SignedNodeInfo struct {
	NodeInfo      *NodeInfo
	HandshakeData HandshakeData
	Signature     []byte
}

func (sni *SignedNodeInfo) Domain() string {
	return sni.NodeInfo.Domain()
}

func (sni *SignedNodeInfo) Codec() []byte {
	return sni.NodeInfo.Codec()
}

func (sni *SignedNodeInfo) MarshalRecord() ([]byte, error) {
	parts := []string{
		sni.HandshakeData.SenderPeerID.String(),
		sni.HandshakeData.RecipientPeerID.String(),
		strconv.FormatInt(sni.HandshakeData.Timestamp.Unix(), 10),
		string(sni.HandshakeData.SenderPubKeyPem),
		string(sni.Signature),
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

	sni.HandshakeData.SenderPeerID = peer.ID(ser.Entries[0])
	sni.HandshakeData.RecipientPeerID = peer.ID(ser.Entries[1])

	timeUnix, err := strconv.ParseInt(ser.Entries[2], 10, 64)
	if err != nil {
		return err
	}

	sni.HandshakeData.Timestamp = time.Unix(timeUnix, 0)
	sni.HandshakeData.SenderPubKeyPem = []byte(ser.Entries[3])
	sni.Signature = []byte(ser.Entries[4])

	ni := &NodeInfo{}
	if err := ni.UnmarshalRecord([]byte(ser.Entries[5])); err != nil {
		return err
	}

	sni.NodeInfo = ni

	return nil
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
	return nil
}
