package validator

import (
	"context"

	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	validatortypes "github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/utils/worker"
)

type Validator interface {
	Start()
	ExecuteDuty(slot uint64, duty *beaconprotocol.Duty)
	ProcessMsg(msg *message.SSVMessage)
	GetShare() *validatortypes.Share
}

type Options struct {
	Context  context.Context
	Logger   *zap.Logger
	Network  p2pprotocol.Network
	Beacon   beaconprotocol.Beacon
	Share    *validatortypes.Share
	ReadMode bool
}

type validator struct {
	ctx     context.Context
	logger  *zap.Logger
	network p2pprotocol.Network
	beacon  beaconprotocol.Beacon
	Share   *validatortypes.Share

	//dutyRunners map[beaconprotocol.RoleType]*DutyRunner
	worker *worker.Worker

	readMode bool
}

func NewValidator(opt *Options) Validator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", opt.Share.NodeID))

	// updating goclient map // TODO move to controller
	//if opt.Share.HasMetadata() && opt.Share.Metadata.Index > 0 {
	//	blsPubkey := spec.BLSPubKey{}
	//	copy(blsPubkey[:], opt.Share.PublicKey.Serialize())
	//	opt.Beacon.ExtendIndexMap(opt.Share.Metadata.Index, blsPubkey)
	//}

	//dutyRunners := setupRunners()
	workerCfg := &worker.WorkerConfig{
		Ctx:          opt.Context,
		WorkersCount: 1,   // TODO flag
		Buffer:       100, // TODO flag
	}
	worker := worker.NewWorker(workerCfg)

	logger.Debug("new validator instance was created", zap.Strings("operators ids", opt.Share.HashOperators()))
	return &validator{
		ctx:     opt.Context,
		logger:  logger,
		network: opt.Network,
		beacon:  opt.Beacon,
		Share:   opt.Share,
		//dutyRunners: dutyRunners,
		worker:   worker,
		readMode: opt.ReadMode,
	}
}

func (v *validator) Start() {
	// start queue workers
	v.worker.AddHandler(messageHandler)
	v.worker.Init()
}

// ProcessMsg processes a new msg, returns true if Decided, non nil byte slice if Decided (Decided value) and error
// Decided returns just once per instance as true, following messages (for example additional commit msgs) will not return Decided true
func (v *validator) ProcessMsg(msg *message.SSVMessage) /*(bool, []byte, error)*/ {
	// check duty type and handle accordingly
	if v.readMode {
		// synchronic process
		return
	}
	v.worker.TryEnqueue(msg)
	// put msg to queue in order t release func
}

func (v *validator) ExecuteDuty(slot uint64, duty *beaconprotocol.Duty) {
	//TODO implement me
	panic("implement me")
}

func (v *validator) GetShare() *validatortypes.Share {
	return v.Share
}

// messageHandler process message from queue,
func messageHandler(msg *message.SignedMessage) {
	// validation
	// check if post consensus
	// of so, process
	// if not, pass to dutyRunner -> QBFT IController
}

// setupRunners return duty runners map with all the supported duty types
//func setupRunners() map[beaconprotocol.RoleType]*DutyRunner {
//	dutyRunners := map[beaconprotocol.RoleType]*DutyRunner{}
//	dutyRunners[beaconprotocol.RoleTypeAttester] = NewDutyRunner()
//	return dutyRunners
//}
