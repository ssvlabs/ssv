package pipeline

import "github.com/bloxapp/ssv/ibft/proto"

// SetStage sets the given stage
type SetStage func(stage proto.RoundState)

// SignAndBroadcast is the function to sign and broadcast message
type SignAndBroadcast func(msg *proto.Message) error

// Pipeline represents the behavior of round pipeline
type Pipeline interface {
	// Run runs the pipeline
	Run(signedMessage *proto.SignedMessage) error
	Name() string
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

// Name implements Pipeline interface
func (p *pipelinesCombination) Name() string {
	ret := "combination of: "
	for _, p := range p.pipelines {
		ret += p.Name() + ", "
	}
	return ret
}

// pipelineFunc implements Pipeline interface using just a function.
type pipelineFunc struct {
	fn   func(signedMessage *proto.SignedMessage) error
	name string
}

// WrapFunc represents the given function as a pipeline implementor
func WrapFunc(name string, fn func(signedMessage *proto.SignedMessage) error) Pipeline {
	return &pipelineFunc{
		fn:   fn,
		name: name,
	}
}

// Run implements Pipeline interface
func (p *pipelineFunc) Run(signedMessage *proto.SignedMessage) error {
	return p.fn(signedMessage)
}

// Name implements Pipeline interface
func (p *pipelineFunc) Name() string {
	return p.name
}
