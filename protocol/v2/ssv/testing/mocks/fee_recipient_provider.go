package mocks

import (
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

type FeeRecipientProvider struct {
}

func (m *FeeRecipientProvider) GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	return spectestingutils.TestingFeeRecipient, nil
}
