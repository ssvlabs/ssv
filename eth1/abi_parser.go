package eth1

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/bloxapp/ssv/eth1/abiparser"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	contractABI = `[{"inputs":[],"name":"ApprovalNotWithinTimeframe","type":"error"},{"inputs":[],"name":"BurnRatePositive","type":"error"},{"inputs":[],"name":"CallerNotOwner","type":"error"},{"inputs":[],"name":"FeeExceedsIncreaseLimit","type":"error"},{"inputs":[],"name":"FeeTooLow","type":"error"},{"inputs":[],"name":"InvalidPublicKeyLength","type":"error"},{"inputs":[],"name":"NegativeBalance","type":"error"},{"inputs":[],"name":"NoPendingFeeChangeRequest","type":"error"},{"inputs":[],"name":"NotEnoughBalance","type":"error"},{"inputs":[],"name":"OperatorDoesNotExist","type":"error"},{"inputs":[],"name":"OperatorIdsStructureInvalid","type":"error"},{"inputs":[],"name":"OperatorNotFound","type":"error"},{"inputs":[],"name":"OperatorWithPublicKeyNotExist","type":"error"},{"inputs":[],"name":"OperatorsListDoesNotSorted","type":"error"},{"inputs":[],"name":"ParametersMismatch","type":"error"},{"inputs":[],"name":"PodAlreadyEnabled","type":"error"},{"inputs":[],"name":"PodDataIsBroken","type":"error"},{"inputs":[],"name":"PodIsLiquidated","type":"error"},{"inputs":[],"name":"PodLiquidatable","type":"error"},{"inputs":[],"name":"PodNotExists","type":"error"},{"inputs":[],"name":"PodNotLiquidatable","type":"error"},{"inputs":[],"name":"ValidatorAlreadyExists","type":"error"},{"inputs":[],"name":"ValidatorNotOwned","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"DeclareOperatorFeePeriodUpdate","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"DeclaredOperatorFeeCancelation","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"ExecuteOperatorFeePeriodUpdate","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":false,"internalType":"bytes32","name":"hashedPod","type":"bytes32"},{"indexed":false,"internalType":"address","name":"owner","type":"address"}],"name":"FundsDeposit","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"oldFee","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"newFee","type":"uint256"}],"name":"NetworkFeeUpdate","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":false,"internalType":"address","name":"recipient","type":"address"}],"name":"NetworkFeesWithdrawal","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint64","name":"id","type":"uint64"},{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64","name":"operatorId","type":"uint64"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorFeeDeclaration","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64","name":"operatorId","type":"uint64"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorFeeExecution","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint64","name":"value","type":"uint64"}],"name":"OperatorFeeIncreaseLimitUpdate","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint64","name":"id","type":"uint64"},{"indexed":false,"internalType":"uint64","name":"fee","type":"uint64"}],"name":"OperatorFeeSet","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":false,"internalType":"uint64","name":"operatorId","type":"uint64"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"}],"name":"OperatorFundsWithdrawal","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint64","name":"id","type":"uint64"}],"name":"OperatorRemoved","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"previousOwner","type":"address"},{"indexed":true,"internalType":"address","name":"newOwner","type":"address"}],"name":"OwnershipTransferred","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"indexed":false,"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"PodEnabled","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"PodFundsWithdrawal","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"indexed":false,"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"PodLiquidated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"indexed":false,"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"PodMetadataUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"bytes[]","name":"sharePublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"indexed":false,"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"ValidatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"indexed":false,"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"ValidatorRemoved","type":"event"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"cancelDeclaredOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"declareOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"internalType":"uint256","name":"amount","type":"uint256"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"deposit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"internalType":"uint256","name":"amount","type":"uint256"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"deposit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"executeOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getDeclaredOperatorFeePeriod","outputs":[{"internalType":"uint64","name":"","type":"uint64"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getExecuteOperatorFeePeriod","outputs":[{"internalType":"uint64","name":"","type":"uint64"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNetworkBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNetworkFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"getOperatorDeclaredFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"getOperatorFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getOperatorFeeIncreaseLimit","outputs":[{"internalType":"uint64","name":"","type":"uint64"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"contract IERC20","name":"token_","type":"address"},{"internalType":"uint64","name":"operatorMaxFeeIncrease_","type":"uint64"},{"internalType":"uint64","name":"declareOperatorFeePeriod_","type":"uint64"},{"internalType":"uint64","name":"executeOperatorFeePeriod_","type":"uint64"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"isLiquidatable","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"isLiquidated","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"liquidatePod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"id","type":"uint64"}],"name":"operatorSnapshot","outputs":[{"internalType":"uint64","name":"currentBlock","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint256","name":"balance","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"owner","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"podBalanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"internalType":"uint256","name":"amount","type":"uint256"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"reactivatePod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"encryptionPK","type":"bytes"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"registerOperator","outputs":[{"internalType":"uint64","name":"id","type":"uint64"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"internalType":"bytes[]","name":"sharePublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"internalType":"uint256","name":"amount","type":"uint256"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"registerValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"removeOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"removeValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"renounceOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newOwner","type":"address"}],"name":"transferOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"newDeclareOperatorFeePeriod","type":"uint64"}],"name":"updateDeclareOperatorFeePeriod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"newExecuteOperatorFeePeriod","type":"uint64"}],"name":"updateExecuteOperatorFeePeriod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"updateNetworkFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"newOperatorMaxFeeIncrease","type":"uint64"}],"name":"updateOperatorFeeIncreaseLimit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdrawDAOEarnings","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdrawOperatorBalance","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64","name":"operatorId","type":"uint64"}],"name":"withdrawOperatorBalance","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint64[]","name":"operatorIds","type":"uint64[]"},{"internalType":"uint256","name":"amount","type":"uint256"},{"components":[{"internalType":"uint32","name":"validatorCount","type":"uint32"},{"internalType":"uint64","name":"networkFee","type":"uint64"},{"internalType":"uint64","name":"networkFeeIndex","type":"uint64"},{"internalType":"uint64","name":"index","type":"uint64"},{"internalType":"uint64","name":"balance","type":"uint64"},{"internalType":"bool","name":"disabled","type":"bool"}],"internalType":"struct ISSVNetwork.Pod","name":"pod","type":"tuple"}],"name":"withdrawPodBalance","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
)

