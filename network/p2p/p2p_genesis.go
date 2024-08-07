package p2pv1

import (
	"fmt"

	"github.com/pkg/errors"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"go.uber.org/zap"
)

type GenesisP2P struct {
	Network *p2pNetwork
}

func (p *GenesisP2P) Broadcast(message *genesisspectypes.SSVMessage) error {

	zap.L().Debug("broadcasting genesis msg", fields.PubKey(message.MsgID.GetPubKey()), zap.Int("msg_type", int(message.MsgType)))

	if !p.Network.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	if !p.Network.operatorDataStore.OperatorIDReady() {
		return fmt.Errorf("operator ID is not ready")
	}

	encodedMsg, err := commons.EncodeGenesisNetworkMsg(message)
	if err != nil {
		return errors.Wrap(err, "could not decode msg")
	}
	signature, err := p.Network.operatorSigner.Sign(encodedMsg)
	if err != nil {
		return err
	}
	encodedMsg = commons.EncodeGenesisSignedSSVMessage(encodedMsg, p.Network.operatorDataStore.GetOperatorID(), signature)

	if err != nil {
		return fmt.Errorf("could not encode signed ssv message: %w", err)
	}

	share := p.Network.nodeStorage.ValidatorStore().Validator(message.MsgID.GetPubKey())
	if share == nil {
		return fmt.Errorf("could not find validator: %x", message.MsgID.GetPubKey())
	}

	topics := commons.ValidatorTopicID(message.MsgID.GetPubKey())

	for _, topic := range topics {
		p.Network.interfaceLogger.Debug("broadcasting msg",
			fields.PubKey(message.MsgID.GetPubKey()),
			zap.Int("msg_type", int(message.MsgType)),
			fields.Topic(topic))
		if err := p.Network.topicsCtrl.Broadcast(topic, encodedMsg, p.Network.cfg.RequestTimeout); err != nil {
			p.Network.interfaceLogger.Debug("could not broadcast msg", fields.PubKey(message.MsgID.GetPubKey()), zap.Error(err))
			return fmt.Errorf("could not broadcast msg: %w", err)
		}
	}
	return nil
}

// Subscribe subscribes to validator subnet
func (n *GenesisP2P) Subscribe(pk genesisspectypes.ValidatorPK) error {
	return n.Network.Subscribe(spectypes.ValidatorPK(pk))
}
