// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package simcontract

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

// CallableCluster is an auto generated low-level Go binding around an user-defined struct.
type CallableCluster struct {
	ValidatorCount  uint32
	NetworkFeeIndex uint64
	Index           uint64
	Active          bool
	Balance         *big.Int
}

// SimcontractMetaData contains all meta data concerning the Simcontract contract.
var SimcontractMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterLiquidated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterReactivated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipientAddress\",\"type\":\"address\"}],\"name\":\"FeeRecipientAddressUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"OperatorAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"OperatorRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"shares\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ValidatorAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"}],\"name\":\"ValidatorExited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ValidatorRemoved\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"exitValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"clusterOwner\",\"type\":\"address\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"liquidate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"reactivate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"registerOperator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"bytes\",\"name\":\"sharesData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"registerValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"removeOperator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structCallable.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"removeValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recipientAddress\",\"type\":\"address\"}],\"name\":\"setFeeRecipientAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// SimcontractABI is the input ABI used to generate the binding from.
// Deprecated: Use SimcontractMetaData.ABI instead.
var SimcontractABI = SimcontractMetaData.ABI

// Simcontract is an auto generated Go binding around an Ethereum contract.
type Simcontract struct {
	SimcontractCaller     // Read-only binding to the contract
	SimcontractTransactor // Write-only binding to the contract
	SimcontractFilterer   // Log filterer for contract events
}

