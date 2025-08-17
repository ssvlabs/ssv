package eventparser

import (
	"fmt"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
)

// PackOperatorPublicKey is used for testing only, packing the operator pubkey bytes into an event.
func PackOperatorPublicKey(pubKey string) ([]byte, error) {
	byts, err := ethabi.NewType("bytes", "bytes", nil)
	if err != nil {
		return nil, err
	}

	args := ethabi.Arguments{
		{
			Name: "publicKey",
			Type: byts,
		},
	}

	outField, err := args.Pack([]byte(pubKey))
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}

	return outField, nil
}
