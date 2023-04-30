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
	return func(sender peer.ID, sni *records.SignedNodeInfo) (bool, error) {
		if networkID != sni.NodeInfo.NetworkID {
			return false, errors.Errorf("networkID '%s' instead of '%s'", sni.NodeInfo.NetworkID, networkID)
		}
		return true, nil
	}
}

func SenderRecipientIPsCheckFilter(me peer.ID) HandshakeFilter {
	return func(sender peer.ID, sni *records.SignedNodeInfo) (bool, error) {
		if sni.HandshakeData.RecipientPeerID != me {
			return false, errors.Errorf("recepient peer ID '%s' instead of '%s'", sni.HandshakeData.RecipientPeerID, me)
		}

		if sni.HandshakeData.SenderPeerID != sender {
			return false, errors.Errorf("sender peer ID '%s' instead of '%s'", sni.HandshakeData.SenderPeerID, sender)
		}

		return true, nil
	}
}

func SignatureCheckFilter() HandshakeFilter {
	return func(sender peer.ID, sni *records.SignedNodeInfo) (bool, error) {
		publicKey, err := rsaencryption.ConvertPemToPublicKey(sni.HandshakeData.SenderPubKeyPem)
		if err != nil {
			return false, err
		}

		hashed := sni.HandshakeData.Hash()
		if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], sni.Signature); err != nil {
			return false, err
		}

		if difference := time.Since(sni.HandshakeData.Timestamp); difference > AllowedDifference {
			return false, fmt.Errorf("signature made %f seconds ago, should no more than %f seconds ago", difference.Seconds(), AllowedDifference.Seconds())
		}

		return true, nil
	}
}

func RegisteredOperatorsFilter(logger *zap.Logger, nodeStorage storage.Storage) HandshakeFilter { //operator is not registered means operator not whitelisted
	return func(sender peer.ID, sni *records.SignedNodeInfo) (bool, error) {
		_, found, err := nodeStorage.GetOperatorDataByPubKey(logger, sni.HandshakeData.SenderPubKeyPem)
		if !found {
			return false, errors.Wrap(err, "operator wasn't found, probably not registered to a contract")
		}

		return true, nil
	}
}
