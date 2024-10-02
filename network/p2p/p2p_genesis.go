package p2pv1

import (
	"encoding/hex"
	"fmt"

	"github.com/pkg/errors"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

type GenesisP2P struct {
	Network *p2pNetwork
}

func (p *GenesisP2P) Broadcast(message *genesisspectypes.SSVMessage) error {

	zap.L().Debug("broadcasting genesis msg", fields.PubKey(message.MsgID.GetPubKey()), zap.Uint64("msg_type", uint64(message.MsgType)))

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

	_, exists := p.Network.nodeStorage.ValidatorStore().Validator(message.MsgID.GetPubKey())
	if !exists {
		return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(message.MsgID.GetPubKey()))
	}

	topics := commons.ValidatorTopicID(message.MsgID.GetPubKey())

	for _, topic := range topics {
		p.Network.interfaceLogger.Debug("broadcasting msg",
			fields.PubKey(message.MsgID.GetPubKey()),
			zap.Uint64("msg_type", uint64(message.MsgType)),
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
