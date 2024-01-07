package validation

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// qbftConfig is used in message validation and has no signature verification.
type qbftConfig struct {
	domain spectypes.DomainType
}

func newQBFTConfig(domain spectypes.DomainType) qbftConfig {
	return qbftConfig{
		domain: domain,
	}
}

func (q qbftConfig) GetSigner() spectypes.SSVSigner {
	panic("should not be called")
}

func (q qbftConfig) GetSignatureDomainType() spectypes.DomainType {
	return q.domain
}

func (q qbftConfig) GetValueCheckF() specqbft.ProposedValueCheckF {
	panic("should not be called")
}

func (q qbftConfig) GetProposerF() specqbft.ProposerF {
	panic("should not be called")
}

func (q qbftConfig) GetNetwork() specqbft.Network {
	panic("should not be called")
}

func (q qbftConfig) GetStorage() qbftstorage.QBFTStore {
	panic("should not be called")
}

func (q qbftConfig) GetTimer() roundtimer.Timer {
	panic("should not be called")
}

func (q qbftConfig) VerifySignatures() bool {
	return false
}
