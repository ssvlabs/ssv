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
	// TODO - change to normal prepare pipeline
	data, err := signedMessage.Message.GetRoundChangeData()
	if err != nil {
		return fmt.Errorf("could not get roundChange data : %w", err) // TODO(nkryuchkov): remove whitespace in ssv-spec
	}
	if data == nil {
		return errors.New("change round data is nil")
	}
	if data.PreparedValue == nil || len(data.PreparedValue) == 0 { // no justification
		return nil
	}
	roundChangeJust := data.RoundChangeJustification
	if roundChangeJust == nil {
		return errors.New("change round justification is nil")
	}
	if len(roundChangeJust) == 0 {
		return errors.New("change round justification msg array is empty")
	}
	if roundChangeJust[0].Message == nil {
		return errors.New("change round justification msg is nil")
	}
	if roundChangeJust[0].Message.MsgType != specqbft.PrepareMsgType {
		return errors.Errorf("change round justification msg type not Prepare (%d)", roundChangeJust[0].Message.MsgType)
	}
	if signedMessage.Message.Height != roundChangeJust[0].Message.Height {
		return errors.New("change round justification sequence is wrong")
	}
	if signedMessage.Message.Round <= roundChangeJust[0].Message.Round {
		return errors.New("round change justification invalid: msg round wrong")
	}
	if data.PreparedRound != roundChangeJust[0].Message.Round {
		return errors.New("round change justification invalid: msg round wrong")
	}
	if !bytes.Equal(signedMessage.Message.Identifier, roundChangeJust[0].Message.Identifier) {
		return errors.New("change round justification msg Lambda not equal to msg Lambda not equal to instance lambda")
	}
	prepareMsg, err := roundChangeJust[0].Message.GetPrepareData()
	if err != nil {
		return errors.Wrap(err, "failed to get prepare data")
	}
	if !bytes.Equal(data.PreparedValue, prepareMsg.Data) {
		return errors.New("round change justification invalid: prepare data != proposed data")
	}
	if len(roundChangeJust[0].GetSigners()) < p.share.ThresholdSize() {
		return errors.New("change round justification does not constitute a quorum")
	}

	// validateJustification justification signature
	pksMap, err := p.share.PubKeysByID(data.RoundChangeJustification[0].GetSigners())
	var pks beacon.PubKeys
	for _, v := range pksMap {
		pks = append(pks, v)
	}

	if err != nil {
		return errors.Wrap(err, "change round could not get pubkey")
	}
	aggregated := pks.Aggregate()
	for _, justification := range data.RoundChangeJustification {
		err = justification.Signature.Verify(justification, spectypes.GetDefaultDomain(), spectypes.QBFTSignatureType, aggregated.Serialize())
		if err != nil {
			return errors.Wrap(err, "change round could not verify signature")
		}
	}

	return nil
}

// Name implements pipeline.Pipeline interface
func (p *validateJustification) Name() string {
	return "validateJustification msg"
}
