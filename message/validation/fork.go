package validation

import (
	"context"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	genesisvalidation "github.com/ssvlabs/ssv/message/validation/genesis"
	"github.com/ssvlabs/ssv/networkconfig"
)

type ForkingMessageValidation struct {
	NetworkConfig networkconfig.NetworkConfig
	Genesis       genesisvalidation.MessageValidator
	Alan          MessageValidator
	Logger        *zap.Logger
}

func (f *ForkingMessageValidation) Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if f.NetworkConfig.PastAlanFork() {
		f.Logger.Warn("<<<<<<<<<<<<<<<<<<<<<<<<<<<<PastAlanFork (Validate)>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
		return f.Alan.Validate(ctx, p, pmsg)
	}
	f.Logger.Warn("<<<<<<<<<<<<<<<<<<<<<<<<<<<<PreAlanFork (Validate)>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	return f.Genesis.Validate(ctx, p, pmsg)
}

func (f *ForkingMessageValidation) ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if f.NetworkConfig.PastAlanFork() {
		f.Logger.Warn("<<<<<<<<<<<<<<<<<<<<<<<<<<<<PastAlanFork (ValidatorForTopic)>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
		return f.Alan.ValidatorForTopic(topic)
	}
	f.Logger.Warn("<<<<<<<<<<<<<<<<<<<<<<<<<<<<PreAlanFork (ValidatorForTopic)>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	return f.Genesis.ValidatorForTopic(topic)
}
