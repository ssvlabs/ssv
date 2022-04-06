package change_round

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ChangeRoundFetcher is responsible for fetching change round messages from other peers in the subnet
type ChangeRoundFetcher interface {
	// GetChangeRoundMessages fetches change round messages for the given identifier and height
	GetChangeRoundMessages(identifier message.Identifier, height message.Height) ([]*message.SignedMessage, error)
}

// changeRoundFetcher implements ChangeRoundFetcher
type changeRoundFetcher struct {
	logger   *zap.Logger
	syncer   p2pprotocol.Syncer
	validate validation.SignedMessagePipeline
}

// NewLastRoundFetcher returns an instance of changeRoundFetcher
func NewLastRoundFetcher(logger *zap.Logger, syncer p2pprotocol.Syncer, validate validation.SignedMessagePipeline) ChangeRoundFetcher {
	return &changeRoundFetcher{
		logger:   logger,
		syncer:   syncer,
		validate: validate,
	}
}

func (crf *changeRoundFetcher) GetChangeRoundMessages(identifier message.Identifier, height message.Height) ([]*message.SignedMessage, error) {
	msgs, err := crf.syncer.LastChangeRound(identifier, height)
	if err != nil {
		return nil, errors.Wrap(err, "could not get change round messages")
	}
	results := make([]*message.SignedMessage, 0)
	// TODO: report bad/invalid messages
	for _, msg := range msgs {
		syncMsg := &message.SyncMessage{}
		err = syncMsg.Decode(msg.Msg.Data)
		if err != nil {
			crf.logger.Warn("could not decode change round message", zap.Error(err))
			continue
		}
		err = crf.msgError(syncMsg)
		if err != nil {
			crf.logger.Warn("change round api error", zap.Error(err))
			continue
		}
		sm := syncMsg.Data[0]
		err = crf.validate.Run(sm)
		if err != nil {
			crf.logger.Warn("could not validate message", zap.Error(err))
			continue
		}
		results = append(results, sm)
	}
	return results, nil
}

func (crf *changeRoundFetcher) msgError(msg *message.SyncMessage) error {
	if msg == nil {
		return errors.New("msg is nil")
	} else if msg.Status != message.StatusSuccess {
		return errors.Errorf("failed with status %d", msg.Status)
	} else if len(msg.Data) != 1 { // TODO: extract to validation
		return errors.New("invalid result count")
	}
	return nil
}