// SimcontractCaller is an auto generated read-only Go binding around an Ethereum contract.
type SimcontractCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimcontractTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SimcontractTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimcontractFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SimcontractFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimcontractSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SimcontractSession struct {
	Contract     *Simcontract      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SimcontractCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SimcontractCallerSession struct {
	Contract *SimcontractCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// SimcontractTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SimcontractTransactorSession struct {
	Contract     *SimcontractTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// SimcontractRaw is an auto generated low-level Go binding around an Ethereum contract.
type SimcontractRaw struct {
	Contract *Simcontract // Generic contract binding to access the raw methods on
}

// SimcontractCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SimcontractCallerRaw struct {
	Contract *SimcontractCaller // Generic read-only contract binding to access the raw methods on
}

// SimcontractTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SimcontractTransactorRaw struct {
	Contract *SimcontractTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSimcontract creates a new instance of Simcontract, bound to a specific deployed contract.
func NewSimcontract(address common.Address, backend bind.ContractBackend) (*Simcontract, error) {
	contract, err := bindSimcontract(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Simcontract{SimcontractCaller: SimcontractCaller{contract: contract}, SimcontractTransactor: SimcontractTransactor{contract: contract}, SimcontractFilterer: SimcontractFilterer{contract: contract}}, nil
}

// NewSimcontractCaller creates a new read-only instance of Simcontract, bound to a specific deployed contract.
func NewSimcontractCaller(address common.Address, caller bind.ContractCaller) (*SimcontractCaller, error) {
	contract, err := bindSimcontract(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SimcontractCaller{contract: contract}, nil
}

// NewSimcontractTransactor creates a new write-only instance of Simcontract, bound to a specific deployed contract.
func NewSimcontractTransactor(address common.Address, transactor bind.ContractTransactor) (*SimcontractTransactor, error) {
	contract, err := bindSimcontract(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SimcontractTransactor{contract: contract}, nil
}

// NewSimcontractFilterer creates a new log filterer instance of Simcontract, bound to a specific deployed contract.
func NewSimcontractFilterer(address common.Address, filterer bind.ContractFilterer) (*SimcontractFilterer, error) {
	contract, err := bindSimcontract(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SimcontractFilterer{contract: contract}, nil
}

// bindSimcontract binds a generic wrapper to an already deployed contract.
func bindSimcontract(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SimcontractMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Simcontract *SimcontractRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Simcontract.Contract.SimcontractCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Simcontract *SimcontractRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Simcontract.Contract.SimcontractTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Simcontract *SimcontractRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Simcontract.Contract.SimcontractTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Simcontract *SimcontractCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Simcontract.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Simcontract *SimcontractTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Simcontract.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Simcontract *SimcontractTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Simcontract.Contract.contract.Transact(opts, method, params...)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Simcontract *SimcontractTransactor) ExitValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "exitValidator", publicKey, operatorIds)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Simcontract *SimcontractSession) ExitValidator(publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Simcontract.Contract.ExitValidator(&_Simcontract.TransactOpts, publicKey, operatorIds)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Simcontract *SimcontractTransactorSession) ExitValidator(publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Simcontract.Contract.ExitValidator(&_Simcontract.TransactOpts, publicKey, operatorIds)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactor) Liquidate(opts *bind.TransactOpts, clusterOwner common.Address, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "liquidate", clusterOwner, operatorIds, cluster)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractSession) Liquidate(clusterOwner common.Address, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.Liquidate(&_Simcontract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactorSession) Liquidate(clusterOwner common.Address, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.Liquidate(&_Simcontract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// Reactivate is a paid mutator transaction binding the contract method 0x5fec6dd0.
//
// Solidity: function reactivate(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactor) Reactivate(opts *bind.TransactOpts, operatorIds []uint64, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "reactivate", operatorIds, amount, cluster)
}

// Reactivate is a paid mutator transaction binding the contract method 0x5fec6dd0.
//
// Solidity: function reactivate(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractSession) Reactivate(operatorIds []uint64, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.Reactivate(&_Simcontract.TransactOpts, operatorIds, amount, cluster)
}

// Reactivate is a paid mutator transaction binding the contract method 0x5fec6dd0.
//
// Solidity: function reactivate(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactorSession) Reactivate(operatorIds []uint64, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.Reactivate(&_Simcontract.TransactOpts, operatorIds, amount, cluster)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xff212c5c.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee) returns()
func (_Simcontract *SimcontractTransactor) RegisterOperator(opts *bind.TransactOpts, publicKey []byte, fee *big.Int) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "registerOperator", publicKey, fee)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xff212c5c.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee) returns()
func (_Simcontract *SimcontractSession) RegisterOperator(publicKey []byte, fee *big.Int) (*types.Transaction, error) {
	return _Simcontract.Contract.RegisterOperator(&_Simcontract.TransactOpts, publicKey, fee)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xff212c5c.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee) returns()
func (_Simcontract *SimcontractTransactorSession) RegisterOperator(publicKey []byte, fee *big.Int) (*types.Transaction, error) {
	return _Simcontract.Contract.RegisterOperator(&_Simcontract.TransactOpts, publicKey, fee)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0x06e8fb9c.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactor) RegisterValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64, sharesData []byte, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "registerValidator", publicKey, operatorIds, sharesData, amount, cluster)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0x06e8fb9c.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractSession) RegisterValidator(publicKey []byte, operatorIds []uint64, sharesData []byte, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.RegisterValidator(&_Simcontract.TransactOpts, publicKey, operatorIds, sharesData, amount, cluster)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0x06e8fb9c.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactorSession) RegisterValidator(publicKey []byte, operatorIds []uint64, sharesData []byte, amount *big.Int, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.RegisterValidator(&_Simcontract.TransactOpts, publicKey, operatorIds, sharesData, amount, cluster)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Simcontract *SimcontractTransactor) RemoveOperator(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "removeOperator", operatorId)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Simcontract *SimcontractSession) RemoveOperator(operatorId uint64) (*types.Transaction, error) {
	return _Simcontract.Contract.RemoveOperator(&_Simcontract.TransactOpts, operatorId)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Simcontract *SimcontractTransactorSession) RemoveOperator(operatorId uint64) (*types.Transaction, error) {
	return _Simcontract.Contract.RemoveOperator(&_Simcontract.TransactOpts, operatorId)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactor) RemoveValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "removeValidator", publicKey, operatorIds, cluster)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractSession) RemoveValidator(publicKey []byte, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.RemoveValidator(&_Simcontract.TransactOpts, publicKey, operatorIds, cluster)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Simcontract *SimcontractTransactorSession) RemoveValidator(publicKey []byte, operatorIds []uint64, cluster CallableCluster) (*types.Transaction, error) {
	return _Simcontract.Contract.RemoveValidator(&_Simcontract.TransactOpts, publicKey, operatorIds, cluster)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Simcontract *SimcontractTransactor) SetFeeRecipientAddress(opts *bind.TransactOpts, recipientAddress common.Address) (*types.Transaction, error) {
	return _Simcontract.contract.Transact(opts, "setFeeRecipientAddress", recipientAddress)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Simcontract *SimcontractSession) SetFeeRecipientAddress(recipientAddress common.Address) (*types.Transaction, error) {
	return _Simcontract.Contract.SetFeeRecipientAddress(&_Simcontract.TransactOpts, recipientAddress)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Simcontract *SimcontractTransactorSession) SetFeeRecipientAddress(recipientAddress common.Address) (*types.Transaction, error) {
	return _Simcontract.Contract.SetFeeRecipientAddress(&_Simcontract.TransactOpts, recipientAddress)
}

