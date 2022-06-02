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
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

// Options to add in group struct creation
type Options struct {
	Context                    context.Context
	Logger                     *zap.Logger
	Share                      *storage.Share
	SignatureCollectionTimeout time.Duration
	Network                    network.Network
	ETHNetwork                 *core.Network
	DB                         basedb.IDb
	Fork                       forks.Fork
	Signer                     beacon.Signer
	SyncRateLimit              time.Duration

	notifyOperatorID func(string)
}

// Group represents a group operators / mpc players to generate key, perform adhoc signing,
// hence it has similiar structure to Validator, in future it may be merged into Validator,
// but for now it's kept separate.
// it holds the corresponding ibft controllers to trigger consensus layer
type Group struct {
	ctx                        context.Context
	logger                     *zap.Logger
	Share                      *storage.Share
	ethNetwork                 *core.Network
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
