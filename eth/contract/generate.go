// Package contract generates the contract bindings for v1 contract.
package contract

// contract.abi is taken from https://github.com/bloxapp/ssv-network/blob/contract-abi/docs/mainnet/v1.0.2/abi/SSVNetwork.json
//go:generate abigen --abi contract.abi --pkg contract --out contract.go
//go:generate abigen --abi operator_public_key.abi --pkg contract --out operator_public_key.go --type OperatorPublicKey