// SimcontractClusterLiquidatedIterator is returned from FilterClusterLiquidated and is used to iterate over the raw logs and unpacked data for ClusterLiquidated events raised by the Simcontract contract.
type SimcontractClusterLiquidatedIterator struct {
	Event *SimcontractClusterLiquidated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractClusterLiquidatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractClusterLiquidated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractClusterLiquidated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractClusterLiquidatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractClusterLiquidatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractClusterLiquidated represents a ClusterLiquidated event raised by the Simcontract contract.
type SimcontractClusterLiquidated struct {
	Owner       common.Address
	OperatorIds []uint64
	Cluster     CallableCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterLiquidated is a free log retrieval operation binding the contract event 0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688.
//
// Solidity: event ClusterLiquidated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) FilterClusterLiquidated(opts *bind.FilterOpts, owner []common.Address) (*SimcontractClusterLiquidatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "ClusterLiquidated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractClusterLiquidatedIterator{contract: _Simcontract.contract, event: "ClusterLiquidated", logs: logs, sub: sub}, nil
}

// WatchClusterLiquidated is a free log subscription operation binding the contract event 0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688.
//
// Solidity: event ClusterLiquidated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) WatchClusterLiquidated(opts *bind.WatchOpts, sink chan<- *SimcontractClusterLiquidated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "ClusterLiquidated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractClusterLiquidated)
				if err := _Simcontract.contract.UnpackLog(event, "ClusterLiquidated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseClusterLiquidated is a log parse operation binding the contract event 0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688.
//
// Solidity: event ClusterLiquidated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) ParseClusterLiquidated(log types.Log) (*SimcontractClusterLiquidated, error) {
	event := new(SimcontractClusterLiquidated)
	if err := _Simcontract.contract.UnpackLog(event, "ClusterLiquidated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractClusterReactivatedIterator is returned from FilterClusterReactivated and is used to iterate over the raw logs and unpacked data for ClusterReactivated events raised by the Simcontract contract.
type SimcontractClusterReactivatedIterator struct {
	Event *SimcontractClusterReactivated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractClusterReactivatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractClusterReactivated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractClusterReactivated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractClusterReactivatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractClusterReactivatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractClusterReactivated represents a ClusterReactivated event raised by the Simcontract contract.
type SimcontractClusterReactivated struct {
	Owner       common.Address
	OperatorIds []uint64
	Cluster     CallableCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterReactivated is a free log retrieval operation binding the contract event 0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859.
//
// Solidity: event ClusterReactivated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) FilterClusterReactivated(opts *bind.FilterOpts, owner []common.Address) (*SimcontractClusterReactivatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "ClusterReactivated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractClusterReactivatedIterator{contract: _Simcontract.contract, event: "ClusterReactivated", logs: logs, sub: sub}, nil
}

// WatchClusterReactivated is a free log subscription operation binding the contract event 0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859.
//
// Solidity: event ClusterReactivated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) WatchClusterReactivated(opts *bind.WatchOpts, sink chan<- *SimcontractClusterReactivated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "ClusterReactivated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractClusterReactivated)
				if err := _Simcontract.contract.UnpackLog(event, "ClusterReactivated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseClusterReactivated is a log parse operation binding the contract event 0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859.
//
// Solidity: event ClusterReactivated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) ParseClusterReactivated(log types.Log) (*SimcontractClusterReactivated, error) {
	event := new(SimcontractClusterReactivated)
	if err := _Simcontract.contract.UnpackLog(event, "ClusterReactivated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractFeeRecipientAddressUpdatedIterator is returned from FilterFeeRecipientAddressUpdated and is used to iterate over the raw logs and unpacked data for FeeRecipientAddressUpdated events raised by the Simcontract contract.
type SimcontractFeeRecipientAddressUpdatedIterator struct {
	Event *SimcontractFeeRecipientAddressUpdated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractFeeRecipientAddressUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractFeeRecipientAddressUpdated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractFeeRecipientAddressUpdated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractFeeRecipientAddressUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractFeeRecipientAddressUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractFeeRecipientAddressUpdated represents a FeeRecipientAddressUpdated event raised by the Simcontract contract.
type SimcontractFeeRecipientAddressUpdated struct {
	Owner            common.Address
	RecipientAddress common.Address
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterFeeRecipientAddressUpdated is a free log retrieval operation binding the contract event 0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548.
//
// Solidity: event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress)
func (_Simcontract *SimcontractFilterer) FilterFeeRecipientAddressUpdated(opts *bind.FilterOpts, owner []common.Address) (*SimcontractFeeRecipientAddressUpdatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "FeeRecipientAddressUpdated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractFeeRecipientAddressUpdatedIterator{contract: _Simcontract.contract, event: "FeeRecipientAddressUpdated", logs: logs, sub: sub}, nil
}

// WatchFeeRecipientAddressUpdated is a free log subscription operation binding the contract event 0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548.
//
// Solidity: event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress)
func (_Simcontract *SimcontractFilterer) WatchFeeRecipientAddressUpdated(opts *bind.WatchOpts, sink chan<- *SimcontractFeeRecipientAddressUpdated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "FeeRecipientAddressUpdated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractFeeRecipientAddressUpdated)
				if err := _Simcontract.contract.UnpackLog(event, "FeeRecipientAddressUpdated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFeeRecipientAddressUpdated is a log parse operation binding the contract event 0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548.
//
// Solidity: event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress)
func (_Simcontract *SimcontractFilterer) ParseFeeRecipientAddressUpdated(log types.Log) (*SimcontractFeeRecipientAddressUpdated, error) {
	event := new(SimcontractFeeRecipientAddressUpdated)
	if err := _Simcontract.contract.UnpackLog(event, "FeeRecipientAddressUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractOperatorAddedIterator is returned from FilterOperatorAdded and is used to iterate over the raw logs and unpacked data for OperatorAdded events raised by the Simcontract contract.
type SimcontractOperatorAddedIterator struct {
	Event *SimcontractOperatorAdded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractOperatorAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractOperatorAdded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractOperatorAdded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractOperatorAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractOperatorAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractOperatorAdded represents a OperatorAdded event raised by the Simcontract contract.
type SimcontractOperatorAdded struct {
	OperatorId uint64
	Owner      common.Address
	PublicKey  []byte
	Fee        *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorAdded is a free log retrieval operation binding the contract event 0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4.
//
// Solidity: event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee)
func (_Simcontract *SimcontractFilterer) FilterOperatorAdded(opts *bind.FilterOpts, operatorId []uint64, owner []common.Address) (*SimcontractOperatorAddedIterator, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "OperatorAdded", operatorIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractOperatorAddedIterator{contract: _Simcontract.contract, event: "OperatorAdded", logs: logs, sub: sub}, nil
}

// WatchOperatorAdded is a free log subscription operation binding the contract event 0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4.
//
// Solidity: event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee)
func (_Simcontract *SimcontractFilterer) WatchOperatorAdded(opts *bind.WatchOpts, sink chan<- *SimcontractOperatorAdded, operatorId []uint64, owner []common.Address) (event.Subscription, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "OperatorAdded", operatorIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractOperatorAdded)
				if err := _Simcontract.contract.UnpackLog(event, "OperatorAdded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOperatorAdded is a log parse operation binding the contract event 0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4.
//
// Solidity: event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee)
func (_Simcontract *SimcontractFilterer) ParseOperatorAdded(log types.Log) (*SimcontractOperatorAdded, error) {
	event := new(SimcontractOperatorAdded)
	if err := _Simcontract.contract.UnpackLog(event, "OperatorAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractOperatorRemovedIterator is returned from FilterOperatorRemoved and is used to iterate over the raw logs and unpacked data for OperatorRemoved events raised by the Simcontract contract.
type SimcontractOperatorRemovedIterator struct {
	Event *SimcontractOperatorRemoved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractOperatorRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractOperatorRemoved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractOperatorRemoved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractOperatorRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractOperatorRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractOperatorRemoved represents a OperatorRemoved event raised by the Simcontract contract.
type SimcontractOperatorRemoved struct {
	OperatorId uint64
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorRemoved is a free log retrieval operation binding the contract event 0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e.
//
// Solidity: event OperatorRemoved(uint64 indexed operatorId)
func (_Simcontract *SimcontractFilterer) FilterOperatorRemoved(opts *bind.FilterOpts, operatorId []uint64) (*SimcontractOperatorRemovedIterator, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "OperatorRemoved", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractOperatorRemovedIterator{contract: _Simcontract.contract, event: "OperatorRemoved", logs: logs, sub: sub}, nil
}

// WatchOperatorRemoved is a free log subscription operation binding the contract event 0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e.
//
// Solidity: event OperatorRemoved(uint64 indexed operatorId)
func (_Simcontract *SimcontractFilterer) WatchOperatorRemoved(opts *bind.WatchOpts, sink chan<- *SimcontractOperatorRemoved, operatorId []uint64) (event.Subscription, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "OperatorRemoved", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractOperatorRemoved)
				if err := _Simcontract.contract.UnpackLog(event, "OperatorRemoved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOperatorRemoved is a log parse operation binding the contract event 0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e.
//
// Solidity: event OperatorRemoved(uint64 indexed operatorId)
func (_Simcontract *SimcontractFilterer) ParseOperatorRemoved(log types.Log) (*SimcontractOperatorRemoved, error) {
	event := new(SimcontractOperatorRemoved)
	if err := _Simcontract.contract.UnpackLog(event, "OperatorRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractValidatorAddedIterator is returned from FilterValidatorAdded and is used to iterate over the raw logs and unpacked data for ValidatorAdded events raised by the Simcontract contract.
type SimcontractValidatorAddedIterator struct {
	Event *SimcontractValidatorAdded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractValidatorAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractValidatorAdded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractValidatorAdded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractValidatorAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractValidatorAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractValidatorAdded represents a ValidatorAdded event raised by the Simcontract contract.
type SimcontractValidatorAdded struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Shares      []byte
	Cluster     CallableCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorAdded is a free log retrieval operation binding the contract event 0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5.
//
// Solidity: event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) FilterValidatorAdded(opts *bind.FilterOpts, owner []common.Address) (*SimcontractValidatorAddedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "ValidatorAdded", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractValidatorAddedIterator{contract: _Simcontract.contract, event: "ValidatorAdded", logs: logs, sub: sub}, nil
}

// WatchValidatorAdded is a free log subscription operation binding the contract event 0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5.
//
// Solidity: event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) WatchValidatorAdded(opts *bind.WatchOpts, sink chan<- *SimcontractValidatorAdded, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "ValidatorAdded", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractValidatorAdded)
				if err := _Simcontract.contract.UnpackLog(event, "ValidatorAdded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseValidatorAdded is a log parse operation binding the contract event 0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5.
//
// Solidity: event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) ParseValidatorAdded(log types.Log) (*SimcontractValidatorAdded, error) {
	event := new(SimcontractValidatorAdded)
	if err := _Simcontract.contract.UnpackLog(event, "ValidatorAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractValidatorExitedIterator is returned from FilterValidatorExited and is used to iterate over the raw logs and unpacked data for ValidatorExited events raised by the Simcontract contract.
type SimcontractValidatorExitedIterator struct {
	Event *SimcontractValidatorExited // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractValidatorExitedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractValidatorExited)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractValidatorExited)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractValidatorExitedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractValidatorExitedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractValidatorExited represents a ValidatorExited event raised by the Simcontract contract.
type SimcontractValidatorExited struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorExited is a free log retrieval operation binding the contract event 0xb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c.
//
// Solidity: event ValidatorExited(address indexed owner, uint64[] operatorIds, bytes publicKey)
func (_Simcontract *SimcontractFilterer) FilterValidatorExited(opts *bind.FilterOpts, owner []common.Address) (*SimcontractValidatorExitedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "ValidatorExited", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractValidatorExitedIterator{contract: _Simcontract.contract, event: "ValidatorExited", logs: logs, sub: sub}, nil
}

// WatchValidatorExited is a free log subscription operation binding the contract event 0xb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c.
//
// Solidity: event ValidatorExited(address indexed owner, uint64[] operatorIds, bytes publicKey)
func (_Simcontract *SimcontractFilterer) WatchValidatorExited(opts *bind.WatchOpts, sink chan<- *SimcontractValidatorExited, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "ValidatorExited", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractValidatorExited)
				if err := _Simcontract.contract.UnpackLog(event, "ValidatorExited", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseValidatorExited is a log parse operation binding the contract event 0xb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c.
//
// Solidity: event ValidatorExited(address indexed owner, uint64[] operatorIds, bytes publicKey)
func (_Simcontract *SimcontractFilterer) ParseValidatorExited(log types.Log) (*SimcontractValidatorExited, error) {
	event := new(SimcontractValidatorExited)
	if err := _Simcontract.contract.UnpackLog(event, "ValidatorExited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SimcontractValidatorRemovedIterator is returned from FilterValidatorRemoved and is used to iterate over the raw logs and unpacked data for ValidatorRemoved events raised by the Simcontract contract.
type SimcontractValidatorRemovedIterator struct {
	Event *SimcontractValidatorRemoved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SimcontractValidatorRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimcontractValidatorRemoved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SimcontractValidatorRemoved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SimcontractValidatorRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimcontractValidatorRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimcontractValidatorRemoved represents a ValidatorRemoved event raised by the Simcontract contract.
type SimcontractValidatorRemoved struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Cluster     CallableCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorRemoved is a free log retrieval operation binding the contract event 0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e.
//
// Solidity: event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) FilterValidatorRemoved(opts *bind.FilterOpts, owner []common.Address) (*SimcontractValidatorRemovedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.FilterLogs(opts, "ValidatorRemoved", ownerRule)
	if err != nil {
		return nil, err
	}
	return &SimcontractValidatorRemovedIterator{contract: _Simcontract.contract, event: "ValidatorRemoved", logs: logs, sub: sub}, nil
}

// WatchValidatorRemoved is a free log subscription operation binding the contract event 0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e.
//
// Solidity: event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) WatchValidatorRemoved(opts *bind.WatchOpts, sink chan<- *SimcontractValidatorRemoved, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Simcontract.contract.WatchLogs(opts, "ValidatorRemoved", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimcontractValidatorRemoved)
				if err := _Simcontract.contract.UnpackLog(event, "ValidatorRemoved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseValidatorRemoved is a log parse operation binding the contract event 0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e.
//
// Solidity: event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Simcontract *SimcontractFilterer) ParseValidatorRemoved(log types.Log) (*SimcontractValidatorRemoved, error) {
	event := new(SimcontractValidatorRemoved)
	if err := _Simcontract.contract.UnpackLog(event, "ValidatorRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
