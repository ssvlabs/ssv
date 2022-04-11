package controller

import (
	"context"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

type Controllers map[beaconprotocol.RoleType]IController

func (c Controllers) ControllerForIdentifier(identifier message.Identifier) IController {
	role := identifier.GetRoleType()
	return c[role]
}

type SyncContext struct {
	Store qbftstorage.DecidedMsgStore
	Syncer p2pprotocol.Syncer
	Validate validation.SignedMessagePipeline
	Identifier message.Identifier
}

type SyncDecided func(ctx context.Context, sctx *SyncContext) error
