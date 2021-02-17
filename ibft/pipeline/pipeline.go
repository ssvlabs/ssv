package pipeline

import "github.com/bloxapp/ssv/ibft/proto"

// Pipeline represents the behavior of round pipeline
type Pipeline interface {
	// Run runs the pipeline
	Run(signedMessage *proto.SignedMessage) error
}

// pipelinesCombination implements Pipeline interface with multiple pipelines logic.
type pipelinesCombination struct {
	pipelines []Pipeline
}

// Combine is the constructor of pipelinesCombination
func Combine(pipelines ...Pipeline) Pipeline {
	return &pipelinesCombination{
		pipelines: pipelines,
	}
}

// Run implements Pipeline interface
func (p *pipelinesCombination) Run(signedMessage *proto.SignedMessage) error {
	for _, pp := range p.pipelines {
		if err := pp.Run(signedMessage); err != nil {
			return err
		}
	}
	return nil
}

// pipelineFunc implements Pipeline interface using just a function.
type pipelineFunc struct {
	fn func(signedMessage *proto.SignedMessage) error
}

// PipelineFunc represents the given function as a pipeline implementor
func PipelineFunc(fn func(signedMessage *proto.SignedMessage) error) Pipeline {
	return &pipelineFunc{
		fn: fn,
	}
}

// Run implements Pipeline interface
func (p *pipelineFunc) Run(signedMessage *proto.SignedMessage) error {
	return p.fn(signedMessage)
}
