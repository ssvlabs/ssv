package p2p

import (
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
)

// SubscribeToValidatorNetwork  for new validator create new topic, subscribe and start listen
func (n *p2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	topicID := n.fork.ValidatorTopicID(validatorPk.Serialize())
	name := getTopicName(topicID)
	cn, err := n.topicManager.Subscribe(name)
	if err != nil {
		return err
	}

	go func() {
		for n.ctx.Err() == nil {
			select {
			case msg := <-cn:
				if msg == nil {
					continue
				}
				n.trace("received raw network msg", zap.ByteString("network.Message bytes", msg.Data))
				cm, err := n.fork.DecodeNetworkMsg(msg.Data)
				if err != nil {
					n.logger.Error("failed to un-marshal message", zap.Error(err))
					continue
				}
				if n.reportLastMsg && len(msg.ReceivedFrom) > 0 {
					reportLastMsg(msg.ReceivedFrom.String())
				}
				n.propagateSignedMsg(cm)
			default:
				return
			}
		}
	}()

	return nil
}

// AllPeers returns all connected peers for a validator PK (except for the validator itself)
func (n *p2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	topicID := n.fork.ValidatorTopicID(validatorPk)
	name := getTopicName(topicID)
	peers, err := n.topicManager.Peers(name)
	if err != nil {
		return nil, err
	}
	return n.allPeersOfTopic(peers), nil
}

// AllPeers returns all connected peers for a validator PK (except for the validator itself and public peers like exporter)
func (n *p2pNetwork) allPeersOfTopic(peers []peer.ID) []string {
	ret := make([]string, 0)

	skippedPeers := map[string]bool{
		n.cfg.ExporterPeerID: true,
	}
	for _, p := range peers {
		nodeType, err := n.peersIndex.getNodeType(p)
		if err != nil {
			n.logger.Debug("could not get node type", zap.String("peer", p.String()))
			continue
		}
		if !validateNodeType(nodeType) {
			continue
		}
		if s := peerToString(p); !skippedPeers[s] {
			ret = append(ret, peerToString(p))
		}
	}

	return ret
}

// validateNodeType return if peer nodeType is valid.
func validateNodeType(nt NodeType) bool {
	return nt != Exporter
}

// getTopicName return formatted topic name
func getTopicName(pk string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, pk)
}

//// getTopicName return formatted topic name
//func unwrapTopicName(topicName string) string {
//	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
//}
