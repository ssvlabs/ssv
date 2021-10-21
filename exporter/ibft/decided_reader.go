package ibft

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

// DecidedReaderOptions defines the required parameters to create an instance
type DecidedReaderOptions struct {
	Logger         *zap.Logger
	Storage        collections.Iibft
	Network        network.Network
	Config         *proto.InstanceConfig
	ValidatorShare *storage.Share

	Out pubsub.Publisher
}

// decidedReader reads decided messages history
type decidedReader struct {
	logger  *zap.Logger
	storage collections.Iibft
	network network.Network

	config         *proto.InstanceConfig
	validatorShare *storage.Share

	out pubsub.Publisher

	identifier []byte
}

// newDecidedReader creates new instance of DecidedReader
func newDecidedReader(opts DecidedReaderOptions) SyncRead {
	r := decidedReader{
		logger: opts.Logger.With(
			zap.String("pubKey", opts.ValidatorShare.PublicKey.SerializeToHexStr()),
			zap.String("ibft", "decided_reader")),
		storage:        opts.Storage,
		network:        opts.Network,
		config:         opts.Config,
		validatorShare: opts.ValidatorShare,
		out:            opts.Out,
		identifier: []byte(format.IdentifierFormat(opts.ValidatorShare.PublicKey.Serialize(),
			beacon.RoleTypeAttester.String())),
	}
	return &r
}

// Sync starts to fetch best known decided message (highest sequence) from the network and sync to it.
func (r *decidedReader) Sync() error {
	if err := r.network.SubscribeToValidatorNetwork(r.validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}
	// wait for network setup (subscribe to topic)
	var netWaitGroup sync.WaitGroup
	netWaitGroup.Add(1)
	go func() {
		defer netWaitGroup.Done()
		time.Sleep(1 * time.Second)
	}()
	netWaitGroup.Wait()

	r.logger.Debug("syncing ibft data")
	// creating HistorySync and starts it
	hs := history.New(r.logger, r.validatorShare.PublicKey.Serialize(), r.identifier, r.network,
		r.storage, r.validateDecidedMsg)
	err := hs.Start()
	if err != nil {
		r.logger.Error("could not sync validator's data", zap.Error(err))
	}
	return err
}

// Start starts to listen to decided messages
func (r *decidedReader) Start() error {
	if err := r.network.SubscribeToValidatorNetwork(r.validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	r.logger.Debug("starting to read decided messages")
	if err := r.waitForMinPeers(r.validatorShare.PublicKey, 2); err != nil {
		return errors.Wrap(err, "could not wait for min peers")
	}
	r.listenToNetwork(r.network.ReceivedDecidedChan())
	return nil
}

func (r *decidedReader) listenToNetwork(cn <-chan *proto.SignedMessage) {
	r.logger.Debug("listening to decided messages")
	for msg := range cn {
		if err := validateMsg(msg, string(r.identifier)); err != nil {
			continue
		}
		logger := r.logger.With(messageFields(msg)...)
		if err := validateDecidedMsg(msg, r.validatorShare); err != nil {
			logger.Debug("received invalid decided message")
			continue
		}
		if msg.Message.SeqNumber == 0 {
			logger.Debug("received invalid sequence")
			continue
		}
		if saved, err := r.handleNewDecidedMessage(msg); err != nil {
			logger.Error("could not handle decided message")
			continue
		} else if saved {
			go r.out.Notify(newDecidedNetworkMsg(msg, r.validatorShare.PublicKey.SerializeToHexStr()))
		}
	}
}

// handleNewDecidedMessage saves an incoming (valid) decided message
func (r *decidedReader) handleNewDecidedMessage(msg *proto.SignedMessage) (bool, error) {
	logger := r.logger.With(messageFields(msg)...)
	if decided, found, _ := r.storage.GetDecided(r.identifier, msg.Message.SeqNumber); found && decided != nil {
		logger.Debug("received known sequence")
		return false, nil
	}
	if err := r.storage.SaveDecided(msg); err != nil {
		return false, errors.Wrap(err, "could not save decided")
	}
	logger.Debug("decided saved")
	ibft.ReportDecided(r.validatorShare.PublicKey.SerializeToHexStr(), msg)
	return true, r.checkHighestDecided(msg)
}

// checkHighestDecided check if highest decided should be updated
func (r *decidedReader) checkHighestDecided(msg *proto.SignedMessage) error {
	logger := r.logger.With(messageFields(msg)...)
	seq := msg.Message.SeqNumber
	highestKnown, found, err := r.storage.GetHighestDecidedInstance(r.identifier)
	if err != nil {
		return errors.Wrap(err, "could not get highest decided")
	}
	if found {
		highestSeqKnown := uint64(0)
		if highestKnown != nil {
			highestSeqKnown = highestKnown.Message.SeqNumber
		}
		if seq < highestSeqKnown {
			logger.Debug("received old sequence",
				zap.Uint64("highestSeqKnown", highestSeqKnown))
			return nil
		}
		if seq > highestSeqKnown+1 {
			if err := r.Sync(); err != nil {
				logger.Debug("could not sync", zap.Uint64("seq", seq),
					zap.Uint64("highestSeqKnown", highestSeqKnown))
				return err
			}
			return nil
		}
	}
	if err := r.storage.SaveHighestDecidedInstance(msg); err != nil {
		return errors.Wrap(err, "could not save highest decided")
	}
	logger.Info("highest decided saved")
	return nil
}

// validateDecidedMsg validates the message
func (r *decidedReader) validateDecidedMsg(msg *proto.SignedMessage) error {
	r.logger.Debug("validating a new decided message", zap.String("msg", msg.String()))
	return validateDecidedMsg(msg, r.validatorShare)
}

// waitForMinPeers will wait until enough peers joined the topic
func (r *decidedReader) waitForMinPeers(pk *bls.PublicKey, minPeerCount int) error {
	ctx := commons.WaitMinPeersCtx{
		Ctx:    context.Background(),
		Logger: r.logger,
		Net:    r.network,
	}
	return commons.WaitForMinPeers(ctx, pk.Serialize(), minPeerCount,
		1*time.Second, 64*time.Second, false)
}

func validateDecidedMsg(msg *proto.SignedMessage, share *storage.Share) error {
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.AuthorizeMsg(share),
		auth.ValidateQuorum(share.ThresholdSize()),
	)
	return p.Run(msg)
}

func validateMsg(msg *proto.SignedMessage, identifier string) error {
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.ValidateLambdas([]byte(identifier)),
	)
	return p.Run(msg)
}

func newDecidedNetworkMsg(msg *proto.SignedMessage, pk string) api.NetworkMessage {
	return api.NetworkMessage{Msg: api.Message{
		Type: api.TypeDecided,
		Filter: api.MessageFilter{
			PublicKey: pk,
			From:      int64(msg.Message.SeqNumber), To: int64(msg.Message.SeqNumber),
			Role: api.RoleAttester},
		Data: []*proto.SignedMessage{msg},
	}, Conn: nil}
}
