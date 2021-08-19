package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

type IncomingMsgsReader interface {
	Start() error
}

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

// NewIbftIncomingMsgsReader creates  new instance of incomingMsgsReader
func NewIbftIncomingMsgsReader(opts IncomingMsgsReaderOptions) *incomingMsgsReader {
	r := &incomingMsgsReader{
		logger:    opts.Logger.With(zap.String("ibft", "msg_reader")),
		network:   opts.Network,
		config:    opts.Config,
		publicKey: opts.PK,
	}
	return r
}

func (i *incomingMsgsReader) Start() error {
	if err := i.network.SubscribeToValidatorNetwork(i.publicKey); err != nil {
		return errors.Wrap(err, "could not subscribe to subnet")
	}
	if err := i.waitForMinPeers(i.publicKey, 2, false); err != nil {
		return errors.Wrap(err, "could not wait for min peers")
	}
	i.listenToNetwork()
	return nil
}

func (i *incomingMsgsReader) listenToNetwork() {
	msgChan := i.network.ReceivedMsgChan()
	for msg := range msgChan {
		if msg == nil || msg.Message == nil {
			i.logger.Info("received invalid msg")
			continue
		}

		fields := []zap.Field{
			zap.Uint64("seq_num", msg.Message.SeqNumber),
			zap.Uint64("round", msg.Message.Round),
			zap.String("signers", msg.SignersIDString()),
			zap.String("identifier", string(msg.Message.Lambda)),
		}

		switch msg.Message.Type {
		case proto.RoundState_PrePrepare:
			i.logger.Info("pre-prepare msg", fields...)
		case proto.RoundState_Prepare:
			i.logger.Info("prepare msg", fields...)
		case proto.RoundState_Commit:
			i.logger.Info("commit msg", fields...)
		case proto.RoundState_Decided:
			i.logger.Info("decided msg", fields...)
		case proto.RoundState_ChangeRound:
			i.logger.Info("change round msg", fields...)
		default:
			i.logger.Warn("undefined message type", zap.Any("msg", msg))
		}
	}
}

// waitForMinPeers will wait until enough peers joined the topic
// it runs in an exponent interval: 1s > 2s > 4s > ... 64s > 1s > 2s > ...
func (i *incomingMsgsReader) waitForMinPeers(pk *bls.PublicKey, minPeerCount int, stopAtLimit bool) error {
	start := 1 * time.Second
	limit := 64 * time.Second
	interval := start
	for {
		ok, n := i.haveMinPeers(pk, minPeerCount)
		if ok {
			i.logger.Info("found enough peers",
				zap.Int("current peer count", n))
			break
		}
		i.logger.Info("waiting for min peers",
			zap.Int("current peer count", n))

		time.Sleep(interval)

		interval *= 2
		if stopAtLimit && interval == limit {
			return errors.New("could not find peers")
		}
		interval %= limit
		if interval == 0 {
			interval = start
		}
	}
	return nil
}

// haveMinPeers checks that there are at least <count> connected peers
func (i *incomingMsgsReader) haveMinPeers(pk *bls.PublicKey, count int) (bool, int) {
	peers, err := i.network.AllPeers(pk.Serialize())
	if err != nil {
		i.logger.Error("failed fetching peers", zap.Error(err))
		return false, 0
	}
	n := len(peers)
	if len(peers) >= count {
		return true, n
	}
	return false, n
}
