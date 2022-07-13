package signedmsg

import (
	"errors"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateQuorum is the pipeline to validate msg quorum requirement
func ValidateQuorum(threshold int) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("quorum", func(signedMessage *specqbft.SignedMessage) error {
		if len(signedMessage.GetSigners()) < threshold {
			return errors.New("quorum not achieved")
		}
		return nil
	})
}
