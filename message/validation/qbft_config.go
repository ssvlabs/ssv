package validation

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

type qbftConfig struct {
}

func (q qbftConfig) GetSigner() spectypes.SSVSigner {
	panic("should not be called")
}

func (q qbftConfig) GetSignatureDomainType() spectypes.DomainType {
	panic("should not be called")
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
	return true
}
