package ibft

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// IncomingMsgsReaderOptions defines the required parameters to create an instance
type IncomingMsgsReaderOptions struct {
	Logger  *zap.Logger
	Network network.Network
	Config  *proto.InstanceConfig
	PK      *bls.PublicKey
}

type incomingMsgsReader struct {
	logger    *zap.Logger
	network   network.Network
	config    *proto.InstanceConfig
	publicKey *bls.PublicKey
}

// newIncomingMsgsReader creates new instance
func newIncomingMsgsReader(opts IncomingMsgsReaderOptions) Reader {
	r := &incomingMsgsReader{
		logger: opts.Logger.With(zap.String("ibft", "msg_reader"),
			zap.String("pubKey", opts.PK.SerializeToHexStr())),
		network:   opts.Network,
		config:    opts.Config,
		publicKey: opts.PK,
	}
	return r
}

func (i *incomingMsgsReader) Start() error {
	if !i.network.IsSubscribeToValidatorNetwork(i.publicKey) {
		if err := i.network.SubscribeToValidatorNetwork(i.publicKey); err != nil {
			return errors.Wrap(err, "failed to subscribe topic")
		}
	}

	if err := i.waitForMinPeers(i.publicKey, 2); err != nil {
		return errors.Wrap(err, "could not wait for min peers")
	}
	i.listenToNetwork()
	return nil
}

func (i *incomingMsgsReader) listenToNetwork() {
	msgChan := i.network.ReceivedMsgChan()
	identifier := validator.IdentifierFormat(i.publicKey.Serialize(), beacon.RoleTypeAttester)
	i.logger.Debug("listening to network messages")
	for msg := range msgChan {
		if msg == nil || msg.Message == nil {
			i.logger.Info("received invalid msg")
			continue
		}
		// filtering irrelevant messages
		// TODO: handle other types of roles
		if identifier != string(msg.Message.Lambda) {
			continue
		}

		fields := messageFields(msg)

		switch msg.Message.Type {
		case proto.RoundState_PrePrepare:
			i.logger.Info("pre-prepare msg", fields...)
		case proto.RoundState_Prepare:
			i.logger.Info("prepare msg", fields...)
		case proto.RoundState_Commit:
			i.logger.Info("commit msg", fields...)
		case proto.RoundState_ChangeRound:
			i.logger.Info("change round msg", fields...)
		default:
			i.logger.Warn("undefined message type", zap.Any("msg", msg))
		}
	}
}

// waitForMinPeers will wait until enough peers joined the topic
func (i *incomingMsgsReader) waitForMinPeers(pk *bls.PublicKey, minPeerCount int) error {
	ctx := commons.WaitMinPeersCtx{
		Ctx:    context.Background(),
		Logger: i.logger,
		Net:    i.network,
	}
	return commons.WaitForMinPeers(ctx, pk.Serialize(), minPeerCount,
		1*time.Second, 64*time.Second, false)
}

func messageFields(msg *proto.SignedMessage) []zap.Field {
	return []zap.Field{
		zap.Uint64("seq_num", msg.Message.SeqNumber),
		zap.Uint64("round", msg.Message.Round),
		zap.String("signers", msg.SignersIDString()),
		zap.String("identifier", string(msg.Message.Lambda)),
	}
}
