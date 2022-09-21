package qbft

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types/testingutils"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/proposal"
)

type forkWithValueCheck struct {
	forks.Fork
}

func (f forkWithValueCheck) ProposalMsgValidationPipeline(share *beaconprotocol.Share, state *qbftprotocol.State, roundLeader proposal.LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		f.Fork.ProposalMsgValidationPipeline(share, state, roundLeader),
		msgValueCheck(),
	)
}

func msgValueCheck() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("value check", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage.Message.MsgType != specqbft.ProposalMsgType {
			return nil
		}

		proposalData, err := signedMessage.Message.GetProposalData()
		if err != nil {
			return nil
		}
		if err := proposalData.Validate(); err != nil {
			return nil
		}

		if err := testingutils.TestingConfig(testingutils.Testing4SharesSet()).ValueCheckF(proposalData.Data); err != nil {
			return fmt.Errorf("proposal not justified: proposal value invalid: %w", err)
		}

		return nil
	})
}
