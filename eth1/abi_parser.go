package eth1

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/utils/logex"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Abi's to use
var (
	contractABI   = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"bytes","name":"validatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"index","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"name":"OessAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"components":[{"internalType":"uint256","name":"index","type":"uint256"},{"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"indexed":false,"internalType":"struct ISSVNetwork.Oess[]","name":"oessList","type":"tuple[]"}],"name":"ValidatorAdded","type":"event"},{"inputs":[{"internalType":"string","name":"_name","type":"string"},{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"}],"name":"addOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"},{"internalType":"bytes[]","name":"_operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_encryptedKeys","type":"bytes[]"}],"name":"addValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"operatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"operators","outputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"address","name":"ownerAddress","type":"address"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"score","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"validatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
	ContractAbiV2 = `[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"}],"name":"AccountEnabled","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"}],"name":"AccountLiquidated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"ApproveOperatorFeePeriodUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"address","name":"senderAddress","type":"address"}],"name":"FundsDeposited","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"}],"name":"FundsWithdrawn","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"LiquidationThresholdPeriodUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"oldFee","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"newFee","type":"uint256"}],"name":"NetworkFeeUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"},{"indexed":false,"internalType":"address","name":"recipient","type":"address"}],"name":"NetworkFeesWithdrawn","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"OperatorActivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"id","type":"uint256"},{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"OperatorDeactivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorFeeApproved","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"OperatorFeeIncreaseLimitUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorFeeSet","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"OperatorFeeSetCanceled","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"OperatorRemoved","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":true,"internalType":"uint256","name":"operatorId","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"score","type":"uint256"}],"name":"OperatorScoreUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"previousOwner","type":"address"},{"indexed":true,"internalType":"address","name":"newOwner","type":"address"}],"name":"OwnershipTransferred","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"RegisteredOperatorsPerAccountLimitUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"SetOperatorFeePeriodUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256[]","name":"operatorIds","type":"uint256[]"},{"indexed":false,"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"}],"name":"ValidatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"ValidatorRemoved","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256[]","name":"operatorIds","type":"uint256[]"},{"indexed":false,"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"}],"name":"ValidatorUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"ValidatorsPerOperatorLimitUpdated","type":"event"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"addressNetworkFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"cancelDeclaredOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"declareOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"deposit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"executeOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"getAddressBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"getAddressBurnRate","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getDeclareOperatorFeePeriod","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getExecuteOperatorFeePeriod","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getLiquidationThresholdPeriod","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNetworkFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNetworkTreasury","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"getOperatorById","outputs":[{"internalType":"string","name":"","type":"string"},{"internalType":"address","name":"","type":"address"},{"internalType":"bytes","name":"","type":"bytes"},{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"getOperatorByPublicKey","outputs":[{"internalType":"string","name":"","type":"string"},{"internalType":"address","name":"","type":"address"},{"internalType":"bytes","name":"","type":"bytes"},{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"getOperatorDeclaredFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"getOperatorFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getOperatorFeeIncreaseLimit","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"getOperatorsByValidator","outputs":[{"internalType":"uint256[]","name":"","type":"uint256[]"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRegisteredOperatorsPerAccountLimit","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"getValidatorsByOwnerAddress","outputs":[{"internalType":"bytes[]","name":"","type":"bytes[]"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getValidatorsPerOperatorLimit","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"contract ISSVRegistry","name":"registryAddress_","type":"address"},{"internalType":"contract IERC20","name":"token_","type":"address"},{"internalType":"uint256","name":"minimumBlocksBeforeLiquidation_","type":"uint256"},{"internalType":"uint256","name":"operatorMaxFeeIncrease_","type":"uint256"},{"internalType":"uint256","name":"setOperatorFeePeriod_","type":"uint256"},{"internalType":"uint256","name":"approveOperatorFeePeriod_","type":"uint256"},{"internalType":"uint256","name":"validatorsPerOperatorLimit_","type":"uint256"},{"internalType":"uint256","name":"registeredOperatorsPerAccountLimit_","type":"uint256"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"isLiquidatable","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"isLiquidated","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address[]","name":"ownerAddresses","type":"address[]"}],"name":"liquidate","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"owner","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"reactivateAccount","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"registerOperator","outputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256[]","name":"operatorIds","type":"uint256[]"},{"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"registerValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"}],"name":"removeOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"removeValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"renounceOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newOwner","type":"address"}],"name":"transferOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"newSetOperatorFeePeriod","type":"uint256"}],"name":"updateDeclareOperatorFeePeriod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"newApproveOperatorFeePeriod","type":"uint256"}],"name":"updateExecuteOperatorFeePeriod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"newMinimumBlocksBeforeLiquidation","type":"uint256"}],"name":"updateLiquidationThresholdPeriod","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"updateNetworkFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"newOperatorMaxFeeIncrease","type":"uint256"}],"name":"updateOperatorFeeIncreaseLimit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId","type":"uint256"},{"internalType":"uint256","name":"score","type":"uint256"}],"name":"updateOperatorScore","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"registeredOperatorsPerAccountLimit_","type":"uint256"}],"name":"updateRegisteredOperatorsPerAccountLimit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256[]","name":"operatorIds","type":"uint256[]"},{"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"updateValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"validatorsPerOperatorLimit_","type":"uint256"}],"name":"updateValidatorsPerOperatorLimit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorId_","type":"uint256"}],"name":"validatorsPerOperatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"withdrawAll","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdrawNetworkEarnings","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
)

// Version enum to support more than one abi format
type Version int64

// Version types
const (
	Legacy Version = iota
	V2
)

func (v Version) String() string {
	switch v {
	case Legacy:
		return "legacy"
	case V2:
		return "v2"
	}
	return "legacy"
}

// AbiParser serves as a parsing client for events from contract
type AbiParser struct {
	Logger  *zap.Logger
	Version AbiVersion
}

// NewParser return parser client based on the contract version
func NewParser(logger *zap.Logger, version Version) AbiParser {
	var parserVersion AbiVersion
	switch version {
	case Legacy:
		parserVersion = abiparser.AdapterLegacy{}
	case V2:
		parserVersion = &abiparser.AbiV2{}
	}
	return AbiParser{Logger: logger, Version: parserVersion}
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (ap AbiParser) ParseOperatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorAddedEvent, error) {
	return ap.Version.ParseOperatorAddedEvent(ap.Logger, log, contractAbi)
}

// ParseOperatorRemovedEvent parses an OperatorRemovedEvent
func (ap AbiParser) ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.OperatorRemovedEvent, error) {
	return ap.Version.ParseOperatorRemovedEvent(ap.Logger, log, contractAbi)
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (ap AbiParser) ParseValidatorAddedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorAddedEvent, error) {
	return ap.Version.ParseValidatorAddedEvent(ap.Logger, log, contractAbi)
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (ap AbiParser) ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorRemovedEvent, error) {
	return ap.Version.ParseValidatorRemovedEvent(ap.Logger, log, contractAbi)
}

// ParseAccountLiquidatedEvent parses AccountLiquidatedEvent
func (ap AbiParser) ParseAccountLiquidatedEvent(log types.Log) (*abiparser.AccountLiquidatedEvent, error) {
	return ap.Version.ParseAccountLiquidatedEvent(log)
}

// ParseAccountEnabledEvent parses AccountEnabledEvent
func (ap AbiParser) ParseAccountEnabledEvent(log types.Log) (*abiparser.AccountEnabledEvent, error) {
	return ap.Version.ParseAccountEnabledEvent(log)
}

// AbiVersion serves as the parser client interface
type AbiVersion interface {
	ParseOperatorAddedEvent(logger *zap.Logger, log types.Log, contractAbi abi.ABI) (*abiparser.OperatorAddedEvent, error)
	ParseOperatorRemovedEvent(logger *zap.Logger, log types.Log, contractAbi abi.ABI) (*abiparser.OperatorRemovedEvent, error)
	ParseValidatorAddedEvent(logger *zap.Logger, log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorAddedEvent, error)
	ParseValidatorRemovedEvent(logger *zap.Logger, log types.Log, contractAbi abi.ABI) (*abiparser.ValidatorRemovedEvent, error)
	ParseAccountLiquidatedEvent(log types.Log) (*abiparser.AccountLiquidatedEvent, error)
	ParseAccountEnabledEvent(log types.Log) (*abiparser.AccountEnabledEvent, error)
}

// LoadABI enables to load a custom abi json
func LoadABI(abiFilePath string) error {
	jsonFile, err := os.Open(filepath.Clean(abiFilePath))
	if err != nil {
		return errors.Wrap(err, "failed to open abi")
	}

	raw, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return errors.Wrap(err, "failed to read abi")
	}
	if err := jsonFile.Close(); err != nil {
		logex.GetLogger().Warn("failed to close abi json", zap.Error(err))
	}
	s := string(raw)

	if err := jsonFile.Close(); err != nil {
		logex.GetLogger().Warn("failed to close abi json", zap.Error(err))
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
	case Legacy:
		return contractABI
	case V2:
		return ContractAbiV2
	default:
		return contractABI
	}
}
