package records

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/record"
	"github.com/mr-tron/base58"
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
		base58.Encode([]byte(sni.HandshakeData.SenderPeerID)),
		base58.Encode([]byte(sni.HandshakeData.RecipientPeerID)),
		strconv.FormatInt(sni.HandshakeData.Timestamp.Unix(), 10),
		base64.StdEncoding.EncodeToString(sni.HandshakeData.SenderPubKeyPem),
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

	if len(ser.Entries[0]) != 0 {
		senderPeerID, err := base58.Decode(ser.Entries[0])
		if err != nil {
			return err
		}
		sni.HandshakeData.SenderPeerID = peer.ID(senderPeerID)
	} else {
		sni.HandshakeData.SenderPeerID = ""
	}

	if len(ser.Entries[1]) != 0 {
		recipientPeerID, err := base58.Decode(ser.Entries[1])
		if err != nil {
			return err
		}
		sni.HandshakeData.RecipientPeerID = peer.ID(recipientPeerID)
	} else {
		sni.HandshakeData.RecipientPeerID = ""
	}

	timeUnix, err := strconv.ParseInt(ser.Entries[2], 10, 64)
	if err != nil {
		return err
	}

	sni.HandshakeData.Timestamp = time.Unix(timeUnix, 0)

	senderPubKeyPem, err := base64.StdEncoding.DecodeString(ser.Entries[3])
	if err != nil {
		return err
	}

	sni.HandshakeData.SenderPubKeyPem = senderPubKeyPem

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
