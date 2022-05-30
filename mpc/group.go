package mpc

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/operator/forks"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

// Group represents a group operators / mpc players to generate key, perform adhoc signing,
// hence it has similiar structure to Validator, in future it may be merged into Validator,
// but for now it's kept separate.
// it holds the corresponding ibft controllers to trigger consensus layer
type Group struct {
	ctx                        context.Context
	logger                     *zap.Logger
	Share                      *storage.Share
	ethNetwork                 *core.Network
	beacon                     beacon.Beacon
	ibfts                      map[beacon.RoleType]ibft.Controller
	msgQueue                   *msgqueue.MessageQueue
	network                    network.Network
	signatureCollectionTimeout time.Duration
	startOnce                  sync.Once
	fork                       forks.Fork // TODO: Do we need it?
	signer                     beacon.Signer
}

// Start Group
func (g *Group) Start() error {
	// Reference Validator code
	return errors.New("implement me")
}

// GetMsgResolver returns proper handler for msg based on msg type
func (g *Group) GetMsgResolver(networkMsg network.NetworkMsg) func(msg *proto.SignedMessage) {
	// TODO: Implement, reference Validator code
	return nil
}


// ExecuteDuty executes the given duty
func (g *Group) ExecuteDuty(ctx context.Context, duty *Duty) {
	// TODO: Implement, reference Validator code, but the duty is replaced with mpc.Duty
}
