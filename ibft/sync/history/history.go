package history

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// Syncer is the interface for history sync
type Syncer interface {
	Start() error
	StartRange(from, to uint64) (int, error)
}

// Sync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type Sync struct {
	logger              *zap.Logger
	publicKey           []byte
	network             network.Network
	ibftStorage         collections.Iibft
	validateDecidedMsgF func(msg *proto.SignedMessage) error
	identifier          []byte
	// paginationMaxSize is the max number of returned elements in a single response
	paginationMaxSize uint64
	committeeSize     int
}

// New returns a new instance of Sync
func New(logger *zap.Logger, publicKey []byte, committeeSize int, identifier []byte, network network.Network, ibftStorage collections.Iibft, validateDecidedMsgF func(msg *proto.SignedMessage) error) *Sync {
	return &Sync{
		logger:              logger.With(zap.String("sync", "history")),
		publicKey:           publicKey,
		identifier:          identifier,
		network:             network,
		validateDecidedMsgF: validateDecidedMsgF,
		ibftStorage:         ibftStorage,
		paginationMaxSize:   network.MaxBatch(),
		committeeSize:       committeeSize,
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
	localHighest, found, err := s.ibftStorage.GetHighestDecidedInstance(s.identifier)
	if err != nil { // if not found, don't continue with sync
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}

	syncStartSeqNumber := uint64(0)
	if localHighest != nil {
		syncStartSeqNumber = localHighest.Message.SeqNumber
	}

	specialStartupCase := !found && remoteHighest.Message.SeqNumber == 0 // in case when remote return seqNum of 0 and local is empty (notFound) we need to save and not assumed to be synced
	if !specialStartupCase {
		// check we are behind and need to sync
		if syncStartSeqNumber >= remoteHighest.Message.SeqNumber {
			s.logger.Info("node is synced", zap.Uint64("highest seq", syncStartSeqNumber), zap.String("duration", time.Since(start).String()))
			return nil
		}
	}

	// fetch, validate and save missing data
	highestSaved, _, err := s.fetchValidateAndSaveInstances(fromPeer, syncStartSeqNumber, remoteHighest.Message.SeqNumber)
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

// StartRange starts to sync old messages in a specific range
// first it tries to find a synced peer and then ask a specific range of decided messages
func (s *Sync) StartRange(from, to uint64) (int, error) {
	var n int
	start := time.Now()
	// fetch remote highest
	remoteHighest, fromPeer, err := s.findHighestInstance()
	if err != nil {
		return n, errors.Wrap(err, "could not fetch highest instance during sync")
	}
	if remoteHighest == nil { // could not find highest, there isn't one
		s.logger.Info("node is synced: could not find any peer with highest decided, assuming sequence number is 0",
			zap.String("duration", time.Since(start).String()))
		return n, nil
	}
	if remoteHighest.Message.SeqNumber < from {
		return n, errors.New("range is out of decided sequence boundaries")
	}
	// fetch, validate and save missing data
	_, n, err = s.fetchValidateAndSaveInstances(fromPeer, from, to)
	if err != nil {
		return n, errors.Wrap(err, "could not fetch decided by range during sync")
	}
	s.logger.Info("finished syncing in range", zap.Uint64("from", from), zap.Uint64("to", to),
		zap.String("duration", time.Since(start).String()), zap.Int("items", n))
	return n, nil
}
