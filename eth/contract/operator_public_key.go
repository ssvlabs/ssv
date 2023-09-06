// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contract

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// OperatorPublicKeyMetaData contains all meta data concerning the OperatorPublicKey contract.
var OperatorPublicKeyMetaData = &bind.MetaData{
	ABI: "[{\"name\":\"method\",\"type\":\"function\",\"outputs\":[{\"type\":\"bytes\"}]}]",
}

// OperatorPublicKeyABI is the input ABI used to generate the binding from.
// Deprecated: Use OperatorPublicKeyMetaData.ABI instead.
var OperatorPublicKeyABI = OperatorPublicKeyMetaData.ABI

// OperatorPublicKey is an auto generated Go binding around an Ethereum contract.
type OperatorPublicKey struct {
	OperatorPublicKeyCaller     // Read-only binding to the contract
	OperatorPublicKeyTransactor // Write-only binding to the contract
	OperatorPublicKeyFilterer   // Log filterer for contract events
}

// OperatorPublicKeyCaller is an auto generated read-only Go binding around an Ethereum contract.
type OperatorPublicKeyCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// OperatorPublicKeyTransactor is an auto generated write-only Go binding around an Ethereum contract.
type OperatorPublicKeyTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// OperatorPublicKeyFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type OperatorPublicKeyFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// OperatorPublicKeySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type OperatorPublicKeySession struct {
	Contract     *OperatorPublicKey // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// OperatorPublicKeyCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type OperatorPublicKeyCallerSession struct {
	Contract *OperatorPublicKeyCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// OperatorPublicKeyTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type OperatorPublicKeyTransactorSession struct {
	Contract     *OperatorPublicKeyTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// OperatorPublicKeyRaw is an auto generated low-level Go binding around an Ethereum contract.
type OperatorPublicKeyRaw struct {
	Contract *OperatorPublicKey // Generic contract binding to access the raw methods on
}

// OperatorPublicKeyCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type OperatorPublicKeyCallerRaw struct {
	Contract *OperatorPublicKeyCaller // Generic read-only contract binding to access the raw methods on
}

// OperatorPublicKeyTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type OperatorPublicKeyTransactorRaw struct {
	Contract *OperatorPublicKeyTransactor // Generic write-only contract binding to access the raw methods on
}

// NewOperatorPublicKey creates a new instance of OperatorPublicKey, bound to a specific deployed contract.
func NewOperatorPublicKey(address common.Address, backend bind.ContractBackend) (*OperatorPublicKey, error) {
	contract, err := bindOperatorPublicKey(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &OperatorPublicKey{OperatorPublicKeyCaller: OperatorPublicKeyCaller{contract: contract}, OperatorPublicKeyTransactor: OperatorPublicKeyTransactor{contract: contract}, OperatorPublicKeyFilterer: OperatorPublicKeyFilterer{contract: contract}}, nil
}

// NewOperatorPublicKeyCaller creates a new read-only instance of OperatorPublicKey, bound to a specific deployed contract.
func NewOperatorPublicKeyCaller(address common.Address, caller bind.ContractCaller) (*OperatorPublicKeyCaller, error) {
	contract, err := bindOperatorPublicKey(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &OperatorPublicKeyCaller{contract: contract}, nil
}

// NewOperatorPublicKeyTransactor creates a new write-only instance of OperatorPublicKey, bound to a specific deployed contract.
func NewOperatorPublicKeyTransactor(address common.Address, transactor bind.ContractTransactor) (*OperatorPublicKeyTransactor, error) {
	contract, err := bindOperatorPublicKey(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &OperatorPublicKeyTransactor{contract: contract}, nil
}

// NewOperatorPublicKeyFilterer creates a new log filterer instance of OperatorPublicKey, bound to a specific deployed contract.
func NewOperatorPublicKeyFilterer(address common.Address, filterer bind.ContractFilterer) (*OperatorPublicKeyFilterer, error) {
	contract, err := bindOperatorPublicKey(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &OperatorPublicKeyFilterer{contract: contract}, nil
}

// bindOperatorPublicKey binds a generic wrapper to an already deployed contract.
func bindOperatorPublicKey(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := OperatorPublicKeyMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_OperatorPublicKey *OperatorPublicKeyRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _OperatorPublicKey.Contract.OperatorPublicKeyCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_OperatorPublicKey *OperatorPublicKeyRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.OperatorPublicKeyTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_OperatorPublicKey *OperatorPublicKeyRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.OperatorPublicKeyTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_OperatorPublicKey *OperatorPublicKeyCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _OperatorPublicKey.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_OperatorPublicKey *OperatorPublicKeyTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_OperatorPublicKey *OperatorPublicKeyTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.contract.Transact(opts, method, params...)
}

// Method is a paid mutator transaction binding the contract method 0x2c383a9f.
//
// Solidity: function method() returns(bytes)
func (_OperatorPublicKey *OperatorPublicKeyTransactor) Method(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _OperatorPublicKey.contract.Transact(opts, "method")
}

// Method is a paid mutator transaction binding the contract method 0x2c383a9f.
//
// Solidity: function method() returns(bytes)
func (_OperatorPublicKey *OperatorPublicKeySession) Method() (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.Method(&_OperatorPublicKey.TransactOpts)
}

// Method is a paid mutator transaction binding the contract method 0x2c383a9f.
//
// Solidity: function method() returns(bytes)
func (_OperatorPublicKey *OperatorPublicKeyTransactorSession) Method() (*types.Transaction, error) {
	return _OperatorPublicKey.Contract.Method(&_OperatorPublicKey.TransactOpts)
}
