package networkhandler

import (
	"context"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/validator/router"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

const networkRouterConcurrency = 2048

type NonCommitteeMessageHandler interface {
	HandleNonCommitteeValidatorMessage(msg *queue.DecodedSSVMessage) error
}

type NetworkHandler interface {
	StartNetworkHandlers()
}

type networkHandler struct {
	context                    context.Context
	logger                     *zap.Logger
	p2pNetwork                 network.P2PNetwork
	messageRouter              *router.MessageRouter
	messageWorker              *worker.Worker
	nonCommitteeMessageHandler NonCommitteeMessageHandler
	validatorsMap              *validatorsmap.ValidatorsMap
	exporter                   bool
}

func NewNetworkHandler(
	ctx context.Context,
	logger *zap.Logger,
	p2pNetwork network.P2PNetwork,
	nonCommitteeMessageHandler NonCommitteeMessageHandler,
	validatorsMap *validatorsmap.ValidatorsMap,
	exporter bool,
	workersCount int,
	queueBufferSize int,
) NetworkHandler {
	workerCfg := &worker.Config{
		Ctx:          ctx,
		WorkersCount: workersCount,
		Buffer:       queueBufferSize,
	}

	return &networkHandler{
		context:                    ctx,
		logger:                     logger,
		p2pNetwork:                 p2pNetwork,
		messageRouter:              router.NewMessageRouter(logger),
		messageWorker:              worker.NewWorker(logger, workerCfg),
		nonCommitteeMessageHandler: nonCommitteeMessageHandler,
		validatorsMap:              validatorsMap,
		exporter:                   exporter,
	}
}

// setupNetworkHandlers registers all the required handlers for sync protocols
func (nh *networkHandler) setupNetworkHandlers() error {
	syncHandlers := []*p2pprotocol.SyncHandler{}
	nh.logger.Debug("setting up network handlers",
		zap.Int("count", len(syncHandlers)),
		zap.Bool("exporter", nh.exporter))
	nh.p2pNetwork.RegisterHandlers(nh.logger, syncHandlers...)
	return nil
}

func (nh *networkHandler) handleRouterMessages() {
	ctx, cancel := context.WithCancel(nh.context)
	defer cancel()

	ch := nh.messageRouter.Messages()

	for {
		select {
		case <-ctx.Done():
			nh.logger.Debug("router message handler stopped")
			return
		case msg := <-ch:
			// TODO temp solution to prevent getting event msgs from network. need to to add validation in p2p
			if msg.MsgType == message.SSVEventMsgType {
				continue
			}

			pk := msg.GetID().GetPubKey()
			if v, ok := nh.validatorsMap.GetValidator(pk); ok {
				v.HandleMessage(nh.logger, msg)
			} else if nh.exporter {
				if msg.MsgType != spectypes.SSVConsensusMsgType {
					continue // not supporting other types
				}
				if !nh.messageWorker.TryEnqueue(msg) { // start to save non committee decided messages only post fork
					nh.logger.Warn("Failed to enqueue post consensus message: buffer is full")
				}
			}
		}
	}
}

// StartNetworkHandlers init msg worker that handles network messages
func (nh *networkHandler) StartNetworkHandlers() {
	if err := nh.setupNetworkHandlers(); err != nil {
		nh.logger.Panic("could not register stream handlers", zap.Error(err))
	}

	nh.p2pNetwork.UseMessageRouter(nh.messageRouter)

	for i := 0; i < networkRouterConcurrency; i++ {
		go nh.handleRouterMessages()
	}

	nh.messageWorker.UseHandler(nh.nonCommitteeMessageHandler.HandleNonCommitteeValidatorMessage)
}
