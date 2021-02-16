package ibft

import "github.com/bloxapp/ssv/ibft/proto"

// PipelineFunc defines the function signature
type PipelineFunc func(signedMessage *proto.SignedMessage) error

// Pipeline defines an array of PipelineFunc function types
type Pipeline []PipelineFunc

// Run pipeline checks against the signed message
func (p Pipeline) Run(signedMessage *proto.SignedMessage) error {
	for _, pp := range p {
		if err := pp(signedMessage); err != nil {
			return err
		}
	}
	return nil
}
