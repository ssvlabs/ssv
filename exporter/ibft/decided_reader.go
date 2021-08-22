package ibft

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"strings"
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
}

// decidedReader reads decided messages history
type decidedReader struct {
	logger  *zap.Logger
	storage collections.Iibft
	network network.Network

	config         *proto.InstanceConfig
	validatorShare *storage.Share
}

// NewIbftDecidedReadOnly creates  new instance of DecidedReader
func NewIbftDecidedReadOnly(opts DecidedReaderOptions) Reader {
	r := decidedReader{
		logger:         opts.Logger.With(
			zap.String("pubKey", opts.ValidatorShare.PublicKey.SerializeToHexStr()),
			zap.String("ibft", "decided_reader")),
		storage:        opts.Storage,
		network:        opts.Network,
		config:         opts.Config,
		validatorShare: opts.ValidatorShare,
	}
	return &r
}

// Start starts to fetch best known decided message (highest sequence) from the network and sync to it.
func (r *decidedReader) Start() error {
	// subscribe to topic so we could find relevant nodes
	if err := r.network.SubscribeToValidatorNetwork(r.validatorShare.PublicKey); err != nil {
		if !strings.Contains(err.Error(), "topic already exists") {
			r.logger.Warn("could not subscribe to validator channel", zap.Error(err))
			return err
		}
		r.logger.Debug("no need to subscribe, topic already exist")
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
	identifier := []byte(validator.IdentifierFormat(r.validatorShare.PublicKey.Serialize(), beacon.RoleTypeAttester))
	hs := history.New(r.logger, r.validatorShare.PublicKey.Serialize(), identifier, r.network,
		r.storage, r.validateDecidedMsg)
	err := hs.Start()
	if err != nil {
		r.logger.Error("could not sync validator's data", zap.Error(err))
	}
	return err
}

// validateDecidedMsg validates the message
func (r *decidedReader) validateDecidedMsg(msg *proto.SignedMessage) error {
	r.logger.Debug("validating a new decided message", zap.String("msg", msg.String()))
	p := pipeline.Combine(
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.AuthorizeMsg(r.validatorShare),
		auth.ValidateQuorum(r.validatorShare.ThresholdSize()),
	)
	return p.Run(msg)
}
