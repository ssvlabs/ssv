package validation

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	genesisvalidation "github.com/ssvlabs/ssv/message/validation/genesis"
	"github.com/ssvlabs/ssv/networkconfig"
)

type ForkingMessageValidation struct {
	NetworkConfig networkconfig.NetworkConfig
	Genesis       genesisvalidation.MessageValidator
	Alan          MessageValidator
}

func (f *ForkingMessageValidation) Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if f.NetworkConfig.PastAlanFork() {
		return f.Alan.Validate(ctx, p, pmsg)
	}
	return f.Genesis.Validate(ctx, p, pmsg)
}

func (f *ForkingMessageValidation) ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		if f.NetworkConfig.PastAlanFork() {
			return f.Alan.ValidatorForTopic(topic)(ctx, p, pmsg)
		}
		return f.Genesis.ValidatorForTopic(topic)(ctx, p, pmsg)
	}
}
