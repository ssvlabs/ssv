package validator

import (
	"context"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
)

type Options struct {
	Context  context.Context
	Logger   *zap.Logger
	Network  p2pprotocol.Network
	Beacon   beaconprotocol.Beacon
	Share    *storage.Share // TODO should be protocol
	ReadMode bool
}

type Validator struct {
	ctx         context.Context
	logger      *zap.Logger
	Share       *storage.Share
	network     p2pprotocol.Network
	beacon      beaconprotocol.Beacon
	dutyRunners map[beaconprotocol.RoleType]*DutyRunner

	// queue

	readMode bool
}

func NewValidator(opt Options) *Validator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", opt.Share.NodeID))

	// updating goclient map
	if opt.Share.HasMetadata() && opt.Share.Metadata.Index > 0 {
		blsPubkey := spec.BLSPubKey{}
		copy(blsPubkey[:], opt.Share.PublicKey.Serialize())
		opt.Beacon.ExtendIndexMap(opt.Share.Metadata.Index, blsPubkey)
	}

	return &Validator{}
}

func ProcessMsg(msg *message.SignedMessage) {

}
