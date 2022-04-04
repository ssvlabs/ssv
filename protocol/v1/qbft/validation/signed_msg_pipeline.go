package validation

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// TODO: use generics when updating to go 1.18
// to avoid duplicating the pipeline interface for SignedMessages and PostConsensusSignedMessages

// SignedMessagePipeline represents the behavior of round pipeline
type SignedMessagePipeline interface {
	// Run runs the pipeline
	Run(signedMessage *message.SignedMessage) error
	Name() string
}

// CombineQuiet runs quiet and afterwards pipeline if not error returned.
// if quiet returns an error it will be ignored
func CombineQuiet(quiet SignedMessagePipeline, pipeline SignedMessagePipeline) SignedMessagePipeline {
	return WrapFunc("if first pipeline non error, continue to second", func(signedMessage *message.SignedMessage) error {
		if quiet.Run(signedMessage) == nil {
			return pipeline.Run(signedMessage)
		}
		return nil
	})
}

// Combine is the constructor of pipelinesCombination
func Combine(pipelines ...SignedMessagePipeline) SignedMessagePipeline {
	return &pipelinesCombination{
		pipelines: pipelines,
	}
}

// pipelinesCombination implements SignedMessagePipeline interface with multiple pipelines logic.
type pipelinesCombination struct {
	pipelines []SignedMessagePipeline
}

// Run implements SignedMessagePipeline interface
func (p *pipelinesCombination) Run(signedMessage *message.SignedMessage) error {
	for _, pp := range p.pipelines {
		if err := pp.Run(signedMessage); err != nil {
			return err
		}
	}
	return nil
}

// Name implements SignedMessagePipeline interface
func (p *pipelinesCombination) Name() string {
	ret := "combination of: "
	for _, p := range p.pipelines {
		ret += p.Name() + ", "
	}
	return ret
}

// pipelineFunc implements SignedMessagePipeline interface using just a function.
type pipelineFunc struct {
	fn   func(signedMessage *message.SignedMessage) error
	name string
}

// WrapFunc represents the given function as a pipeline implementor
func WrapFunc(name string, fn func(signedMessage *message.SignedMessage) error) SignedMessagePipeline {
	return &pipelineFunc{
		fn:   fn,
		name: name,
	}
}

// Run implements SignedMessagePipeline interface
func (p *pipelineFunc) Run(signedMessage *message.SignedMessage) error {
	return p.fn(signedMessage)
}

// Name implements SignedMessagePipeline interface
func (p *pipelineFunc) Name() string {
	return p.name
}

