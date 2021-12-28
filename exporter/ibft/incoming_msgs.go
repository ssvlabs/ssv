package ibft

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/utils/format"
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
	if err := i.network.SubscribeToValidatorNetwork(i.publicKey); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	if err := i.waitForMinPeers(ctx, i.publicKey, 1); err != nil {
		return errors.Wrap(err, "could not wait for min peers")
	}
	return nil
}

func (i *incomingMsgsReader) GetMsgResolver(networkMsg network.NetworkMsg) func(msg *proto.SignedMessage) {
	switch networkMsg {
	case network.NetworkMsg_IBFTType:
		i.logger.Debug("return network resolver")
		return i.onMessage
	}
	return func(msg *proto.SignedMessage) {
		i.logger.Warn(fmt.Sprintf("handler type (%s) is not supported", networkMsg))
	}
}

func (i *incomingMsgsReader) onMessage(msg *proto.SignedMessage) {
	identifier := format.IdentifierFormat(i.publicKey.Serialize(), beacon.RoleTypeAttester.String())
	i.logger.Debug("got network msg")
	if msg == nil || msg.Message == nil {
		i.logger.Info("received invalid msg")
		return
	}
	// filtering irrelevant messages
	// TODO: handle other types of roles
	if identifier != string(msg.Message.Lambda) {
		return
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

// waitForMinPeers will wait until enough peers joined the topic
func (i *incomingMsgsReader) waitForMinPeers(ctx context.Context, pk *bls.PublicKey, minPeerCount int) error {
	return commons.WaitForMinPeers(commons.WaitMinPeersCtx{
		Ctx:    ctx,
		Logger: i.logger,
		Net:    i.network,
	}, pk.Serialize(), minPeerCount, 1*time.Second, 64*time.Second, false)
}

func messageFields(msg *proto.SignedMessage) []zap.Field {
	return []zap.Field{
		zap.Uint64("seq_num", msg.Message.SeqNumber),
		zap.Uint64("round", msg.Message.Round),
		zap.String("signers", msg.SignersIDString()),
		zap.ByteString("sig", msg.Signature),
		zap.String("identifier", string(msg.Message.Lambda)),
	}
}
