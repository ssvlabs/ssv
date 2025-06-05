package ethtest

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type SetFeeRecipientAddressInput struct {
	*CommonTestInput
	events []*SetFeeRecipientAddressEventInput
}

func NewSetFeeRecipientAddressInput(common *CommonTestInput) *SetFeeRecipientAddressInput {
	return &SetFeeRecipientAddressInput{common, nil}
}

func (input *SetFeeRecipientAddressInput) validate() error {
	if input.CommonTestInput == nil {
		return fmt.Errorf("validation error: CommonTestInput is empty")
	}
	if input.events == nil {
		return fmt.Errorf("validation error: empty events")
	}
	for _, e := range input.events {
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

type SetFeeRecipientAddressEventInput struct {
	auth    *bind.TransactOpts
	address *ethcommon.Address
}

func (input *SetFeeRecipientAddressEventInput) validate() error {
	if input == nil {
		return fmt.Errorf("validation error: empty input")
	}

	switch {
	case input.auth == nil:
		return fmt.Errorf("validation error: input.auth is empty")
	case input.address == nil:
		return fmt.Errorf("validation error: input.address is empty")
	}

	return nil
}

func (input *SetFeeRecipientAddressInput) prepare(
	eventsToDo []*SetFeeRecipientAddressEventInput,
) {
	input.events = eventsToDo
}

func (input *SetFeeRecipientAddressInput) produce() {
	err := input.validate()
	require.NoError(input.t, err)

	for _, event := range input.events {
		// Call the contract method
		_, err = input.boundContract.SetFeeRecipientAddress(
			event.auth,
			*event.address,
		)
		require.NoError(input.t, err)

		if !input.doInOneBlock {
			commitBlock(input.sim, input.blockNum)
		}
	}
	if input.doInOneBlock {
		commitBlock(input.sim, input.blockNum)
	}
}
