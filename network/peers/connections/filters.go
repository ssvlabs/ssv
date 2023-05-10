package connections

import (
	"crypto"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var AllowedDifference = 30 * time.Second

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkID string) HandshakeFilter {
	return func(sender peer.ID, sni records.SignedNodeInfo) error {
		if networkID != sni.NodeInfo.NetworkID {
			return errors.Errorf("networkID '%s' instead of '%s'", sni.NodeInfo.NetworkID, networkID)
		}
		return nil
	}
}

func SenderRecipientIPsCheckFilter(me peer.ID) HandshakeFilter { // for some reason we're loosing 'me' value
	return func(sender peer.ID, sni records.SignedNodeInfo) error {
		if sni.HandshakeData.RecipientPeerID != me {
			return errors.Errorf("recepient peer ID '%s' instead of '%s'", sni.HandshakeData.RecipientPeerID, me)
		}

		if sni.HandshakeData.SenderPeerID != sender {
			return errors.Errorf("sender peer ID '%s' instead of '%s'", sni.HandshakeData.SenderPeerID, sender)
		}

		return nil
	}
}

func SignatureCheckFilter() HandshakeFilter {
	return func(sender peer.ID, sni records.SignedNodeInfo) error {
		publicKey, err := rsaencryption.ConvertPemToPublicKey(sni.HandshakeData.SenderPubicKey)
		if err != nil {
			return err
		}

		hashed := sni.HandshakeData.Hash()
		if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], sni.Signature); err != nil {
			return err
		}

		if difference := time.Since(sni.HandshakeData.Timestamp); difference > AllowedDifference {
			return fmt.Errorf("signature made %f seconds ago, should no more than %f seconds ago", difference.Seconds(), AllowedDifference.Seconds())
		}

		return nil
	}
}

func RegisteredOperatorsFilter(logger *zap.Logger, nodeStorage storage.Storage, keysConfigWhitelist []string) HandshakeFilter {
	return func(sender peer.ID, sni records.SignedNodeInfo) error {
		if len(sni.HandshakeData.SenderPubicKey) == 0 {
			return errors.New("empty SenderPubicKey")
		}

		for _, key := range keysConfigWhitelist {
			if key == string(sni.HandshakeData.SenderPubicKey) {
				return nil
			}
		}

		data, found, err := nodeStorage.GetOperatorDataByPubKey(logger, sni.HandshakeData.SenderPubicKey) //проверить что мы отправляем зашифрованный в base64
		if !found || data != nil {
			return errors.Wrap(err, "operator wasn't found, probably not registered to a contract")
		}

		return nil
	}
}