// Version enum to support more than one abi format
type Version int64

// Version types
const (
	V2 Version = iota
)

func (v Version) String() string {
	switch v {
	case V2:
		return "v2"
	}
	return "v2"
}

// AbiParser serves as a parsing client for events from contract
type AbiParser struct {
	Version AbiVersion
}

// NewParser return parser client based on the contract version
func NewParser(logger *zap.Logger, version Version) AbiParser {
	var parserVersion AbiVersion
	switch version {
	case V2:
		parserVersion = &abiparser.AbiV2{}
	}
	return AbiParser{Version: parserVersion}
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (ap AbiParser) ParseOperatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorAddedEvent, error) {
	return ap.Version.ParseOperatorAddedEvent(log, contractAbi)
}

// ParseOperatorRemovedEvent parses an OperatorRemovedEvent
func (ap AbiParser) ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorRemovedEvent, error) {
	return ap.Version.ParseOperatorRemovedEvent(log, contractAbi)
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (ap AbiParser) ParseValidatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorAddedEvent, error) {
	return ap.Version.ParseValidatorAddedEvent(log, contractAbi)
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (ap AbiParser) ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorRemovedEvent, error) {
	return ap.Version.ParseValidatorRemovedEvent(log, contractAbi)
}

// ParsePodLiquidatedEvent parses PodLiquidatedEvent
func (ap AbiParser) ParsePodLiquidatedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.PodLiquidatedEvent, error) {
	return ap.Version.ParsePodLiquidatedEvent(log, contractAbi)
}

// ParsePodEnabledEvent parses PodEnabledEvent
func (ap AbiParser) ParsePodEnabledEvent(log types.Log, contractAbi abi.ABI) (*abiparser.PodEnabledEvent, error) {
	return ap.Version.ParsePodEnabledEvent(log, contractAbi)
}

// ParseFeeRecipientAddressAddedEvent parses FeeRecipientAddressAddedEvent
func (ap AbiParser) ParseFeeRecipientAddressAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.FeeRecipientAddressAddedEvent, error) {
	return ap.Version.ParseFeeRecipientAddressAddedEvent(log, contractAbi)
}

// AbiVersion serves as the parser client interface
type AbiVersion interface {
	ParseOperatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorAddedEvent, error)
	ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorRemovedEvent, error)
	ParseValidatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorAddedEvent, error)
	ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorRemovedEvent, error)
	ParsePodLiquidatedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.PodLiquidatedEvent, error)
	ParsePodEnabledEvent(log types.Log, contractAbi abi.ABI) (*abiparser.PodEnabledEvent, error)
	ParseFeeRecipientAddressAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.FeeRecipientAddressAddedEvent, error)
}

// LoadABI enables to load a custom abi json
func LoadABI(logger *zap.Logger, abiFilePath string) error {
	jsonFile, err := os.Open(filepath.Clean(abiFilePath))
	if err != nil {
		return errors.Wrap(err, "failed to open abi")
	}

	raw, err := io.ReadAll(jsonFile)
	if err != nil {
		return errors.Wrap(err, "failed to read abi")
	}
	if err := jsonFile.Close(); err != nil {
		logger.Warn("failed to close abi json", zap.Error(err))
	}
	s := string(raw)

	if err := jsonFile.Close(); err != nil {
		logger.Warn("failed to close abi json", zap.Error(err))
	}

	// assert valid JSON
	var obj []interface{}
	err = json.Unmarshal(raw, &obj)
	if err != nil {
		return errors.Wrap(err, "abi is not a valid json")
	}
	contractABI = s
	return nil
}

// ContractABI abi of the ssv-network contract
func ContractABI(abiVersion Version) string {
	switch abiVersion {
	case V2:
		return contractABI
	default:
		return contractABI
	}
}
