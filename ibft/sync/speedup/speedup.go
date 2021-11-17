package speedup

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	sync2 "github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// Speedup enables a fast syncing with a current running instance from peers by actively fetching their latest change round msgs.
type Speedup struct {
	logger                *zap.Logger
	identifier            []byte
	publicKey             []byte
	seqNumber             uint64
	network               network.Network
	msgValidationPipeline pipeline.Pipeline
}

// New returns a new Speedup instance
func New(
	logger *zap.Logger,
	identifier []byte,
	publicKey []byte,
	seqNumber uint64,
	network network.Network,
	msgValidationPipeline pipeline.Pipeline,
) *Speedup {
	return &Speedup{
		logger:                logger.With(zap.String("sync", "fast_catchup")),
		identifier:            identifier,
		publicKey:             publicKey,
		seqNumber:             seqNumber,
		network:               network,
		msgValidationPipeline: msgValidationPipeline,
	}
}

// Start starts the speedup sync
func (s *Speedup) Start() ([]*proto.SignedMessage, error) {
	usedPeers, err := sync2.GetPeers(s.network, s.publicKey, 4)
	if err != nil {
		return nil, err
	}

	wg := sync.WaitGroup{}
	res := make([]*proto.SignedMessage, 0)
	for _, p := range usedPeers {
		wg.Add(1)
		go func(peer string) {
			msg, err := s.network.GetLastChangeRoundMsg(peer, &network.SyncMessage{
				Type:   network.Sync_GetLatestChangeRound,
				Params: []uint64{s.seqNumber},
				Lambda: s.identifier,
			})
			if err != nil {
				s.logger.Error("error fetching latest change round", zap.Error(err), zap.String("peer", peer))
			} else if err := s.lastMsgError(msg); err != nil {
				if err.Error() != kv.EntryNotFoundError {
					s.logger.Debug("could not fetch latest change round", zap.Error(err), zap.String("peer", peer))
				}
			} else {
				signedMsg := msg.SignedMessages[0]
				if err := s.msgValidationPipeline.Run(signedMsg); err != nil {
					s.logger.Error("invalid change round msg", zap.Error(err))
				} else {
					res = append(res, signedMsg)

					// log
					s.logChangeRoundMsg(signedMsg)
				}
			}
			wg.Done()
		}(p)
	}
	wg.Wait()
	s.logger.Debug("got latest change round msg's from peers", zap.Any("count", len(res)))
	return res, nil
}

func (s *Speedup) lastMsgError(msg *network.SyncMessage) error {
	if msg == nil {
		return errors.New("msg is nil")
	} else if len(msg.Error) > 0 {
		return errors.New(msg.Error)
	} else if len(msg.SignedMessages) != 1 {
		return errors.New("invalid result count")
	}
	return nil
}

func (s *Speedup) logChangeRoundMsg(signedMsg *proto.SignedMessage) {
	for _, signer := range signedMsg.SignerIds {
		s.logger.Debug("received valid change round speedup",
			zap.Uint64("signer_id", signer),
			zap.Uint64("peer_round", signedMsg.Message.Round),
			zap.Uint64("seq_number", signedMsg.Message.SeqNumber),
			zap.String("lambda", hex.EncodeToString(signedMsg.Message.Lambda)),
		)
	}
}
