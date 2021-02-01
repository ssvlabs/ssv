package ibft

import "github.com/bloxapp/ssv/ibft/proto"

type PipelineFunc func(signedMessage *proto.SignedMessage) error
type Pipeline []PipelineFunc

func (p Pipeline) Run(signedMessage *proto.SignedMessage) error {
	for _, pp := range p {
		if err := pp(signedMessage); err != nil {
			return err
		}
	}
	return nil
}
