package history

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// Sync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type Sync struct {
	logger              *zap.Logger
	publicKey           []byte
	network             network.Network
	ibftStorage         collections.Iibft
	validateDecidedMsgF func(msg *proto.SignedMessage) error
	identifier          []byte
}

// New returns a new instance of Sync
func New(logger *zap.Logger, publicKey []byte, identifier []byte, network network.Network, ibftStorage collections.Iibft, validateDecidedMsgF func(msg *proto.SignedMessage) error) *Sync {
	return &Sync{
		logger:              logger,
		publicKey:           publicKey,
		identifier:          identifier,
		network:             network,
		validateDecidedMsgF: validateDecidedMsgF,
		ibftStorage:         ibftStorage,
	}
}

// Start the sync
func (s *Sync) Start() error {
	start := time.Now()
	// fetch remote highest
	remoteHighest, fromPeer, err := s.findHighestInstance()
	if err != nil {
		return errors.Wrap(err, "could not fetch highest instance during sync")
	}
	if remoteHighest == nil { // could not find highest, there isn't one
		s.logger.Info("node is synced: could not find any peer with highest decided, assuming sequence number is 0", zap.String("duration", time.Since(start).String()))
		return nil
	}

	// fetch local highest
	localHighest, err := s.ibftStorage.GetHighestDecidedInstance(s.identifier)
	if err != nil && err.Error() != kv.EntryNotFoundError { // if not found, don't continue with sync
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}

	syncStartSeqNumber := uint64(0)
	if localHighest != nil {
		syncStartSeqNumber = localHighest.Message.SeqNumber
	}

	specialStartupCase := err != nil && err.Error() == kv.EntryNotFoundError && remoteHighest.Message.SeqNumber == 0 // in case when remote return seqNum of 0 and local is empty (notFound) we need to save and not assumed to be synced
	if !specialStartupCase {
		// check we are behind and need to sync
		if syncStartSeqNumber >= remoteHighest.Message.SeqNumber {
			s.logger.Info("node is synced", zap.Uint64("highest seq", syncStartSeqNumber), zap.String("duration", time.Since(start).String()))
			return nil
		}
	}

	// fetch, validate and save missing data
	highestSaved, err := s.fetchValidateAndSaveInstances(fromPeer, syncStartSeqNumber, remoteHighest.Message.SeqNumber)
	if err != nil {
		return errors.Wrap(err, "could not fetch decided by range during sync")
	}

	// save highest
	if highestSaved != nil {
		if err := s.ibftStorage.SaveHighestDecidedInstance(highestSaved); err != nil {
			return errors.Wrap(err, "could not save highest decided msg during sync")
		}
	}

	s.logger.Info("finished syncing", zap.Uint64("highest seq", highestSaved.Message.SeqNumber), zap.String("duration", time.Since(start).String()))
	return nil
}
