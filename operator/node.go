package operator

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/exporter/api"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/operator/fee_recipient"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
)

// Node represents the behavior of SSV node
type Node interface {
	Start(logger *zap.Logger) error
	StartEth1(logger *zap.Logger, syncOffset *eth1.SyncOffset) error
}

// Options contains options to create the node
type Options struct {
	// NetworkName is the network name of this node
	NetworkName         string `yaml:"Network" env:"NETWORK" env-default:"mainnet" env-description:"Network is the network of this node"`
	Network             networkconfig.NetworkConfig
	BeaconNode          beaconprotocol.BeaconNode
	P2PNetwork          network.P2PNetwork
	Context             context.Context
	Eth1Client          eth1.Client
	DB                  basedb.IDb
	ValidatorController validator.Controller
	DutyExec            duties.DutyExecutor
	// max slots for duty to wait
	DutyLimit        uint64                      `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	ValidatorOptions validator.ControllerOptions `yaml:"ValidatorOptions"`

	ForkVersion forksprotocol.ForkVersion

	WS        api.WebSocketServer
	WsAPIPort int
}

// operatorNode implements Node interface
type operatorNode struct {
	network          networkconfig.NetworkConfig
	context          context.Context
	ticker           slot_ticker.Ticker
	validatorsCtrl   validator.Controller
	beacon           beaconprotocol.BeaconNode
	net              network.P2PNetwork
	storage          storage.Storage
	qbftStorage      *qbftstorage.QBFTStores
	eth1Client       eth1.Client
	dutyCtrl         duties.DutyController
	feeRecipientCtrl fee_recipient.RecipientController
	// fork           *forks.Forker

	forkVersion forksprotocol.ForkVersion

	ws        api.WebSocketServer
	wsAPIPort int
}

// New is the constructor of operatorNode
func New(logger *zap.Logger, opts Options, slotTicker slot_ticker.Ticker) Node {
	storageMap := qbftstorage.NewStores()

	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRoleValidatorRegistration,
	}
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(opts.DB, role.String(), opts.ForkVersion))
	}

	node := &operatorNode{
		context:        opts.Context,
		ticker:         slotTicker,
		validatorsCtrl: opts.ValidatorController,
		network:        opts.Network,
		beacon:         opts.BeaconNode,
		net:            opts.P2PNetwork,
		eth1Client:     opts.Eth1Client,
		storage:        opts.ValidatorOptions.RegistryStorage,
		qbftStorage:    storageMap,
		dutyCtrl: duties.NewDutyController(logger, &duties.ControllerOptions{
			Ctx:                 opts.Context,
			BeaconClient:        opts.BeaconNode,
			Network:             opts.Network,
			ValidatorController: opts.ValidatorController,
			DutyLimit:           opts.DutyLimit,
			Executor:            opts.DutyExec,
			ForkVersion:         opts.ForkVersion,
			Ticker:              slotTicker,
			BuilderProposals:    opts.ValidatorOptions.BuilderProposals,
		}),
		feeRecipientCtrl: fee_recipient.NewController(&fee_recipient.ControllerOptions{
			Ctx:              opts.Context,
			BeaconClient:     opts.BeaconNode,
			Network:          opts.Network,
			ShareStorage:     opts.ValidatorOptions.RegistryStorage.Shares(),
			RecipientStorage: opts.ValidatorOptions.RegistryStorage,
			Ticker:           slotTicker,
			OperatorData:     opts.ValidatorOptions.OperatorData,
		}),
		forkVersion: opts.ForkVersion,

		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	return node
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start(logger *zap.Logger) error {
	logger.Named(logging.NameOperator)

	logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer(logger)
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	// slot ticker init
	go n.ticker.Start(logger)

	n.validatorsCtrl.StartNetworkHandlers(logger)
	n.validatorsCtrl.StartValidators(logger)
	go n.net.UpdateSubnets(logger)
	go n.validatorsCtrl.UpdateValidatorMetaDataLoop(logger)
	go n.listenForCurrentSlot(logger)
	go n.reportOperators(logger)

	go func() {
		time.Sleep(time.Minute)
		pks := strings.Split(`ee4ee10bddf8cf3d31e43cebc3a54de86ea2222ba5de9ddff36c283b8118a87a2fa886698d47965214e44cb9ea1845ce,a87e23bafa49de0ff2f4174534db434a57612d35d3e4e19d7ee6e54feb691bfca0a768eeeebad7cc40af9c455f823bd8,28d4e451a6b351ed0d183a0f01795bcc4bcd10d7a6466d3e450dec4ce8d2a8f7fb0fa492fd4ec03db28e73a980db0928,fad74bc2e04507e9014b85c3af09276339c8c3a9c7652fd1a4b1250db94e938552de1030e4d76b37e2fcda006ce4ac8e,df128c30994df64cff7dd163e782b17f3057154916407689572f6f948a5629fff4d4fce8b5011cd2bf95fafad37a09e3,1ae2ecd3111555e4df15d6c6908f835a9882db033d18a4d1dea67bc0885ff165d5825277726a8c78670bba3c65b52389,3bcab5b07171181db75067bfa3fe1d1a0b0bd1d6f1acb213d114018805152b47e01def7448a2b42d73dc4f1254a623e4,a25e2750a7303886f786d0a2f4c9fb4207974b596c75c6042343c684feebb9da13f521f1e5e5b61ec0efe2524e980126,ce651001d8c82e2ffd1033d61b4953f71ffac532582cf66f43dddc6d662c1343c01edd1415fad4ee2ed122af1adb1fcf,05a075ead9e86bdfd82dda4784364252c947dc70487ebb8de4cb6d3a3c787ece5859df3457104dcabd042efac91b24bb,2b666a48f923bca6d2797d11dd55a9117125ae5e8f7bbd3b5a9d208cae4bbbd66be34a47ebd9fe6793c828c305b249f5,0e5f81f6da277f77134eaea0cbc25bc2681fa3ba091bbb4417b21aa32d738e937165b6910fa3a3601b34296f374c39ec,fa3953122b8a313b83dc4418f71683c95c17c30adc894d4126af5be31232a04360c19cb010d924646d17b4b74945926a,e2412e80275a028219dcb6dfb65c882482e0d34af6fda72237f9fb9c6002c1d57da01823677e16f7c85522f6d2b1ca69,ff1de1cb866ecab95cb4112bf9fef6e775b33c4ac4366220925674620043f97516f07fe706c8ed2a293e556231255e4b,81c3d6c86d8db25fc303fff641716588b3dd8b05e2157938a408e2176caf8e625bd541a10aad1b60871c9121a91ad692`, ",")
		for _, s := range pks {
			randomPK, err := hex.DecodeString(s)
			if len(randomPK) != 48 {
				logger.Error("rvrt: failed to generate random pk", zap.Int("n", len(randomPK)))
				return
			}
			err = n.net.Subscribe(randomPK)
			if err != nil {
				logger.Error("rvrt: failed to subscribe to random pk", zap.Error(err))
				return
			}
			logger.Debug("rvrt: subscribed to validator", fields.PubKey(randomPK))
		}
	}()

	go n.feeRecipientCtrl.Start(logger)
	n.dutyCtrl.Start(logger)

	return nil
}

// listenForCurrentSlot listens to current slot and trigger relevant components if needed
func (n *operatorNode) listenForCurrentSlot(logger *zap.Logger) {
	tickerChan := make(chan phase0.Slot, 32)
	n.ticker.Subscribe(tickerChan)
	for slot := range tickerChan {
		n.setFork(logger, slot)
	}
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1(logger *zap.Logger, syncOffset *eth1.SyncOffset) error {
	logger.Info("starting operator node syncing with eth1")

	handler := n.validatorsCtrl.Eth1EventHandler(logger, false)
	// sync past events
	if err := eth1.SyncEth1Events(logger, n.eth1Client, n.storage, n.network, syncOffset, handler); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	logger.Info("manage to sync contract events")
	shares := n.storage.Shares().List()
	operators, err := n.storage.ListOperators(logger, 0, 0)
	if err != nil {
		logger.Error("failed to get operators", zap.Error(err))
	}
	operatorID := n.validatorsCtrl.GetOperatorData().ID
	operatorValidatorsCount := 0
	if operatorID != 0 {
		for _, share := range shares {
			if share.BelongsToOperator(operatorID) {
				operatorValidatorsCount++
			}
		}
	}

	logger.Info("ETH1 sync history stats",
		zap.Int("validators count", len(shares)),
		zap.Int("operators count", len(operators)),
		zap.Int("my validators count", operatorValidatorsCount),
	)

	// setup validator controller to listen to new events
	go n.validatorsCtrl.ListenToEth1Events(logger, n.eth1Client.EventsFeed())

	// starts the eth1 events subscription
	if err := n.eth1Client.Start(logger); err != nil {
		return errors.Wrap(err, "failed to start eth1 client")
	}

	return nil
}

// HealthCheck returns a list of issues regards the state of the operator node
func (n *operatorNode) HealthCheck() []string {
	errs := metrics.ProcessAgents(n.healthAgents())
	metrics.ReportSSVNodeHealthiness(len(errs) == 0)
	return errs
}

func (n *operatorNode) healthAgents() []metrics.HealthCheckAgent {
	var agents []metrics.HealthCheckAgent
	if agent, ok := n.eth1Client.(metrics.HealthCheckAgent); ok {
		agents = append(agents, agent)
	}
	if agent, ok := n.beacon.(metrics.HealthCheckAgent); ok {
		agents = append(agents, agent)
	}
	return agents
}

// handleQueryRequests waits for incoming messages and
func (n *operatorNode) handleQueryRequests(logger *zap.Logger, nm *api.NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
	}
	logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))
	switch nm.Msg.Type {
	case api.TypeDecided:
		api.HandleDecidedQuery(logger, n.qbftStorage, nm)
	case api.TypeError:
		api.HandleErrorQuery(logger, nm)
	default:
		api.HandleUnknownQuery(logger, nm)
	}
}

func (n *operatorNode) startWSServer(logger *zap.Logger) error {
	if n.ws != nil {
		logger.Info("starting WS server")

		n.ws.UseQueryHandler(n.handleQueryRequests)

		if err := n.ws.Start(logger, fmt.Sprintf(":%d", n.wsAPIPort)); err != nil {
			return err
		}
	}

	return nil
}

func (n *operatorNode) reportOperators(logger *zap.Logger) {
	operators, err := n.storage.ListOperators(logger, 0, 1000) // TODO more than 1000?
	if err != nil {
		logger.Warn("failed to get all operators for reporting", zap.Error(err))
		return
	}
	logger.Debug("reporting operators", zap.Int("count", len(operators)))
	for i := range operators {
		exporter.ReportOperatorIndex(logger, &operators[i])
	}
}
