package controller

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

type Controllers map[message.RoleType]IController

func (c Controllers) ControllerForIdentifier(identifier message.Identifier) IController {
	role := identifier.GetRoleType()
	return c[role]
}

type SyncContext struct {
	Store      qbftstorage.DecidedMsgStore
	Syncer     p2pprotocol.Syncer
	Validate   pipelines.SignedMessagePipeline
	Identifier message.Identifier
}

type SyncDecided func(ctx context.Context, sctx *SyncContext) error

type SyncRound func(ctx context.Context, sctx *SyncContext) ([]*message.SignedMessage, error)
