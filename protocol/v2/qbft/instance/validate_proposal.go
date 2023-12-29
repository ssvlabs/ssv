package instance

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func ValidateJustificationWithoutSignatureCheck(
	domain spectypes.DomainType,
	share *ssvtypes.SSVShare,
	roundChangeMsgs []*specqbft.SignedMessage,
	prepareMsgs []*specqbft.SignedMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	var nonSigningInstance Instance

	return nonSigningInstance.isProposalJustification(
		&specqbft.State{
			Share:  &share.Share,
			Height: height,
		},
		newQBFTConfigWithoutSignatureVerification(domain),
		roundChangeMsgs,
		prepareMsgs,
		height,
		round,
		fullData,
		func(data []byte) error { return nil },
	)
}

type qbftConfigWithoutSignatureVerification struct {
	domain spectypes.DomainType
}

func newQBFTConfigWithoutSignatureVerification(domain spectypes.DomainType) qbftConfigWithoutSignatureVerification {
	return qbftConfigWithoutSignatureVerification{
		domain: domain,
	}
}

func (q qbftConfigWithoutSignatureVerification) GetSigner() spectypes.SSVSigner {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) GetSignatureDomainType() spectypes.DomainType {
	return q.domain
}

func (q qbftConfigWithoutSignatureVerification) GetValueCheckF() specqbft.ProposedValueCheckF {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) GetProposerF() specqbft.ProposerF {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) GetNetwork() specqbft.Network {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) GetStorage() qbftstorage.QBFTStore {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) GetTimer() roundtimer.Timer {
	panic("should not be called")
}

func (q qbftConfigWithoutSignatureVerification) VerifySignatures() bool {
	return false
}
