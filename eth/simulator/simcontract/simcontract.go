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
	Bin: "0x608060405260008060006101000a81548167ffffffffffffffff021916908367ffffffffffffffff16021790555034801561003957600080fd5b50611073806100496000396000f3fe608060405234801561001057600080fd5b50600436106100885760003560e01c80635fec6dd01161005b5780635fec6dd0146100fd578063bf0f2fb214610119578063dbcdc2cc14610135578063ff212c5c1461015157610088565b806306e8fb9c1461008d57806312b3fc19146100a95780632e168e0e146100c55780633877322b146100e1575b600080fd5b6100a760048036038101906100a291906107be565b61016d565b005b6100c360048036038101906100be91906108ed565b6101ce565b005b6100df60048036038101906100da9190610982565b61022b565b005b6100fb60048036038101906100f691906109af565b610265565b005b61011760048036038101906101129190610a2b565b6102bc565b005b610133600480360381019061012e9190610afd565b610314565b005b61014f600480360381019061014a9190610b6c565b610369565b005b61016b60048036038101906101669190610b99565b6103ba565b005b3373ffffffffffffffffffffffffffffffffffffffff167f48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e58689898888876040516101bd96959493929190610d99565b60405180910390a250505050505050565b3373ffffffffffffffffffffffffffffffffffffffff167fccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e848488888660405161021c959493929190610e83565b60405180910390a25050505050565b8067ffffffffffffffff167f0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e60405160405180910390a250565b3373ffffffffffffffffffffffffffffffffffffffff167fb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c8285856040516102af93929190610ecc565b60405180910390a2505050565b3373ffffffffffffffffffffffffffffffffffffffff167fc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b685985858460405161030693929190610f05565b60405180910390a250505050565b3373ffffffffffffffffffffffffffffffffffffffff167f1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688838360405161035c929190610f37565b60405180910390a2505050565b3373ffffffffffffffffffffffffffffffffffffffff167f259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548826040516103af9190610f76565b60405180910390a250565b60016000808282829054906101000a900467ffffffffffffffff166103df9190610fc0565b92506101000a81548167ffffffffffffffff021916908367ffffffffffffffff1602179055503373ffffffffffffffffffffffffffffffffffffffff1660008054906101000a900467ffffffffffffffff1667ffffffffffffffff167fd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f485858560405161046e9392919061100b565b60405180910390a3505050565b6000604051905090565b600080fd5b600080fd5b600080fd5b600080fd5b600080fd5b60008083601f8401126104b4576104b361048f565b5b8235905067ffffffffffffffff8111156104d1576104d0610494565b5b6020830191508360018202830111156104ed576104ec610499565b5b9250929050565b6000601f19601f8301169050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b61053d826104f4565b810181811067ffffffffffffffff8211171561055c5761055b610505565b5b80604052505050565b600061056f61047b565b905061057b8282610534565b919050565b600067ffffffffffffffff82111561059b5761059a610505565b5b602082029050602081019050919050565b600067ffffffffffffffff82169050919050565b6105c9816105ac565b81146105d457600080fd5b50565b6000813590506105e6816105c0565b92915050565b60006105ff6105fa84610580565b610565565b9050808382526020820190506020840283018581111561062257610621610499565b5b835b8181101561064b578061063788826105d7565b845260208401935050602081019050610624565b5050509392505050565b600082601f83011261066a5761066961048f565b5b813561067a8482602086016105ec565b91505092915050565b6000819050919050565b61069681610683565b81146106a157600080fd5b50565b6000813590506106b38161068d565b92915050565b600080fd5b600063ffffffff82169050919050565b6106d7816106be565b81146106e257600080fd5b50565b6000813590506106f4816106ce565b92915050565b60008115159050919050565b61070f816106fa565b811461071a57600080fd5b50565b60008135905061072c81610706565b92915050565b600060a08284031215610748576107476106b9565b5b61075260a0610565565b90506000610762848285016106e5565b6000830152506020610776848285016105d7565b602083015250604061078a848285016105d7565b604083015250606061079e8482850161071d565b60608301525060806107b2848285016106a4565b60808301525092915050565b6000806000806000806000610120888a0312156107de576107dd610485565b5b600088013567ffffffffffffffff8111156107fc576107fb61048a565b5b6108088a828b0161049e565b9750975050602088013567ffffffffffffffff81111561082b5761082a61048a565b5b6108378a828b01610655565b955050604088013567ffffffffffffffff8111156108585761085761048a565b5b6108648a828b0161049e565b945094505060606108778a828b016106a4565b92505060806108888a828b01610732565b91505092959891949750929550565b60008083601f8401126108ad576108ac61048f565b5b8235905067ffffffffffffffff8111156108ca576108c9610494565b5b6020830191508360208202830111156108e6576108e5610499565b5b9250929050565b600080600080600060e0868803121561090957610908610485565b5b600086013567ffffffffffffffff8111156109275761092661048a565b5b6109338882890161049e565b9550955050602086013567ffffffffffffffff8111156109565761095561048a565b5b61096288828901610897565b9350935050604061097588828901610732565b9150509295509295909350565b60006020828403121561099857610997610485565b5b60006109a6848285016105d7565b91505092915050565b6000806000604084860312156109c8576109c7610485565b5b600084013567ffffffffffffffff8111156109e6576109e561048a565b5b6109f28682870161049e565b9350935050602084013567ffffffffffffffff811115610a1557610a1461048a565b5b610a2186828701610655565b9150509250925092565b60008060008060e08587031215610a4557610a44610485565b5b600085013567ffffffffffffffff811115610a6357610a6261048a565b5b610a6f87828801610897565b94509450506020610a82878288016106a4565b9250506040610a9387828801610732565b91505092959194509250565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610aca82610a9f565b9050919050565b610ada81610abf565b8114610ae557600080fd5b50565b600081359050610af781610ad1565b92915050565b600080600060e08486031215610b1657610b15610485565b5b6000610b2486828701610ae8565b935050602084013567ffffffffffffffff811115610b4557610b4461048a565b5b610b5186828701610655565b9250506040610b6286828701610732565b9150509250925092565b600060208284031215610b8257610b81610485565b5b6000610b9084828501610ae8565b91505092915050565b600080600060408486031215610bb257610bb1610485565b5b600084013567ffffffffffffffff811115610bd057610bcf61048a565b5b610bdc8682870161049e565b93509350506020610bef868287016106a4565b9150509250925092565b600081519050919050565b600082825260208201905092915050565b6000819050602082019050919050565b610c2e816105ac565b82525050565b6000610c408383610c25565b60208301905092915050565b6000602082019050919050565b6000610c6482610bf9565b610c6e8185610c04565b9350610c7983610c15565b8060005b83811015610caa578151610c918882610c34565b9750610c9c83610c4c565b925050600181019050610c7d565b5085935050505092915050565b600082825260208201905092915050565b82818337600083830152505050565b6000610ce38385610cb7565b9350610cf0838584610cc8565b610cf9836104f4565b840190509392505050565b610d0d816106be565b82525050565b610d1c816106fa565b82525050565b610d2b81610683565b82525050565b60a082016000820151610d476000850182610d04565b506020820151610d5a6020850182610c25565b506040820151610d6d6040850182610c25565b506060820151610d806060850182610d13565b506080820151610d936080850182610d22565b50505050565b6000610100820190508181036000830152610db48189610c59565b90508181036020830152610dc9818789610cd7565b90508181036040830152610dde818587610cd7565b9050610ded6060830184610d31565b979650505050505050565b6000819050919050565b6000610e1160208401846105d7565b905092915050565b6000602082019050919050565b6000610e328385610c04565b9350610e3d82610df8565b8060005b85811015610e7657610e538284610e02565b610e5d8882610c34565b9750610e6883610e19565b925050600181019050610e41565b5085925050509392505050565b600060e0820190508181036000830152610e9e818789610e26565b90508181036020830152610eb3818587610cd7565b9050610ec26040830184610d31565b9695505050505050565b60006040820190508181036000830152610ee68186610c59565b90508181036020830152610efb818486610cd7565b9050949350505050565b600060c0820190508181036000830152610f20818587610e26565b9050610f2f6020830184610d31565b949350505050565b600060c0820190508181036000830152610f518185610c59565b9050610f606020830184610d31565b9392505050565b610f7081610abf565b82525050565b6000602082019050610f8b6000830184610f67565b92915050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b6000610fcb826105ac565b9150610fd6836105ac565b9250828201905067ffffffffffffffff811115610ff657610ff5610f91565b5b92915050565b61100581610683565b82525050565b60006040820190508181036000830152611026818587610cd7565b90506110356020830184610ffc565b94935050505056fea26469706673582212202444d9ec497e6a7489032ee6ac45705fd2068fe67e242d0d1d98f080df138fde64736f6c63430008120033",
}

// SimcontractABI is the input ABI used to generate the binding from.
// Deprecated: Use SimcontractMetaData.ABI instead.
var SimcontractABI = SimcontractMetaData.ABI

// SimcontractBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use SimcontractMetaData.Bin instead.
var SimcontractBin = SimcontractMetaData.Bin

// DeploySimcontract deploys a new Ethereum contract, binding an instance of Simcontract to it.
func DeploySimcontract(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *Simcontract, error) {
	parsed, err := SimcontractMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(SimcontractBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &Simcontract{SimcontractCaller: SimcontractCaller{contract: contract}, SimcontractTransactor: SimcontractTransactor{contract: contract}, SimcontractFilterer: SimcontractFilterer{contract: contract}}, nil
}

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
