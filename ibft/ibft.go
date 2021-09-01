package ibft

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"sync"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
)

// StartOptions defines type for IBFT instance options
type StartOptions struct {
	Logger         *zap.Logger
	ValueCheck     valcheck.ValueCheck
	SeqNumber      uint64
	Value          []byte
	ValidatorShare *storage.Share
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
}

// InstanceResult is a struct holding the result of a single iBFT instance
type InstanceResult struct {
	Decided bool
	Msg     *proto.SignedMessage
}

// IBFT represents behavior of the IBFT
type IBFT interface {
	// Init should be called after creating an IBFT instance to init the instance, sync it, etc.
	Init()

	// StartInstance starts a new instance by the given options
	StartInstance(opts StartOptions) (*InstanceResult, error)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() (uint64, error)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[uint64]*proto.Node

	// GetIdentifier returns ibft identifier made of public key and role (type)
	GetIdentifier() []byte
}

// ibftImpl implements IBFT interface
type ibftImpl struct {
	role            beacon.RoleType
	currentInstance *Instance
	logger          *zap.Logger
	ibftStorage     collections.Iibft
	network         network.Network
	msgQueue        *msgqueue.MessageQueue
	instanceConfig  *proto.InstanceConfig
	ValidatorShare  *storage.Share
	Identifier      []byte

	// flags
	initFinished bool

	// locks
	currentInstanceLock sync.Locker
	syncingLock         *semaphore.Weighted
}

// New is the constructor of IBFT
func New(role beacon.RoleType, identifier []byte, logger *zap.Logger, storage collections.Iibft, network network.Network, queue *msgqueue.MessageQueue, instanceConfig *proto.InstanceConfig, ValidatorShare *storage.Share) IBFT {
	logger = logger.With(zap.String("role", role.String()))
	ret := &ibftImpl{
		role:           role,
		ibftStorage:    storage,
		logger:         logger,
		network:        network,
		msgQueue:       queue,
		instanceConfig: instanceConfig,
		ValidatorShare: ValidatorShare,
		Identifier:     identifier,

		// flags
		initFinished: false,

		// locks
		currentInstanceLock: &sync.Mutex{},
		syncingLock:         semaphore.NewWeighted(1),
	}
	return ret
}

// Init sets all major processes of iBFT while blocking until completed.
func (i *ibftImpl) Init() {
	i.logger.Info("iBFT implementation init started")
	i.processDecidedQueueMessages()
	i.processSyncQueueMessages()
	i.listenToSyncMessages()
	i.waitForMinPeerOnInit(2) // minimum of 3 validators (me + 2)
	if err := i.SyncIBFT(); err != nil {
		i.logger.Error("crashing.. ", zap.Error(err))
		return // returning means initFinished is false, can't start new instances
	}
	i.listenToNetworkMessages()
	i.listenToNetworkDecidedMessages()
	i.initFinished = true
	i.logger.Info("iBFT implementation init finished")
}

func (i *ibftImpl) StartInstance(opts StartOptions) (*InstanceResult, error) {
	instanceOpts, err := i.instanceOptionsFromStartOptions(opts)
	if err != nil {
		return nil, errors.WithMessage(err, "can't generate instance options")
	}

	if err := i.canStartNewInstance(*instanceOpts); err != nil {
		return nil, errors.WithMessage(err, "can't start new iBFT instance")
	}

	return i.startInstanceWithOptions(instanceOpts, opts.Value)
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (i *ibftImpl) GetIBFTCommittee() map[uint64]*proto.Node {
	return i.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (i *ibftImpl) GetIdentifier() []byte {
	return i.Identifier //TODO should use mutex to lock var?
}
