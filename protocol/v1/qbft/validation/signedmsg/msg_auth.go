package signedmsg

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("authorize", func(signedMessage *specqbft.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
