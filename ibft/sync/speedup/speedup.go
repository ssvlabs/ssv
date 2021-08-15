package speedup

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

// Speedup enables a fast syncing with a current running instance from peers by actively fetching their latest change round msgs.
type Speedup struct {
	logger                *zap.Logger
	identifier            []byte
	network               network.Network
	msgValidationPipeline pipeline.Pipeline
}

func New(
	logger *zap.Logger,
	identifier []byte,
	network network.Network,
	msgValidationPipeline pipeline.Pipeline,
) *Speedup {
	return &Speedup{
		logger:                logger.With(zap.String("sync", "fast_catchup")),
		identifier:            identifier,
		network:               network,
		msgValidationPipeline: msgValidationPipeline,
	}
}

func (s *Speedup) Start() ([]*proto.SignedMessage, error) {
	return nil, nil
}
