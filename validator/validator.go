package validator

import (
	"bytes"
	"context"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/beacon/valcheck"
	controller2 "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/operator/forks"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/validator/storage"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
)

// Options to add in validator struct creation
type Options struct {
	Context                    context.Context
	Logger                     *zap.Logger
	Share                      *storage.Share
	SignatureCollectionTimeout time.Duration
	Network                    network.Network
	Beacon                     beacon.Beacon
	ETHNetwork                 *core.Network
	DB                         basedb.IDb
	Fork                       forks.Fork
	Signer                     beacon.Signer
}

// Validator struct that manages all ibft wrappers
type Validator struct {
	ctx                        context.Context
	logger                     *zap.Logger
	Share                      *storage.Share
	ethNetwork                 *core.Network
	beacon                     beacon.Beacon
	ibfts                      map[beacon.RoleType]ibft.Controller
	msgQueue                   *msgqueue.MessageQueue
	network                    network.Network
	signatureCollectionTimeout time.Duration
	valueCheck                 *valcheck.SlashingProtection
	startOnce                  sync.Once
	fork                       forks.Fork
	signer                     beacon.Signer
}

// New Validator creation
func New(opt Options) *Validator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", opt.Share.NodeID))

	msgQueue := msgqueue.New()
	ibfts := make(map[beacon.RoleType]ibft.Controller)
	ibfts[beacon.RoleTypeAttester] = setupIbftController(beacon.RoleTypeAttester, logger, opt.DB, opt.Network, msgQueue, opt.Share, opt.Fork, opt.Signer)
	//ibfts[beacon.RoleAggregator] = setupIbftController(beacon.RoleAggregator, logger, db, opt.Network, msgQueue, opt.Share) TODO not supported for now
	//ibfts[beacon.RoleProposer] = setupIbftController(beacon.RoleProposer, logger, db, opt.Network, msgQueue, opt.Share) TODO not supported for now

	// updating goclient map
	if opt.Share.HasMetadata() && opt.Share.Metadata.Index > 0 {
		blsPubkey := spec.BLSPubKey{}
		copy(blsPubkey[:], opt.Share.PublicKey.Serialize())
		opt.Beacon.ExtendIndexMap(opt.Share.Metadata.Index, blsPubkey)
	}

	return &Validator{
		ctx:                        opt.Context,
		logger:                     logger,
		msgQueue:                   msgQueue,
		Share:                      opt.Share,
		signatureCollectionTimeout: opt.SignatureCollectionTimeout,
		network:                    opt.Network,
		ibfts:                      ibfts,
		ethNetwork:                 opt.ETHNetwork,
		beacon:                     opt.Beacon,
		valueCheck:                 valcheck.New(),
		startOnce:                  sync.Once{},
		fork:                       opt.Fork,
		signer:                     opt.Signer,
	}
}

// Start validator
func (v *Validator) Start() error {
	if err := v.network.SubscribeToValidatorNetwork(v.Share.PublicKey); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	v.startOnce.Do(func() {
		go v.listenToSignatureMessages()

		for _, ib := range v.ibfts { // init all ibfts
			go func(ib ibft.Controller) {
				ReportIBFTStatus(v.Share.PublicKey.SerializeToHexStr(), false, false)
				if err := ib.Init(); err != nil {
					v.logger.Error("could not initialize ibft instance", zap.Error(err))
					ReportIBFTStatus(v.Share.PublicKey.SerializeToHexStr(), false, true)
				} else {
					ReportIBFTStatus(v.Share.PublicKey.SerializeToHexStr(), true, false)
				}
			}(ib)
		}

		v.logger.Debug("validator started")
	})

	return nil
}

func (v *Validator) listenToSignatureMessages() {
	sigChan := v.network.ReceivedSignatureChan()
	for sigMsg := range sigChan {
		if sigMsg == nil {
			v.logger.Debug("got nil message")
			continue
		}

		if sigMsg.Message != nil && v.oneOfIBFTIdentifiers(sigMsg.Message.Lambda) {
			v.msgQueue.AddMessage(&network.Message{
				SignedMessage: sigMsg,
				Type:          network.NetworkMsg_SignatureType,
			})
		}
	}
}

// getSlotStartTime returns the start time for the given slot  TODO: redundant func (in ssvNode) need to fix
func (v *Validator) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(v.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(v.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

func setupIbftController(
	role beacon.RoleType,
	logger *zap.Logger,
	db basedb.IDb,
	network network.Network,
	msgQueue *msgqueue.MessageQueue,
	share *storage.Share,
	fork forks.Fork,
	signer beacon.Signer,
) ibft.Controller {

	ibftStorage := collections.NewIbft(db, logger, role.String())
	identifier := []byte(format.IdentifierFormat(share.PublicKey.Serialize(), role.String()))
	return controller2.New(
		role,
		identifier,
		logger,
		&ibftStorage,
		network,
		msgQueue,
		proto.DefaultConsensusParams(),
		share,
		fork.NewIBFTControllerFork(),
		signer)
}

// oneOfIBFTIdentifiers will return true if provided identifier matches one of the iBFT instances.
func (v *Validator) oneOfIBFTIdentifiers(toMatch []byte) bool {
	for _, i := range v.ibfts {
		if bytes.Equal(i.GetIdentifier(), toMatch) {
			return true
		}
	}
	return false
}
