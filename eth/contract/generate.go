// Package contract generates the contract bindings for v1 contract.
package contract

// contract.abi is taken from https://github.com/ssvlabs/ssv-network/blob/contract-abi/docs/mainnet/v1.0.2/abi/SSVNetwork.json
//go:generate go tool -modfile=../../tool.mod abigen --abi contract.abi --pkg contract --out contract.go
//go:generate go tool -modfile=../../tool.mod abigen --abi operator_public_key.abi --pkg contract --out operator_public_key.go --type OperatorPublicKey
