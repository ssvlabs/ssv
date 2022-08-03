package changeround

import (
	"bytes"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// validateJustification validates change round justifications
type validateJustification struct {
	share *beacon.Share
}

// Validate is the constructor of validateJustification
func Validate(share *beacon.Share) pipelines.SignedMessagePipeline {
	return &validateJustification{
		share: share,
	}
}

// Run implements pipeline.Pipeline interface
func (p *validateJustification) Run(signedMessage *specqbft.SignedMessage) error {
	if signedMessage.Message.Data == nil {
		return errors.New("change round justification msg is nil")
	}

	if len(signedMessage.GetSigners()) != 1 {
		return errors.New("round change msg allows 1 signer")
	}

	// TODO - change to normal prepare pipeline
	data, err := signedMessage.Message.GetRoundChangeData()
	if err != nil {
		return fmt.Errorf("could not get roundChange data : %w", err) // TODO(nkryuchkov): remove whitespace in ssv-spec
	}
	if data == nil {
		return errors.New("change round data is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("roundChangeData invalid: %w", err)
	}

	if !data.Prepared() {
		return nil
	}

	roundChangeJust := data.RoundChangeJustification
	if roundChangeJust == nil {
		return errors.New("change round justification is nil")
	}
	if len(roundChangeJust) == 0 {
		return errors.New("change round justification msg array is empty")
	}

	for _, rcj := range roundChangeJust {
		if rcj.Message == nil {
			return errors.New("change round justification msg is nil")
		}
		if rcj.Message.MsgType != specqbft.PrepareMsgType {
			return errors.Errorf("change round justification msg type not Prepare (%d)", rcj.Message.MsgType)
		}
		if signedMessage.Message.Height != rcj.Message.Height {
			return errors.New("change round justification sequence is wrong")
		}
		if signedMessage.Message.Round <= rcj.Message.Round {
			return errors.New("round change justification invalid: msg round wrong")
		}
		if data.PreparedRound != rcj.Message.Round {
			return errors.New("round change justification invalid: msg round wrong")
		}
		if !bytes.Equal(signedMessage.Message.Identifier, rcj.Message.Identifier) {
			return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
		}
		prepareMsg, err := rcj.Message.GetPrepareData()
		if err != nil {
			return errors.Wrap(err, "failed to get prepare data")
		}
		if !bytes.Equal(data.PreparedValue, prepareMsg.Data) {
			return errors.New("round change justification invalid: prepare data != proposed data")
		}
		if len(rcj.GetSigners()) != 1 {
			return errors.New("round change msg allows 1 signer")
		}

		// validateJustification justification signature
		pksMap, err := p.share.PubKeysByID(rcj.GetSigners())
		var pks beacon.PubKeys
		for _, v := range pksMap {
			pks = append(pks, v)
		}

		if err != nil {
			return errors.Wrap(err, "change round could not get pubkey")
		}
		aggregated := pks.Aggregate()

		if err = rcj.Signature.Verify(rcj, spectypes.PrimusTestnet, spectypes.QBFTSignatureType, aggregated.Serialize()); err != nil {
			return errors.Wrap(err, "change round could not verify signature")
		}
	}

	if quorum, _, _ := p.changeRoundQuorum(roundChangeJust); !quorum {
		return fmt.Errorf("no justifications quorum")
	}

	if data.PreparedRound > signedMessage.Message.Round {
		return errors.New("prepared round > round")
	}

	return nil
}

// Name implements pipeline.Pipeline interface
func (p *validateJustification) Name() string {
	return "validateJustification msg"
}

// TODO(nkryuchkov): Consider merging with all changeRoundQuorum functions.
func (p *validateJustification) changeRoundQuorum(msgs []*specqbft.SignedMessage) (quorum bool, t int, n int) {
	uniqueSigners := make(map[spectypes.OperatorID]bool)
	for _, msg := range msgs {
		for _, signer := range msg.GetSigners() {
			uniqueSigners[signer] = true
		}
	}
	quorum = len(uniqueSigners)*3 >= p.share.CommitteeSize()*2
	return quorum, len(uniqueSigners), p.share.CommitteeSize()
}
