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

// ISSVNetworkCoreCluster is an auto generated low-level Go binding around an user-defined struct.
type ISSVNetworkCoreCluster struct {
	ValidatorCount  uint32
	NetworkFeeIndex uint64
	Index           uint64
	Active          bool
	Balance         *big.Int
}

// ISSVNetworkNetworkInitParams is an auto generated low-level Go binding around an user-defined struct.
type ISSVNetworkNetworkInitParams struct {
	MinimumBlocksBeforeLiquidation uint64
	MinimumLiquidationCollateral   *big.Int
	ValidatorsPerOperatorLimit     uint32
	DeclareOperatorFeePeriod       uint64
	ExecuteOperatorFeePeriod       uint64
	OperatorMaxFeeIncrease         uint64
	DefaultOracleIds               [4]uint32
	QuorumBps                      uint16
}

// ContractMetaData contains all meta data concerning the Contract contract.
var ContractMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"contractAddress\",\"type\":\"address\"}],\"name\":\"AddressIsWhitelistingContract\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AlreadyVoted\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ApprovalNotWithinTimeframe\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"CallerNotOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"CallerNotOwnerWithData\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"CallerNotWhitelisted\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"CallerNotWhitelistedWithData\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ClusterAlreadyEnabled\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ClusterDoesNotExist\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ClusterIsLiquidated\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ClusterNotLiquidatable\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"EBBelowMinimum\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"EBExceedsMaximum\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ETHTransferFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"EmptyPublicKeysList\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"ExceedValidatorLimit\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"ExceedValidatorLimitWithData\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FeeExceedsIncreaseLimit\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FeeIncreaseNotAllowed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FeeTooHigh\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FeeTooLow\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FutureBlockNumber\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"IncorrectClusterState\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"IncorrectClusterVersion\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"operatorVersion\",\"type\":\"uint8\"}],\"name\":\"IncorrectOperatorVersion\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"IncorrectValidatorState\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"}],\"name\":\"IncorrectValidatorStateWithData\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InsufficientBalance\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InsufficientCSSVSupply\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidOperatorFeeIncreaseLimit\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidOperatorFeeRange\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidOperatorIdsLength\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidOracleId\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidProof\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidPublicKeyLength\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidQuorum\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidToken\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidWhitelistAddressesLength\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"contractAddress\",\"type\":\"address\"}],\"name\":\"InvalidWhitelistingContract\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"LegacyOperatorFeeDeclarationInvalid\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MaxPrecisionExceeded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MaxRequestsAmountReached\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MaxValueExceeded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MustUseLatestRoot\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NewBlockPeriodIsBelowMinimum\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoFeeDeclared\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotCSSV\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotOracle\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NothingToClaim\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NothingToWithdraw\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"OperatorAlreadyExists\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"OperatorDoesNotExist\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"OperatorsListNotUnique\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"OracleAlreadyAssigned\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"PublicKeysSharesLengthMismatch\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"RootNotFound\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"SameFeeChangeNotAllowed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"SameOracleAddressNotAllowed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"StakeTooLow\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"StaleBlockNumber\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"StaleUpdate\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TargetModuleDoesNotExist\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"moduleId\",\"type\":\"uint8\"}],\"name\":\"TargetModuleDoesNotExistWithData\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TokenTransferFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnsortedOperatorsList\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnstakeAmountExceedsBalance\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UpdateTooFrequent\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ValidatorAlreadyExists\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"}],\"name\":\"ValidatorAlreadyExistsWithData\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"ValidatorAlreadyRegistered\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ValidatorDoesNotExist\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddressNotAllowed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAmount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroCSSVSupply\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"previousAdmin\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"newAdmin\",\"type\":\"address\"}],\"name\":\"AdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"beacon\",\"type\":\"address\"}],\"name\":\"BeaconUpgraded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"blockNum\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"effectiveBalance\",\"type\":\"uint32\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterBalanceUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterDeposited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterLiquidated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"ethDeposited\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"ssvRefunded\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"effectiveBalance\",\"type\":\"uint32\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterMigratedToETH\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterReactivated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ClusterWithdrawn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"newCooldownDuration\",\"type\":\"uint64\"}],\"name\":\"CooldownDurationUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"DeclareOperatorFeePeriodUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"ERC20Rescued\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"ExecuteOperatorFeePeriodUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipientAddress\",\"type\":\"address\"}],\"name\":\"FeeRecipientAddressUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newFeesWei\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"accEthPerShare\",\"type\":\"uint256\"}],\"name\":\"FeesSynced\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"version\",\"type\":\"uint8\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"LiquidationThresholdPeriodSSVUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"LiquidationThresholdPeriodUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"newMinBlocksBetweenUpdates\",\"type\":\"uint32\"}],\"name\":\"MinBlocksBetweenUpdatesUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"MinimumLiquidationCollateralSSVUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"MinimumLiquidationCollateralUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"minFee\",\"type\":\"uint256\"}],\"name\":\"MinimumOperatorEthFeeUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"enumSSVModules\",\"name\":\"moduleId\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"moduleAddress\",\"type\":\"address\"}],\"name\":\"ModuleUpgraded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"}],\"name\":\"NetworkEarningsWithdrawn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldFee\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newFee\",\"type\":\"uint256\"}],\"name\":\"NetworkFeeUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldFee\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newFee\",\"type\":\"uint256\"}],\"name\":\"NetworkFeeUpdatedSSV\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"OperatorAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"OperatorFeeDeclarationCancelled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"OperatorFeeDeclared\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"OperatorFeeExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"name\":\"OperatorFeeIncreaseLimitUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"maxFee\",\"type\":\"uint256\"}],\"name\":\"OperatorMaximumFeeUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"whitelistAddresses\",\"type\":\"address[]\"}],\"name\":\"OperatorMultipleWhitelistRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"whitelistAddresses\",\"type\":\"address[]\"}],\"name\":\"OperatorMultipleWhitelistUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"toPrivate\",\"type\":\"bool\"}],\"name\":\"OperatorPrivacyStatusUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"OperatorRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"whitelisted\",\"type\":\"address\"}],\"name\":\"OperatorWhitelistUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"whitelistingContract\",\"type\":\"address\"}],\"name\":\"OperatorWhitelistingContractUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"OperatorWithdrawn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"OperatorWithdrawnSSV\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"oracleId\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"oldOracle\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOracle\",\"type\":\"address\"}],\"name\":\"OracleReplaced\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferStarted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint16\",\"name\":\"newQuorum\",\"type\":\"uint16\"}],\"name\":\"QuorumUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"RewardsClaimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"pending\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"accrued\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"userIndex\",\"type\":\"uint256\"}],\"name\":\"RewardsSettled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"merkleRoot\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"blockNum\",\"type\":\"uint64\"}],\"name\":\"RootCommitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"string\",\"name\":\"version\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"SSVNetworkUpgradeBlock\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Staked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"unlockTime\",\"type\":\"uint256\"}],\"name\":\"UnstakeRequested\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"UnstakedWithdrawn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"shares\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ValidatorAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"}],\"name\":\"ValidatorExited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"indexed\":false,\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"ValidatorRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"merkleRoot\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"blockNum\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"accumulatedWeight\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"quorum\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"oracleId\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"oracle\",\"type\":\"address\"}],\"name\":\"WeightedRootProposed\",\"type\":\"event\"},{\"stateMutability\":\"nonpayable\",\"type\":\"fallback\"},{\"inputs\":[],\"name\":\"acceptOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[]\",\"name\":\"publicKeys\",\"type\":\"bytes[]\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"bulkExitValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[]\",\"name\":\"publicKeys\",\"type\":\"bytes[]\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"bytes[]\",\"name\":\"sharesData\",\"type\":\"bytes[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"bulkRegisterValidator\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[]\",\"name\":\"publicKeys\",\"type\":\"bytes[]\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"bulkRemoveValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"cancelDeclaredOperatorFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"claimEthRewards\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"merkleRoot\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"blockNum\",\"type\":\"uint64\"}],\"name\":\"commitRoot\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"declareOperatorFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"clusterOwner\",\"type\":\"address\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"deposit\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"executeOperatorFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"exitValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getVersion\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"version\",\"type\":\"string\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"token_\",\"type\":\"address\"},{\"internalType\":\"contractISSVOperators\",\"name\":\"ssvOperators_\",\"type\":\"address\"},{\"internalType\":\"contractISSVClusters\",\"name\":\"ssvClusters_\",\"type\":\"address\"},{\"internalType\":\"contractISSVDAO\",\"name\":\"ssvDAO_\",\"type\":\"address\"},{\"internalType\":\"contractISSVViews\",\"name\":\"ssvViews_\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"uint64\",\"name\":\"minimumBlocksBeforeLiquidation\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"minimumLiquidationCollateral\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"validatorsPerOperatorLimit\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"declareOperatorFeePeriod\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"executeOperatorFeePeriod\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"operatorMaxFeeIncrease\",\"type\":\"uint64\"},{\"internalType\":\"uint32[4]\",\"name\":\"defaultOracleIds\",\"type\":\"uint32[4]\"},{\"internalType\":\"uint16\",\"name\":\"quorumBps\",\"type\":\"uint16\"}],\"internalType\":\"structISSVNetwork.NetworkInitParams\",\"name\":\"params\",\"type\":\"tuple\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"clusterOwner\",\"type\":\"address\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"liquidate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"clusterOwner\",\"type\":\"address\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"liquidateSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"migrateClusterToETH\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"onCSSVTransfer\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pendingOwner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"reactivate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"reduceOperatorFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"},{\"internalType\":\"bool\",\"name\":\"setPrivate\",\"type\":\"bool\"}],\"name\":\"registerOperator\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"id\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"bytes\",\"name\":\"sharesData\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"registerValidator\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"removeOperator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"removeOperatorsWhitelistingContract\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"address[]\",\"name\":\"whitelistAddresses\",\"type\":\"address[]\"}],\"name\":\"removeOperatorsWhitelists\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"publicKey\",\"type\":\"bytes\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"removeValidator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"oracleId\",\"type\":\"uint32\"},{\"internalType\":\"address\",\"name\":\"newOracle\",\"type\":\"address\"}],\"name\":\"replaceOracle\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"requestUnstake\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"rescueERC20\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recipientAddress\",\"type\":\"address\"}],\"name\":\"setFeeRecipientAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"setOperatorsPrivateUnchecked\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"}],\"name\":\"setOperatorsPublicUnchecked\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"contractISSVWhitelistingContract\",\"name\":\"whitelistingContract\",\"type\":\"address\"}],\"name\":\"setOperatorsWhitelistingContract\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"address[]\",\"name\":\"whitelistAddresses\",\"type\":\"address[]\"}],\"name\":\"setOperatorsWhitelists\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"stake\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"syncFees\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"blockNum\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"clusterOwner\",\"type\":\"address\"},{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"},{\"internalType\":\"uint32\",\"name\":\"effectiveBalance\",\"type\":\"uint32\"},{\"internalType\":\"bytes32[]\",\"name\":\"merkleProof\",\"type\":\"bytes32[]\"}],\"name\":\"updateClusterBalance\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"timeInSeconds\",\"type\":\"uint64\"}],\"name\":\"updateDeclareOperatorFeePeriod\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"timeInSeconds\",\"type\":\"uint64\"}],\"name\":\"updateExecuteOperatorFeePeriod\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"blocks\",\"type\":\"uint64\"}],\"name\":\"updateLiquidationThresholdPeriod\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"blocks\",\"type\":\"uint64\"}],\"name\":\"updateLiquidationThresholdPeriodSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"maxFee\",\"type\":\"uint256\"}],\"name\":\"updateMaximumOperatorFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"blocks\",\"type\":\"uint32\"}],\"name\":\"updateMinBlocksBetweenUpdates\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"updateMinimumLiquidationCollateral\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"updateMinimumLiquidationCollateralSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"minFee\",\"type\":\"uint256\"}],\"name\":\"updateMinimumOperatorEthFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"enumSSVModules\",\"name\":\"moduleId\",\"type\":\"uint8\"},{\"internalType\":\"address\",\"name\":\"moduleAddress\",\"type\":\"address\"}],\"name\":\"updateModule\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"updateNetworkFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"}],\"name\":\"updateNetworkFeeSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"percentage\",\"type\":\"uint64\"}],\"name\":\"updateOperatorFeeIncreaseLimit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"quorum\",\"type\":\"uint16\"}],\"name\":\"updateQuorumBps\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"duration\",\"type\":\"uint64\"}],\"name\":\"updateUnstakeCooldownDuration\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"}],\"name\":\"upgradeTo\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64[]\",\"name\":\"operatorIds\",\"type\":\"uint64[]\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint32\",\"name\":\"validatorCount\",\"type\":\"uint32\"},{\"internalType\":\"uint64\",\"name\":\"networkFeeIndex\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"index\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"active\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"}],\"internalType\":\"structISSVNetworkCore.Cluster\",\"name\":\"cluster\",\"type\":\"tuple\"}],\"name\":\"withdraw\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"withdrawAllOperatorEarnings\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"withdrawAllOperatorEarningsSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"}],\"name\":\"withdrawAllVersionOperatorEarnings\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"withdrawNetworkSSVEarnings\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"withdrawOperatorEarnings\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"operatorId\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"withdrawOperatorEarningsSSV\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"withdrawUnlocked\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// ContractABI is the input ABI used to generate the binding from.
// Deprecated: Use ContractMetaData.ABI instead.
var ContractABI = ContractMetaData.ABI

// Contract is an auto generated Go binding around an Ethereum contract.
type Contract struct {
	ContractCaller     // Read-only binding to the contract
	ContractTransactor // Write-only binding to the contract
	ContractFilterer   // Log filterer for contract events
}

// ContractCaller is an auto generated read-only Go binding around an Ethereum contract.
type ContractCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ContractTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ContractFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ContractSession struct {
	Contract     *Contract         // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ContractCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ContractCallerSession struct {
	Contract *ContractCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts   // Call options to use throughout this session
}

// ContractTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ContractTransactorSession struct {
	Contract     *ContractTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// ContractRaw is an auto generated low-level Go binding around an Ethereum contract.
type ContractRaw struct {
	Contract *Contract // Generic contract binding to access the raw methods on
}

// ContractCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ContractCallerRaw struct {
	Contract *ContractCaller // Generic read-only contract binding to access the raw methods on
}

// ContractTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ContractTransactorRaw struct {
	Contract *ContractTransactor // Generic write-only contract binding to access the raw methods on
}

// NewContract creates a new instance of Contract, bound to a specific deployed contract.
func NewContract(address common.Address, backend bind.ContractBackend) (*Contract, error) {
	contract, err := bindContract(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Contract{ContractCaller: ContractCaller{contract: contract}, ContractTransactor: ContractTransactor{contract: contract}, ContractFilterer: ContractFilterer{contract: contract}}, nil
}

// NewContractCaller creates a new read-only instance of Contract, bound to a specific deployed contract.
func NewContractCaller(address common.Address, caller bind.ContractCaller) (*ContractCaller, error) {
	contract, err := bindContract(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ContractCaller{contract: contract}, nil
}

// NewContractTransactor creates a new write-only instance of Contract, bound to a specific deployed contract.
func NewContractTransactor(address common.Address, transactor bind.ContractTransactor) (*ContractTransactor, error) {
	contract, err := bindContract(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ContractTransactor{contract: contract}, nil
}

// NewContractFilterer creates a new log filterer instance of Contract, bound to a specific deployed contract.
func NewContractFilterer(address common.Address, filterer bind.ContractFilterer) (*ContractFilterer, error) {
	contract, err := bindContract(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ContractFilterer{contract: contract}, nil
}

// bindContract binds a generic wrapper to an already deployed contract.
func bindContract(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ContractMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Contract *ContractRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Contract.Contract.ContractCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Contract *ContractRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.Contract.ContractTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Contract *ContractRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Contract.Contract.ContractTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Contract *ContractCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Contract.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Contract *ContractTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Contract *ContractTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Contract.Contract.contract.Transact(opts, method, params...)
}

// GetVersion is a free data retrieval call binding the contract method 0x0d8e6e2c.
//
// Solidity: function getVersion() pure returns(string version)
func (_Contract *ContractCaller) GetVersion(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Contract.contract.Call(opts, &out, "getVersion")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// GetVersion is a free data retrieval call binding the contract method 0x0d8e6e2c.
//
// Solidity: function getVersion() pure returns(string version)
func (_Contract *ContractSession) GetVersion() (string, error) {
	return _Contract.Contract.GetVersion(&_Contract.CallOpts)
}

// GetVersion is a free data retrieval call binding the contract method 0x0d8e6e2c.
//
// Solidity: function getVersion() pure returns(string version)
func (_Contract *ContractCallerSession) GetVersion() (string, error) {
	return _Contract.Contract.GetVersion(&_Contract.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Contract *ContractCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Contract.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Contract *ContractSession) Owner() (common.Address, error) {
	return _Contract.Contract.Owner(&_Contract.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Contract *ContractCallerSession) Owner() (common.Address, error) {
	return _Contract.Contract.Owner(&_Contract.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_Contract *ContractCaller) PendingOwner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Contract.contract.Call(opts, &out, "pendingOwner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_Contract *ContractSession) PendingOwner() (common.Address, error) {
	return _Contract.Contract.PendingOwner(&_Contract.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_Contract *ContractCallerSession) PendingOwner() (common.Address, error) {
	return _Contract.Contract.PendingOwner(&_Contract.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_Contract *ContractCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _Contract.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_Contract *ContractSession) ProxiableUUID() ([32]byte, error) {
	return _Contract.Contract.ProxiableUUID(&_Contract.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_Contract *ContractCallerSession) ProxiableUUID() ([32]byte, error) {
	return _Contract.Contract.ProxiableUUID(&_Contract.CallOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_Contract *ContractTransactor) AcceptOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "acceptOwnership")
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_Contract *ContractSession) AcceptOwnership() (*types.Transaction, error) {
	return _Contract.Contract.AcceptOwnership(&_Contract.TransactOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_Contract *ContractTransactorSession) AcceptOwnership() (*types.Transaction, error) {
	return _Contract.Contract.AcceptOwnership(&_Contract.TransactOpts)
}

// BulkExitValidator is a paid mutator transaction binding the contract method 0x32afd02f.
//
// Solidity: function bulkExitValidator(bytes[] publicKeys, uint64[] operatorIds) returns()
func (_Contract *ContractTransactor) BulkExitValidator(opts *bind.TransactOpts, publicKeys [][]byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "bulkExitValidator", publicKeys, operatorIds)
}

// BulkExitValidator is a paid mutator transaction binding the contract method 0x32afd02f.
//
// Solidity: function bulkExitValidator(bytes[] publicKeys, uint64[] operatorIds) returns()
func (_Contract *ContractSession) BulkExitValidator(publicKeys [][]byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.BulkExitValidator(&_Contract.TransactOpts, publicKeys, operatorIds)
}

// BulkExitValidator is a paid mutator transaction binding the contract method 0x32afd02f.
//
// Solidity: function bulkExitValidator(bytes[] publicKeys, uint64[] operatorIds) returns()
func (_Contract *ContractTransactorSession) BulkExitValidator(publicKeys [][]byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.BulkExitValidator(&_Contract.TransactOpts, publicKeys, operatorIds)
}

// BulkRegisterValidator is a paid mutator transaction binding the contract method 0xa1b483ca.
//
// Solidity: function bulkRegisterValidator(bytes[] publicKeys, uint64[] operatorIds, bytes[] sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactor) BulkRegisterValidator(opts *bind.TransactOpts, publicKeys [][]byte, operatorIds []uint64, sharesData [][]byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "bulkRegisterValidator", publicKeys, operatorIds, sharesData, cluster)
}

// BulkRegisterValidator is a paid mutator transaction binding the contract method 0xa1b483ca.
//
// Solidity: function bulkRegisterValidator(bytes[] publicKeys, uint64[] operatorIds, bytes[] sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractSession) BulkRegisterValidator(publicKeys [][]byte, operatorIds []uint64, sharesData [][]byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.BulkRegisterValidator(&_Contract.TransactOpts, publicKeys, operatorIds, sharesData, cluster)
}

// BulkRegisterValidator is a paid mutator transaction binding the contract method 0xa1b483ca.
//
// Solidity: function bulkRegisterValidator(bytes[] publicKeys, uint64[] operatorIds, bytes[] sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactorSession) BulkRegisterValidator(publicKeys [][]byte, operatorIds []uint64, sharesData [][]byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.BulkRegisterValidator(&_Contract.TransactOpts, publicKeys, operatorIds, sharesData, cluster)
}

// BulkRemoveValidator is a paid mutator transaction binding the contract method 0x5aed1142.
//
// Solidity: function bulkRemoveValidator(bytes[] publicKeys, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactor) BulkRemoveValidator(opts *bind.TransactOpts, publicKeys [][]byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "bulkRemoveValidator", publicKeys, operatorIds, cluster)
}

// BulkRemoveValidator is a paid mutator transaction binding the contract method 0x5aed1142.
//
// Solidity: function bulkRemoveValidator(bytes[] publicKeys, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractSession) BulkRemoveValidator(publicKeys [][]byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.BulkRemoveValidator(&_Contract.TransactOpts, publicKeys, operatorIds, cluster)
}

// BulkRemoveValidator is a paid mutator transaction binding the contract method 0x5aed1142.
//
// Solidity: function bulkRemoveValidator(bytes[] publicKeys, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactorSession) BulkRemoveValidator(publicKeys [][]byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.BulkRemoveValidator(&_Contract.TransactOpts, publicKeys, operatorIds, cluster)
}

// CancelDeclaredOperatorFee is a paid mutator transaction binding the contract method 0x23d68a6d.
//
// Solidity: function cancelDeclaredOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractTransactor) CancelDeclaredOperatorFee(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "cancelDeclaredOperatorFee", operatorId)
}

// CancelDeclaredOperatorFee is a paid mutator transaction binding the contract method 0x23d68a6d.
//
// Solidity: function cancelDeclaredOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractSession) CancelDeclaredOperatorFee(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.CancelDeclaredOperatorFee(&_Contract.TransactOpts, operatorId)
}

// CancelDeclaredOperatorFee is a paid mutator transaction binding the contract method 0x23d68a6d.
//
// Solidity: function cancelDeclaredOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) CancelDeclaredOperatorFee(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.CancelDeclaredOperatorFee(&_Contract.TransactOpts, operatorId)
}

// ClaimEthRewards is a paid mutator transaction binding the contract method 0x27596fce.
//
// Solidity: function claimEthRewards() returns()
func (_Contract *ContractTransactor) ClaimEthRewards(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "claimEthRewards")
}

// ClaimEthRewards is a paid mutator transaction binding the contract method 0x27596fce.
//
// Solidity: function claimEthRewards() returns()
func (_Contract *ContractSession) ClaimEthRewards() (*types.Transaction, error) {
	return _Contract.Contract.ClaimEthRewards(&_Contract.TransactOpts)
}

// ClaimEthRewards is a paid mutator transaction binding the contract method 0x27596fce.
//
// Solidity: function claimEthRewards() returns()
func (_Contract *ContractTransactorSession) ClaimEthRewards() (*types.Transaction, error) {
	return _Contract.Contract.ClaimEthRewards(&_Contract.TransactOpts)
}

// CommitRoot is a paid mutator transaction binding the contract method 0x00ebb5f0.
//
// Solidity: function commitRoot(bytes32 merkleRoot, uint64 blockNum) returns()
func (_Contract *ContractTransactor) CommitRoot(opts *bind.TransactOpts, merkleRoot [32]byte, blockNum uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "commitRoot", merkleRoot, blockNum)
}

// CommitRoot is a paid mutator transaction binding the contract method 0x00ebb5f0.
//
// Solidity: function commitRoot(bytes32 merkleRoot, uint64 blockNum) returns()
func (_Contract *ContractSession) CommitRoot(merkleRoot [32]byte, blockNum uint64) (*types.Transaction, error) {
	return _Contract.Contract.CommitRoot(&_Contract.TransactOpts, merkleRoot, blockNum)
}

// CommitRoot is a paid mutator transaction binding the contract method 0x00ebb5f0.
//
// Solidity: function commitRoot(bytes32 merkleRoot, uint64 blockNum) returns()
func (_Contract *ContractTransactorSession) CommitRoot(merkleRoot [32]byte, blockNum uint64) (*types.Transaction, error) {
	return _Contract.Contract.CommitRoot(&_Contract.TransactOpts, merkleRoot, blockNum)
}

// DeclareOperatorFee is a paid mutator transaction binding the contract method 0xb317c35f.
//
// Solidity: function declareOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractTransactor) DeclareOperatorFee(opts *bind.TransactOpts, operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "declareOperatorFee", operatorId, fee)
}

// DeclareOperatorFee is a paid mutator transaction binding the contract method 0xb317c35f.
//
// Solidity: function declareOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractSession) DeclareOperatorFee(operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.DeclareOperatorFee(&_Contract.TransactOpts, operatorId, fee)
}

// DeclareOperatorFee is a paid mutator transaction binding the contract method 0xb317c35f.
//
// Solidity: function declareOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractTransactorSession) DeclareOperatorFee(operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.DeclareOperatorFee(&_Contract.TransactOpts, operatorId, fee)
}

// Deposit is a paid mutator transaction binding the contract method 0x45f47188.
//
// Solidity: function deposit(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactor) Deposit(opts *bind.TransactOpts, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "deposit", clusterOwner, operatorIds, cluster)
}

// Deposit is a paid mutator transaction binding the contract method 0x45f47188.
//
// Solidity: function deposit(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractSession) Deposit(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Deposit(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// Deposit is a paid mutator transaction binding the contract method 0x45f47188.
//
// Solidity: function deposit(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactorSession) Deposit(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Deposit(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// ExecuteOperatorFee is a paid mutator transaction binding the contract method 0x8932cee0.
//
// Solidity: function executeOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractTransactor) ExecuteOperatorFee(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "executeOperatorFee", operatorId)
}

// ExecuteOperatorFee is a paid mutator transaction binding the contract method 0x8932cee0.
//
// Solidity: function executeOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractSession) ExecuteOperatorFee(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.ExecuteOperatorFee(&_Contract.TransactOpts, operatorId)
}

// ExecuteOperatorFee is a paid mutator transaction binding the contract method 0x8932cee0.
//
// Solidity: function executeOperatorFee(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) ExecuteOperatorFee(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.ExecuteOperatorFee(&_Contract.TransactOpts, operatorId)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Contract *ContractTransactor) ExitValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "exitValidator", publicKey, operatorIds)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Contract *ContractSession) ExitValidator(publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.ExitValidator(&_Contract.TransactOpts, publicKey, operatorIds)
}

// ExitValidator is a paid mutator transaction binding the contract method 0x3877322b.
//
// Solidity: function exitValidator(bytes publicKey, uint64[] operatorIds) returns()
func (_Contract *ContractTransactorSession) ExitValidator(publicKey []byte, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.ExitValidator(&_Contract.TransactOpts, publicKey, operatorIds)
}

// Initialize is a paid mutator transaction binding the contract method 0x33dcc58a.
//
// Solidity: function initialize(address token_, address ssvOperators_, address ssvClusters_, address ssvDAO_, address ssvViews_, (uint64,uint256,uint32,uint64,uint64,uint64,uint32[4],uint16) params) returns()
func (_Contract *ContractTransactor) Initialize(opts *bind.TransactOpts, token_ common.Address, ssvOperators_ common.Address, ssvClusters_ common.Address, ssvDAO_ common.Address, ssvViews_ common.Address, params ISSVNetworkNetworkInitParams) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "initialize", token_, ssvOperators_, ssvClusters_, ssvDAO_, ssvViews_, params)
}

// Initialize is a paid mutator transaction binding the contract method 0x33dcc58a.
//
// Solidity: function initialize(address token_, address ssvOperators_, address ssvClusters_, address ssvDAO_, address ssvViews_, (uint64,uint256,uint32,uint64,uint64,uint64,uint32[4],uint16) params) returns()
func (_Contract *ContractSession) Initialize(token_ common.Address, ssvOperators_ common.Address, ssvClusters_ common.Address, ssvDAO_ common.Address, ssvViews_ common.Address, params ISSVNetworkNetworkInitParams) (*types.Transaction, error) {
	return _Contract.Contract.Initialize(&_Contract.TransactOpts, token_, ssvOperators_, ssvClusters_, ssvDAO_, ssvViews_, params)
}

// Initialize is a paid mutator transaction binding the contract method 0x33dcc58a.
//
// Solidity: function initialize(address token_, address ssvOperators_, address ssvClusters_, address ssvDAO_, address ssvViews_, (uint64,uint256,uint32,uint64,uint64,uint64,uint32[4],uint16) params) returns()
func (_Contract *ContractTransactorSession) Initialize(token_ common.Address, ssvOperators_ common.Address, ssvClusters_ common.Address, ssvDAO_ common.Address, ssvViews_ common.Address, params ISSVNetworkNetworkInitParams) (*types.Transaction, error) {
	return _Contract.Contract.Initialize(&_Contract.TransactOpts, token_, ssvOperators_, ssvClusters_, ssvDAO_, ssvViews_, params)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactor) Liquidate(opts *bind.TransactOpts, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "liquidate", clusterOwner, operatorIds, cluster)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractSession) Liquidate(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Liquidate(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// Liquidate is a paid mutator transaction binding the contract method 0xbf0f2fb2.
//
// Solidity: function liquidate(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactorSession) Liquidate(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Liquidate(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// LiquidateSSV is a paid mutator transaction binding the contract method 0x8c1d3d03.
//
// Solidity: function liquidateSSV(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactor) LiquidateSSV(opts *bind.TransactOpts, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "liquidateSSV", clusterOwner, operatorIds, cluster)
}

// LiquidateSSV is a paid mutator transaction binding the contract method 0x8c1d3d03.
//
// Solidity: function liquidateSSV(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractSession) LiquidateSSV(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.LiquidateSSV(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// LiquidateSSV is a paid mutator transaction binding the contract method 0x8c1d3d03.
//
// Solidity: function liquidateSSV(address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactorSession) LiquidateSSV(clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.LiquidateSSV(&_Contract.TransactOpts, clusterOwner, operatorIds, cluster)
}

// MigrateClusterToETH is a paid mutator transaction binding the contract method 0x36e87b12.
//
// Solidity: function migrateClusterToETH(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactor) MigrateClusterToETH(opts *bind.TransactOpts, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "migrateClusterToETH", operatorIds, cluster)
}

// MigrateClusterToETH is a paid mutator transaction binding the contract method 0x36e87b12.
//
// Solidity: function migrateClusterToETH(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractSession) MigrateClusterToETH(operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.MigrateClusterToETH(&_Contract.TransactOpts, operatorIds, cluster)
}

// MigrateClusterToETH is a paid mutator transaction binding the contract method 0x36e87b12.
//
// Solidity: function migrateClusterToETH(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactorSession) MigrateClusterToETH(operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.MigrateClusterToETH(&_Contract.TransactOpts, operatorIds, cluster)
}

// OnCSSVTransfer is a paid mutator transaction binding the contract method 0x3dd704f0.
//
// Solidity: function onCSSVTransfer(address from, address to, uint256 amount) returns()
func (_Contract *ContractTransactor) OnCSSVTransfer(opts *bind.TransactOpts, from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "onCSSVTransfer", from, to, amount)
}

// OnCSSVTransfer is a paid mutator transaction binding the contract method 0x3dd704f0.
//
// Solidity: function onCSSVTransfer(address from, address to, uint256 amount) returns()
func (_Contract *ContractSession) OnCSSVTransfer(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.OnCSSVTransfer(&_Contract.TransactOpts, from, to, amount)
}

// OnCSSVTransfer is a paid mutator transaction binding the contract method 0x3dd704f0.
//
// Solidity: function onCSSVTransfer(address from, address to, uint256 amount) returns()
func (_Contract *ContractTransactorSession) OnCSSVTransfer(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.OnCSSVTransfer(&_Contract.TransactOpts, from, to, amount)
}

// Reactivate is a paid mutator transaction binding the contract method 0xea7844ff.
//
// Solidity: function reactivate(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactor) Reactivate(opts *bind.TransactOpts, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "reactivate", operatorIds, cluster)
}

// Reactivate is a paid mutator transaction binding the contract method 0xea7844ff.
//
// Solidity: function reactivate(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractSession) Reactivate(operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Reactivate(&_Contract.TransactOpts, operatorIds, cluster)
}

// Reactivate is a paid mutator transaction binding the contract method 0xea7844ff.
//
// Solidity: function reactivate(uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactorSession) Reactivate(operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Reactivate(&_Contract.TransactOpts, operatorIds, cluster)
}

// ReduceOperatorFee is a paid mutator transaction binding the contract method 0x190d82e4.
//
// Solidity: function reduceOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractTransactor) ReduceOperatorFee(opts *bind.TransactOpts, operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "reduceOperatorFee", operatorId, fee)
}

// ReduceOperatorFee is a paid mutator transaction binding the contract method 0x190d82e4.
//
// Solidity: function reduceOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractSession) ReduceOperatorFee(operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.ReduceOperatorFee(&_Contract.TransactOpts, operatorId, fee)
}

// ReduceOperatorFee is a paid mutator transaction binding the contract method 0x190d82e4.
//
// Solidity: function reduceOperatorFee(uint64 operatorId, uint256 fee) returns()
func (_Contract *ContractTransactorSession) ReduceOperatorFee(operatorId uint64, fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.ReduceOperatorFee(&_Contract.TransactOpts, operatorId, fee)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xc9bbc9fa.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee, bool setPrivate) returns(uint64 id)
func (_Contract *ContractTransactor) RegisterOperator(opts *bind.TransactOpts, publicKey []byte, fee *big.Int, setPrivate bool) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "registerOperator", publicKey, fee, setPrivate)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xc9bbc9fa.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee, bool setPrivate) returns(uint64 id)
func (_Contract *ContractSession) RegisterOperator(publicKey []byte, fee *big.Int, setPrivate bool) (*types.Transaction, error) {
	return _Contract.Contract.RegisterOperator(&_Contract.TransactOpts, publicKey, fee, setPrivate)
}

// RegisterOperator is a paid mutator transaction binding the contract method 0xc9bbc9fa.
//
// Solidity: function registerOperator(bytes publicKey, uint256 fee, bool setPrivate) returns(uint64 id)
func (_Contract *ContractTransactorSession) RegisterOperator(publicKey []byte, fee *big.Int, setPrivate bool) (*types.Transaction, error) {
	return _Contract.Contract.RegisterOperator(&_Contract.TransactOpts, publicKey, fee, setPrivate)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0xb84319fd.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactor) RegisterValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64, sharesData []byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "registerValidator", publicKey, operatorIds, sharesData, cluster)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0xb84319fd.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractSession) RegisterValidator(publicKey []byte, operatorIds []uint64, sharesData []byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.RegisterValidator(&_Contract.TransactOpts, publicKey, operatorIds, sharesData, cluster)
}

// RegisterValidator is a paid mutator transaction binding the contract method 0xb84319fd.
//
// Solidity: function registerValidator(bytes publicKey, uint64[] operatorIds, bytes sharesData, (uint32,uint64,uint64,bool,uint256) cluster) payable returns()
func (_Contract *ContractTransactorSession) RegisterValidator(publicKey []byte, operatorIds []uint64, sharesData []byte, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.RegisterValidator(&_Contract.TransactOpts, publicKey, operatorIds, sharesData, cluster)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Contract *ContractTransactor) RemoveOperator(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "removeOperator", operatorId)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Contract *ContractSession) RemoveOperator(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperator(&_Contract.TransactOpts, operatorId)
}

// RemoveOperator is a paid mutator transaction binding the contract method 0x2e168e0e.
//
// Solidity: function removeOperator(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) RemoveOperator(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperator(&_Contract.TransactOpts, operatorId)
}

// RemoveOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x6a31cf1d.
//
// Solidity: function removeOperatorsWhitelistingContract(uint64[] operatorIds) returns()
func (_Contract *ContractTransactor) RemoveOperatorsWhitelistingContract(opts *bind.TransactOpts, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "removeOperatorsWhitelistingContract", operatorIds)
}

// RemoveOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x6a31cf1d.
//
// Solidity: function removeOperatorsWhitelistingContract(uint64[] operatorIds) returns()
func (_Contract *ContractSession) RemoveOperatorsWhitelistingContract(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperatorsWhitelistingContract(&_Contract.TransactOpts, operatorIds)
}

// RemoveOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x6a31cf1d.
//
// Solidity: function removeOperatorsWhitelistingContract(uint64[] operatorIds) returns()
func (_Contract *ContractTransactorSession) RemoveOperatorsWhitelistingContract(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperatorsWhitelistingContract(&_Contract.TransactOpts, operatorIds)
}

// RemoveOperatorsWhitelists is a paid mutator transaction binding the contract method 0x4b2fd45e.
//
// Solidity: function removeOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractTransactor) RemoveOperatorsWhitelists(opts *bind.TransactOpts, operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "removeOperatorsWhitelists", operatorIds, whitelistAddresses)
}

// RemoveOperatorsWhitelists is a paid mutator transaction binding the contract method 0x4b2fd45e.
//
// Solidity: function removeOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractSession) RemoveOperatorsWhitelists(operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperatorsWhitelists(&_Contract.TransactOpts, operatorIds, whitelistAddresses)
}

// RemoveOperatorsWhitelists is a paid mutator transaction binding the contract method 0x4b2fd45e.
//
// Solidity: function removeOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractTransactorSession) RemoveOperatorsWhitelists(operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.Contract.RemoveOperatorsWhitelists(&_Contract.TransactOpts, operatorIds, whitelistAddresses)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactor) RemoveValidator(opts *bind.TransactOpts, publicKey []byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "removeValidator", publicKey, operatorIds, cluster)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractSession) RemoveValidator(publicKey []byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.RemoveValidator(&_Contract.TransactOpts, publicKey, operatorIds, cluster)
}

// RemoveValidator is a paid mutator transaction binding the contract method 0x12b3fc19.
//
// Solidity: function removeValidator(bytes publicKey, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactorSession) RemoveValidator(publicKey []byte, operatorIds []uint64, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.RemoveValidator(&_Contract.TransactOpts, publicKey, operatorIds, cluster)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_Contract *ContractTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_Contract *ContractSession) RenounceOwnership() (*types.Transaction, error) {
	return _Contract.Contract.RenounceOwnership(&_Contract.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_Contract *ContractTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _Contract.Contract.RenounceOwnership(&_Contract.TransactOpts)
}

// ReplaceOracle is a paid mutator transaction binding the contract method 0x5d756b1d.
//
// Solidity: function replaceOracle(uint32 oracleId, address newOracle) returns()
func (_Contract *ContractTransactor) ReplaceOracle(opts *bind.TransactOpts, oracleId uint32, newOracle common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "replaceOracle", oracleId, newOracle)
}

// ReplaceOracle is a paid mutator transaction binding the contract method 0x5d756b1d.
//
// Solidity: function replaceOracle(uint32 oracleId, address newOracle) returns()
func (_Contract *ContractSession) ReplaceOracle(oracleId uint32, newOracle common.Address) (*types.Transaction, error) {
	return _Contract.Contract.ReplaceOracle(&_Contract.TransactOpts, oracleId, newOracle)
}

// ReplaceOracle is a paid mutator transaction binding the contract method 0x5d756b1d.
//
// Solidity: function replaceOracle(uint32 oracleId, address newOracle) returns()
func (_Contract *ContractTransactorSession) ReplaceOracle(oracleId uint32, newOracle common.Address) (*types.Transaction, error) {
	return _Contract.Contract.ReplaceOracle(&_Contract.TransactOpts, oracleId, newOracle)
}

// RequestUnstake is a paid mutator transaction binding the contract method 0x23095721.
//
// Solidity: function requestUnstake(uint256 amount) returns()
func (_Contract *ContractTransactor) RequestUnstake(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "requestUnstake", amount)
}

// RequestUnstake is a paid mutator transaction binding the contract method 0x23095721.
//
// Solidity: function requestUnstake(uint256 amount) returns()
func (_Contract *ContractSession) RequestUnstake(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.RequestUnstake(&_Contract.TransactOpts, amount)
}

// RequestUnstake is a paid mutator transaction binding the contract method 0x23095721.
//
// Solidity: function requestUnstake(uint256 amount) returns()
func (_Contract *ContractTransactorSession) RequestUnstake(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.RequestUnstake(&_Contract.TransactOpts, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address token, address to, uint256 amount) returns()
func (_Contract *ContractTransactor) RescueERC20(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "rescueERC20", token, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address token, address to, uint256 amount) returns()
func (_Contract *ContractSession) RescueERC20(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.RescueERC20(&_Contract.TransactOpts, token, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address token, address to, uint256 amount) returns()
func (_Contract *ContractTransactorSession) RescueERC20(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.RescueERC20(&_Contract.TransactOpts, token, to, amount)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Contract *ContractTransactor) SetFeeRecipientAddress(opts *bind.TransactOpts, recipientAddress common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "setFeeRecipientAddress", recipientAddress)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Contract *ContractSession) SetFeeRecipientAddress(recipientAddress common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetFeeRecipientAddress(&_Contract.TransactOpts, recipientAddress)
}

// SetFeeRecipientAddress is a paid mutator transaction binding the contract method 0xdbcdc2cc.
//
// Solidity: function setFeeRecipientAddress(address recipientAddress) returns()
func (_Contract *ContractTransactorSession) SetFeeRecipientAddress(recipientAddress common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetFeeRecipientAddress(&_Contract.TransactOpts, recipientAddress)
}

// SetOperatorsPrivateUnchecked is a paid mutator transaction binding the contract method 0x822124c1.
//
// Solidity: function setOperatorsPrivateUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractTransactor) SetOperatorsPrivateUnchecked(opts *bind.TransactOpts, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "setOperatorsPrivateUnchecked", operatorIds)
}

// SetOperatorsPrivateUnchecked is a paid mutator transaction binding the contract method 0x822124c1.
//
// Solidity: function setOperatorsPrivateUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractSession) SetOperatorsPrivateUnchecked(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsPrivateUnchecked(&_Contract.TransactOpts, operatorIds)
}

// SetOperatorsPrivateUnchecked is a paid mutator transaction binding the contract method 0x822124c1.
//
// Solidity: function setOperatorsPrivateUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractTransactorSession) SetOperatorsPrivateUnchecked(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsPrivateUnchecked(&_Contract.TransactOpts, operatorIds)
}

// SetOperatorsPublicUnchecked is a paid mutator transaction binding the contract method 0x4ad00e54.
//
// Solidity: function setOperatorsPublicUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractTransactor) SetOperatorsPublicUnchecked(opts *bind.TransactOpts, operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "setOperatorsPublicUnchecked", operatorIds)
}

// SetOperatorsPublicUnchecked is a paid mutator transaction binding the contract method 0x4ad00e54.
//
// Solidity: function setOperatorsPublicUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractSession) SetOperatorsPublicUnchecked(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsPublicUnchecked(&_Contract.TransactOpts, operatorIds)
}

// SetOperatorsPublicUnchecked is a paid mutator transaction binding the contract method 0x4ad00e54.
//
// Solidity: function setOperatorsPublicUnchecked(uint64[] operatorIds) returns()
func (_Contract *ContractTransactorSession) SetOperatorsPublicUnchecked(operatorIds []uint64) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsPublicUnchecked(&_Contract.TransactOpts, operatorIds)
}

// SetOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x7dc24d52.
//
// Solidity: function setOperatorsWhitelistingContract(uint64[] operatorIds, address whitelistingContract) returns()
func (_Contract *ContractTransactor) SetOperatorsWhitelistingContract(opts *bind.TransactOpts, operatorIds []uint64, whitelistingContract common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "setOperatorsWhitelistingContract", operatorIds, whitelistingContract)
}

// SetOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x7dc24d52.
//
// Solidity: function setOperatorsWhitelistingContract(uint64[] operatorIds, address whitelistingContract) returns()
func (_Contract *ContractSession) SetOperatorsWhitelistingContract(operatorIds []uint64, whitelistingContract common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsWhitelistingContract(&_Contract.TransactOpts, operatorIds, whitelistingContract)
}

// SetOperatorsWhitelistingContract is a paid mutator transaction binding the contract method 0x7dc24d52.
//
// Solidity: function setOperatorsWhitelistingContract(uint64[] operatorIds, address whitelistingContract) returns()
func (_Contract *ContractTransactorSession) SetOperatorsWhitelistingContract(operatorIds []uint64, whitelistingContract common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsWhitelistingContract(&_Contract.TransactOpts, operatorIds, whitelistingContract)
}

// SetOperatorsWhitelists is a paid mutator transaction binding the contract method 0x5d06ecb4.
//
// Solidity: function setOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractTransactor) SetOperatorsWhitelists(opts *bind.TransactOpts, operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "setOperatorsWhitelists", operatorIds, whitelistAddresses)
}

// SetOperatorsWhitelists is a paid mutator transaction binding the contract method 0x5d06ecb4.
//
// Solidity: function setOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractSession) SetOperatorsWhitelists(operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsWhitelists(&_Contract.TransactOpts, operatorIds, whitelistAddresses)
}

// SetOperatorsWhitelists is a paid mutator transaction binding the contract method 0x5d06ecb4.
//
// Solidity: function setOperatorsWhitelists(uint64[] operatorIds, address[] whitelistAddresses) returns()
func (_Contract *ContractTransactorSession) SetOperatorsWhitelists(operatorIds []uint64, whitelistAddresses []common.Address) (*types.Transaction, error) {
	return _Contract.Contract.SetOperatorsWhitelists(&_Contract.TransactOpts, operatorIds, whitelistAddresses)
}

// Stake is a paid mutator transaction binding the contract method 0xa694fc3a.
//
// Solidity: function stake(uint256 amount) returns()
func (_Contract *ContractTransactor) Stake(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "stake", amount)
}

// Stake is a paid mutator transaction binding the contract method 0xa694fc3a.
//
// Solidity: function stake(uint256 amount) returns()
func (_Contract *ContractSession) Stake(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.Stake(&_Contract.TransactOpts, amount)
}

// Stake is a paid mutator transaction binding the contract method 0xa694fc3a.
//
// Solidity: function stake(uint256 amount) returns()
func (_Contract *ContractTransactorSession) Stake(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.Stake(&_Contract.TransactOpts, amount)
}

// SyncFees is a paid mutator transaction binding the contract method 0xc54f0916.
//
// Solidity: function syncFees() returns()
func (_Contract *ContractTransactor) SyncFees(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "syncFees")
}

// SyncFees is a paid mutator transaction binding the contract method 0xc54f0916.
//
// Solidity: function syncFees() returns()
func (_Contract *ContractSession) SyncFees() (*types.Transaction, error) {
	return _Contract.Contract.SyncFees(&_Contract.TransactOpts)
}

// SyncFees is a paid mutator transaction binding the contract method 0xc54f0916.
//
// Solidity: function syncFees() returns()
func (_Contract *ContractTransactorSession) SyncFees() (*types.Transaction, error) {
	return _Contract.Contract.SyncFees(&_Contract.TransactOpts)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Contract *ContractTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Contract *ContractSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _Contract.Contract.TransferOwnership(&_Contract.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Contract *ContractTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _Contract.Contract.TransferOwnership(&_Contract.TransactOpts, newOwner)
}

// UpdateClusterBalance is a paid mutator transaction binding the contract method 0x4f4d1209.
//
// Solidity: function updateClusterBalance(uint64 blockNum, address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster, uint32 effectiveBalance, bytes32[] merkleProof) returns()
func (_Contract *ContractTransactor) UpdateClusterBalance(opts *bind.TransactOpts, blockNum uint64, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster, effectiveBalance uint32, merkleProof [][32]byte) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateClusterBalance", blockNum, clusterOwner, operatorIds, cluster, effectiveBalance, merkleProof)
}

// UpdateClusterBalance is a paid mutator transaction binding the contract method 0x4f4d1209.
//
// Solidity: function updateClusterBalance(uint64 blockNum, address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster, uint32 effectiveBalance, bytes32[] merkleProof) returns()
func (_Contract *ContractSession) UpdateClusterBalance(blockNum uint64, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster, effectiveBalance uint32, merkleProof [][32]byte) (*types.Transaction, error) {
	return _Contract.Contract.UpdateClusterBalance(&_Contract.TransactOpts, blockNum, clusterOwner, operatorIds, cluster, effectiveBalance, merkleProof)
}

// UpdateClusterBalance is a paid mutator transaction binding the contract method 0x4f4d1209.
//
// Solidity: function updateClusterBalance(uint64 blockNum, address clusterOwner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster, uint32 effectiveBalance, bytes32[] merkleProof) returns()
func (_Contract *ContractTransactorSession) UpdateClusterBalance(blockNum uint64, clusterOwner common.Address, operatorIds []uint64, cluster ISSVNetworkCoreCluster, effectiveBalance uint32, merkleProof [][32]byte) (*types.Transaction, error) {
	return _Contract.Contract.UpdateClusterBalance(&_Contract.TransactOpts, blockNum, clusterOwner, operatorIds, cluster, effectiveBalance, merkleProof)
}

// UpdateDeclareOperatorFeePeriod is a paid mutator transaction binding the contract method 0x79e3e4e4.
//
// Solidity: function updateDeclareOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractTransactor) UpdateDeclareOperatorFeePeriod(opts *bind.TransactOpts, timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateDeclareOperatorFeePeriod", timeInSeconds)
}

// UpdateDeclareOperatorFeePeriod is a paid mutator transaction binding the contract method 0x79e3e4e4.
//
// Solidity: function updateDeclareOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractSession) UpdateDeclareOperatorFeePeriod(timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateDeclareOperatorFeePeriod(&_Contract.TransactOpts, timeInSeconds)
}

// UpdateDeclareOperatorFeePeriod is a paid mutator transaction binding the contract method 0x79e3e4e4.
//
// Solidity: function updateDeclareOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractTransactorSession) UpdateDeclareOperatorFeePeriod(timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateDeclareOperatorFeePeriod(&_Contract.TransactOpts, timeInSeconds)
}

// UpdateExecuteOperatorFeePeriod is a paid mutator transaction binding the contract method 0xeb608022.
//
// Solidity: function updateExecuteOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractTransactor) UpdateExecuteOperatorFeePeriod(opts *bind.TransactOpts, timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateExecuteOperatorFeePeriod", timeInSeconds)
}

// UpdateExecuteOperatorFeePeriod is a paid mutator transaction binding the contract method 0xeb608022.
//
// Solidity: function updateExecuteOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractSession) UpdateExecuteOperatorFeePeriod(timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateExecuteOperatorFeePeriod(&_Contract.TransactOpts, timeInSeconds)
}

// UpdateExecuteOperatorFeePeriod is a paid mutator transaction binding the contract method 0xeb608022.
//
// Solidity: function updateExecuteOperatorFeePeriod(uint64 timeInSeconds) returns()
func (_Contract *ContractTransactorSession) UpdateExecuteOperatorFeePeriod(timeInSeconds uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateExecuteOperatorFeePeriod(&_Contract.TransactOpts, timeInSeconds)
}

// UpdateLiquidationThresholdPeriod is a paid mutator transaction binding the contract method 0x6512447d.
//
// Solidity: function updateLiquidationThresholdPeriod(uint64 blocks) returns()
func (_Contract *ContractTransactor) UpdateLiquidationThresholdPeriod(opts *bind.TransactOpts, blocks uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateLiquidationThresholdPeriod", blocks)
}

// UpdateLiquidationThresholdPeriod is a paid mutator transaction binding the contract method 0x6512447d.
//
// Solidity: function updateLiquidationThresholdPeriod(uint64 blocks) returns()
func (_Contract *ContractSession) UpdateLiquidationThresholdPeriod(blocks uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateLiquidationThresholdPeriod(&_Contract.TransactOpts, blocks)
}

// UpdateLiquidationThresholdPeriod is a paid mutator transaction binding the contract method 0x6512447d.
//
// Solidity: function updateLiquidationThresholdPeriod(uint64 blocks) returns()
func (_Contract *ContractTransactorSession) UpdateLiquidationThresholdPeriod(blocks uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateLiquidationThresholdPeriod(&_Contract.TransactOpts, blocks)
}

// UpdateLiquidationThresholdPeriodSSV is a paid mutator transaction binding the contract method 0xe567ed58.
//
// Solidity: function updateLiquidationThresholdPeriodSSV(uint64 blocks) returns()
func (_Contract *ContractTransactor) UpdateLiquidationThresholdPeriodSSV(opts *bind.TransactOpts, blocks uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateLiquidationThresholdPeriodSSV", blocks)
}

// UpdateLiquidationThresholdPeriodSSV is a paid mutator transaction binding the contract method 0xe567ed58.
//
// Solidity: function updateLiquidationThresholdPeriodSSV(uint64 blocks) returns()
func (_Contract *ContractSession) UpdateLiquidationThresholdPeriodSSV(blocks uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateLiquidationThresholdPeriodSSV(&_Contract.TransactOpts, blocks)
}

// UpdateLiquidationThresholdPeriodSSV is a paid mutator transaction binding the contract method 0xe567ed58.
//
// Solidity: function updateLiquidationThresholdPeriodSSV(uint64 blocks) returns()
func (_Contract *ContractTransactorSession) UpdateLiquidationThresholdPeriodSSV(blocks uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateLiquidationThresholdPeriodSSV(&_Contract.TransactOpts, blocks)
}

// UpdateMaximumOperatorFee is a paid mutator transaction binding the contract method 0x6f4b158d.
//
// Solidity: function updateMaximumOperatorFee(uint256 maxFee) returns()
func (_Contract *ContractTransactor) UpdateMaximumOperatorFee(opts *bind.TransactOpts, maxFee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateMaximumOperatorFee", maxFee)
}

// UpdateMaximumOperatorFee is a paid mutator transaction binding the contract method 0x6f4b158d.
//
// Solidity: function updateMaximumOperatorFee(uint256 maxFee) returns()
func (_Contract *ContractSession) UpdateMaximumOperatorFee(maxFee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMaximumOperatorFee(&_Contract.TransactOpts, maxFee)
}

// UpdateMaximumOperatorFee is a paid mutator transaction binding the contract method 0x6f4b158d.
//
// Solidity: function updateMaximumOperatorFee(uint256 maxFee) returns()
func (_Contract *ContractTransactorSession) UpdateMaximumOperatorFee(maxFee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMaximumOperatorFee(&_Contract.TransactOpts, maxFee)
}

// UpdateMinBlocksBetweenUpdates is a paid mutator transaction binding the contract method 0x11dff249.
//
// Solidity: function updateMinBlocksBetweenUpdates(uint32 blocks) returns()
func (_Contract *ContractTransactor) UpdateMinBlocksBetweenUpdates(opts *bind.TransactOpts, blocks uint32) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateMinBlocksBetweenUpdates", blocks)
}

// UpdateMinBlocksBetweenUpdates is a paid mutator transaction binding the contract method 0x11dff249.
//
// Solidity: function updateMinBlocksBetweenUpdates(uint32 blocks) returns()
func (_Contract *ContractSession) UpdateMinBlocksBetweenUpdates(blocks uint32) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinBlocksBetweenUpdates(&_Contract.TransactOpts, blocks)
}

// UpdateMinBlocksBetweenUpdates is a paid mutator transaction binding the contract method 0x11dff249.
//
// Solidity: function updateMinBlocksBetweenUpdates(uint32 blocks) returns()
func (_Contract *ContractTransactorSession) UpdateMinBlocksBetweenUpdates(blocks uint32) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinBlocksBetweenUpdates(&_Contract.TransactOpts, blocks)
}

// UpdateMinimumLiquidationCollateral is a paid mutator transaction binding the contract method 0xb4c9c408.
//
// Solidity: function updateMinimumLiquidationCollateral(uint256 amount) returns()
func (_Contract *ContractTransactor) UpdateMinimumLiquidationCollateral(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateMinimumLiquidationCollateral", amount)
}

// UpdateMinimumLiquidationCollateral is a paid mutator transaction binding the contract method 0xb4c9c408.
//
// Solidity: function updateMinimumLiquidationCollateral(uint256 amount) returns()
func (_Contract *ContractSession) UpdateMinimumLiquidationCollateral(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumLiquidationCollateral(&_Contract.TransactOpts, amount)
}

// UpdateMinimumLiquidationCollateral is a paid mutator transaction binding the contract method 0xb4c9c408.
//
// Solidity: function updateMinimumLiquidationCollateral(uint256 amount) returns()
func (_Contract *ContractTransactorSession) UpdateMinimumLiquidationCollateral(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumLiquidationCollateral(&_Contract.TransactOpts, amount)
}

// UpdateMinimumLiquidationCollateralSSV is a paid mutator transaction binding the contract method 0x9f5c1307.
//
// Solidity: function updateMinimumLiquidationCollateralSSV(uint256 amount) returns()
func (_Contract *ContractTransactor) UpdateMinimumLiquidationCollateralSSV(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateMinimumLiquidationCollateralSSV", amount)
}

// UpdateMinimumLiquidationCollateralSSV is a paid mutator transaction binding the contract method 0x9f5c1307.
//
// Solidity: function updateMinimumLiquidationCollateralSSV(uint256 amount) returns()
func (_Contract *ContractSession) UpdateMinimumLiquidationCollateralSSV(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumLiquidationCollateralSSV(&_Contract.TransactOpts, amount)
}

// UpdateMinimumLiquidationCollateralSSV is a paid mutator transaction binding the contract method 0x9f5c1307.
//
// Solidity: function updateMinimumLiquidationCollateralSSV(uint256 amount) returns()
func (_Contract *ContractTransactorSession) UpdateMinimumLiquidationCollateralSSV(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumLiquidationCollateralSSV(&_Contract.TransactOpts, amount)
}

// UpdateMinimumOperatorEthFee is a paid mutator transaction binding the contract method 0xe9d232cd.
//
// Solidity: function updateMinimumOperatorEthFee(uint256 minFee) returns()
func (_Contract *ContractTransactor) UpdateMinimumOperatorEthFee(opts *bind.TransactOpts, minFee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateMinimumOperatorEthFee", minFee)
}

// UpdateMinimumOperatorEthFee is a paid mutator transaction binding the contract method 0xe9d232cd.
//
// Solidity: function updateMinimumOperatorEthFee(uint256 minFee) returns()
func (_Contract *ContractSession) UpdateMinimumOperatorEthFee(minFee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumOperatorEthFee(&_Contract.TransactOpts, minFee)
}

// UpdateMinimumOperatorEthFee is a paid mutator transaction binding the contract method 0xe9d232cd.
//
// Solidity: function updateMinimumOperatorEthFee(uint256 minFee) returns()
func (_Contract *ContractTransactorSession) UpdateMinimumOperatorEthFee(minFee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateMinimumOperatorEthFee(&_Contract.TransactOpts, minFee)
}

// UpdateModule is a paid mutator transaction binding the contract method 0xe3e324b0.
//
// Solidity: function updateModule(uint8 moduleId, address moduleAddress) returns()
func (_Contract *ContractTransactor) UpdateModule(opts *bind.TransactOpts, moduleId uint8, moduleAddress common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateModule", moduleId, moduleAddress)
}

// UpdateModule is a paid mutator transaction binding the contract method 0xe3e324b0.
//
// Solidity: function updateModule(uint8 moduleId, address moduleAddress) returns()
func (_Contract *ContractSession) UpdateModule(moduleId uint8, moduleAddress common.Address) (*types.Transaction, error) {
	return _Contract.Contract.UpdateModule(&_Contract.TransactOpts, moduleId, moduleAddress)
}

// UpdateModule is a paid mutator transaction binding the contract method 0xe3e324b0.
//
// Solidity: function updateModule(uint8 moduleId, address moduleAddress) returns()
func (_Contract *ContractTransactorSession) UpdateModule(moduleId uint8, moduleAddress common.Address) (*types.Transaction, error) {
	return _Contract.Contract.UpdateModule(&_Contract.TransactOpts, moduleId, moduleAddress)
}

// UpdateNetworkFee is a paid mutator transaction binding the contract method 0x1f1f9fd5.
//
// Solidity: function updateNetworkFee(uint256 fee) returns()
func (_Contract *ContractTransactor) UpdateNetworkFee(opts *bind.TransactOpts, fee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateNetworkFee", fee)
}

// UpdateNetworkFee is a paid mutator transaction binding the contract method 0x1f1f9fd5.
//
// Solidity: function updateNetworkFee(uint256 fee) returns()
func (_Contract *ContractSession) UpdateNetworkFee(fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateNetworkFee(&_Contract.TransactOpts, fee)
}

// UpdateNetworkFee is a paid mutator transaction binding the contract method 0x1f1f9fd5.
//
// Solidity: function updateNetworkFee(uint256 fee) returns()
func (_Contract *ContractTransactorSession) UpdateNetworkFee(fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateNetworkFee(&_Contract.TransactOpts, fee)
}

// UpdateNetworkFeeSSV is a paid mutator transaction binding the contract method 0x71513214.
//
// Solidity: function updateNetworkFeeSSV(uint256 fee) returns()
func (_Contract *ContractTransactor) UpdateNetworkFeeSSV(opts *bind.TransactOpts, fee *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateNetworkFeeSSV", fee)
}

// UpdateNetworkFeeSSV is a paid mutator transaction binding the contract method 0x71513214.
//
// Solidity: function updateNetworkFeeSSV(uint256 fee) returns()
func (_Contract *ContractSession) UpdateNetworkFeeSSV(fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateNetworkFeeSSV(&_Contract.TransactOpts, fee)
}

// UpdateNetworkFeeSSV is a paid mutator transaction binding the contract method 0x71513214.
//
// Solidity: function updateNetworkFeeSSV(uint256 fee) returns()
func (_Contract *ContractTransactorSession) UpdateNetworkFeeSSV(fee *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.UpdateNetworkFeeSSV(&_Contract.TransactOpts, fee)
}

// UpdateOperatorFeeIncreaseLimit is a paid mutator transaction binding the contract method 0x3631983f.
//
// Solidity: function updateOperatorFeeIncreaseLimit(uint64 percentage) returns()
func (_Contract *ContractTransactor) UpdateOperatorFeeIncreaseLimit(opts *bind.TransactOpts, percentage uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateOperatorFeeIncreaseLimit", percentage)
}

// UpdateOperatorFeeIncreaseLimit is a paid mutator transaction binding the contract method 0x3631983f.
//
// Solidity: function updateOperatorFeeIncreaseLimit(uint64 percentage) returns()
func (_Contract *ContractSession) UpdateOperatorFeeIncreaseLimit(percentage uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateOperatorFeeIncreaseLimit(&_Contract.TransactOpts, percentage)
}

// UpdateOperatorFeeIncreaseLimit is a paid mutator transaction binding the contract method 0x3631983f.
//
// Solidity: function updateOperatorFeeIncreaseLimit(uint64 percentage) returns()
func (_Contract *ContractTransactorSession) UpdateOperatorFeeIncreaseLimit(percentage uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateOperatorFeeIncreaseLimit(&_Contract.TransactOpts, percentage)
}

// UpdateQuorumBps is a paid mutator transaction binding the contract method 0x9ba0e700.
//
// Solidity: function updateQuorumBps(uint16 quorum) returns()
func (_Contract *ContractTransactor) UpdateQuorumBps(opts *bind.TransactOpts, quorum uint16) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateQuorumBps", quorum)
}

// UpdateQuorumBps is a paid mutator transaction binding the contract method 0x9ba0e700.
//
// Solidity: function updateQuorumBps(uint16 quorum) returns()
func (_Contract *ContractSession) UpdateQuorumBps(quorum uint16) (*types.Transaction, error) {
	return _Contract.Contract.UpdateQuorumBps(&_Contract.TransactOpts, quorum)
}

// UpdateQuorumBps is a paid mutator transaction binding the contract method 0x9ba0e700.
//
// Solidity: function updateQuorumBps(uint16 quorum) returns()
func (_Contract *ContractTransactorSession) UpdateQuorumBps(quorum uint16) (*types.Transaction, error) {
	return _Contract.Contract.UpdateQuorumBps(&_Contract.TransactOpts, quorum)
}

// UpdateUnstakeCooldownDuration is a paid mutator transaction binding the contract method 0xbe2270dd.
//
// Solidity: function updateUnstakeCooldownDuration(uint64 duration) returns()
func (_Contract *ContractTransactor) UpdateUnstakeCooldownDuration(opts *bind.TransactOpts, duration uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "updateUnstakeCooldownDuration", duration)
}

// UpdateUnstakeCooldownDuration is a paid mutator transaction binding the contract method 0xbe2270dd.
//
// Solidity: function updateUnstakeCooldownDuration(uint64 duration) returns()
func (_Contract *ContractSession) UpdateUnstakeCooldownDuration(duration uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateUnstakeCooldownDuration(&_Contract.TransactOpts, duration)
}

// UpdateUnstakeCooldownDuration is a paid mutator transaction binding the contract method 0xbe2270dd.
//
// Solidity: function updateUnstakeCooldownDuration(uint64 duration) returns()
func (_Contract *ContractTransactorSession) UpdateUnstakeCooldownDuration(duration uint64) (*types.Transaction, error) {
	return _Contract.Contract.UpdateUnstakeCooldownDuration(&_Contract.TransactOpts, duration)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_Contract *ContractTransactor) UpgradeTo(opts *bind.TransactOpts, newImplementation common.Address) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "upgradeTo", newImplementation)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_Contract *ContractSession) UpgradeTo(newImplementation common.Address) (*types.Transaction, error) {
	return _Contract.Contract.UpgradeTo(&_Contract.TransactOpts, newImplementation)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_Contract *ContractTransactorSession) UpgradeTo(newImplementation common.Address) (*types.Transaction, error) {
	return _Contract.Contract.UpgradeTo(&_Contract.TransactOpts, newImplementation)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_Contract *ContractTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_Contract *ContractSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _Contract.Contract.UpgradeToAndCall(&_Contract.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_Contract *ContractTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _Contract.Contract.UpgradeToAndCall(&_Contract.TransactOpts, newImplementation, data)
}

// Withdraw is a paid mutator transaction binding the contract method 0x686e682c.
//
// Solidity: function withdraw(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactor) Withdraw(opts *bind.TransactOpts, operatorIds []uint64, amount *big.Int, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdraw", operatorIds, amount, cluster)
}

// Withdraw is a paid mutator transaction binding the contract method 0x686e682c.
//
// Solidity: function withdraw(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractSession) Withdraw(operatorIds []uint64, amount *big.Int, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Withdraw(&_Contract.TransactOpts, operatorIds, amount, cluster)
}

// Withdraw is a paid mutator transaction binding the contract method 0x686e682c.
//
// Solidity: function withdraw(uint64[] operatorIds, uint256 amount, (uint32,uint64,uint64,bool,uint256) cluster) returns()
func (_Contract *ContractTransactorSession) Withdraw(operatorIds []uint64, amount *big.Int, cluster ISSVNetworkCoreCluster) (*types.Transaction, error) {
	return _Contract.Contract.Withdraw(&_Contract.TransactOpts, operatorIds, amount, cluster)
}

// WithdrawAllOperatorEarnings is a paid mutator transaction binding the contract method 0x4bc93b64.
//
// Solidity: function withdrawAllOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractTransactor) WithdrawAllOperatorEarnings(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawAllOperatorEarnings", operatorId)
}

// WithdrawAllOperatorEarnings is a paid mutator transaction binding the contract method 0x4bc93b64.
//
// Solidity: function withdrawAllOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractSession) WithdrawAllOperatorEarnings(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllOperatorEarnings(&_Contract.TransactOpts, operatorId)
}

// WithdrawAllOperatorEarnings is a paid mutator transaction binding the contract method 0x4bc93b64.
//
// Solidity: function withdrawAllOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) WithdrawAllOperatorEarnings(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllOperatorEarnings(&_Contract.TransactOpts, operatorId)
}

// WithdrawAllOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x909b1aaa.
//
// Solidity: function withdrawAllOperatorEarningsSSV(uint64 operatorId) returns()
func (_Contract *ContractTransactor) WithdrawAllOperatorEarningsSSV(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawAllOperatorEarningsSSV", operatorId)
}

// WithdrawAllOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x909b1aaa.
//
// Solidity: function withdrawAllOperatorEarningsSSV(uint64 operatorId) returns()
func (_Contract *ContractSession) WithdrawAllOperatorEarningsSSV(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllOperatorEarningsSSV(&_Contract.TransactOpts, operatorId)
}

// WithdrawAllOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x909b1aaa.
//
// Solidity: function withdrawAllOperatorEarningsSSV(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) WithdrawAllOperatorEarningsSSV(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllOperatorEarningsSSV(&_Contract.TransactOpts, operatorId)
}

// WithdrawAllVersionOperatorEarnings is a paid mutator transaction binding the contract method 0x008108d5.
//
// Solidity: function withdrawAllVersionOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractTransactor) WithdrawAllVersionOperatorEarnings(opts *bind.TransactOpts, operatorId uint64) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawAllVersionOperatorEarnings", operatorId)
}

// WithdrawAllVersionOperatorEarnings is a paid mutator transaction binding the contract method 0x008108d5.
//
// Solidity: function withdrawAllVersionOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractSession) WithdrawAllVersionOperatorEarnings(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllVersionOperatorEarnings(&_Contract.TransactOpts, operatorId)
}

// WithdrawAllVersionOperatorEarnings is a paid mutator transaction binding the contract method 0x008108d5.
//
// Solidity: function withdrawAllVersionOperatorEarnings(uint64 operatorId) returns()
func (_Contract *ContractTransactorSession) WithdrawAllVersionOperatorEarnings(operatorId uint64) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawAllVersionOperatorEarnings(&_Contract.TransactOpts, operatorId)
}

// WithdrawNetworkSSVEarnings is a paid mutator transaction binding the contract method 0x6535f740.
//
// Solidity: function withdrawNetworkSSVEarnings(uint256 amount) returns()
func (_Contract *ContractTransactor) WithdrawNetworkSSVEarnings(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawNetworkSSVEarnings", amount)
}

// WithdrawNetworkSSVEarnings is a paid mutator transaction binding the contract method 0x6535f740.
//
// Solidity: function withdrawNetworkSSVEarnings(uint256 amount) returns()
func (_Contract *ContractSession) WithdrawNetworkSSVEarnings(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawNetworkSSVEarnings(&_Contract.TransactOpts, amount)
}

// WithdrawNetworkSSVEarnings is a paid mutator transaction binding the contract method 0x6535f740.
//
// Solidity: function withdrawNetworkSSVEarnings(uint256 amount) returns()
func (_Contract *ContractTransactorSession) WithdrawNetworkSSVEarnings(amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawNetworkSSVEarnings(&_Contract.TransactOpts, amount)
}

// WithdrawOperatorEarnings is a paid mutator transaction binding the contract method 0x35f63767.
//
// Solidity: function withdrawOperatorEarnings(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractTransactor) WithdrawOperatorEarnings(opts *bind.TransactOpts, operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawOperatorEarnings", operatorId, amount)
}

// WithdrawOperatorEarnings is a paid mutator transaction binding the contract method 0x35f63767.
//
// Solidity: function withdrawOperatorEarnings(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractSession) WithdrawOperatorEarnings(operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawOperatorEarnings(&_Contract.TransactOpts, operatorId, amount)
}

// WithdrawOperatorEarnings is a paid mutator transaction binding the contract method 0x35f63767.
//
// Solidity: function withdrawOperatorEarnings(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractTransactorSession) WithdrawOperatorEarnings(operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawOperatorEarnings(&_Contract.TransactOpts, operatorId, amount)
}

// WithdrawOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x6bf5c92e.
//
// Solidity: function withdrawOperatorEarningsSSV(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractTransactor) WithdrawOperatorEarningsSSV(opts *bind.TransactOpts, operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawOperatorEarningsSSV", operatorId, amount)
}

// WithdrawOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x6bf5c92e.
//
// Solidity: function withdrawOperatorEarningsSSV(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractSession) WithdrawOperatorEarningsSSV(operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawOperatorEarningsSSV(&_Contract.TransactOpts, operatorId, amount)
}

// WithdrawOperatorEarningsSSV is a paid mutator transaction binding the contract method 0x6bf5c92e.
//
// Solidity: function withdrawOperatorEarningsSSV(uint64 operatorId, uint256 amount) returns()
func (_Contract *ContractTransactorSession) WithdrawOperatorEarningsSSV(operatorId uint64, amount *big.Int) (*types.Transaction, error) {
	return _Contract.Contract.WithdrawOperatorEarningsSSV(&_Contract.TransactOpts, operatorId, amount)
}

// WithdrawUnlocked is a paid mutator transaction binding the contract method 0x6fcd112b.
//
// Solidity: function withdrawUnlocked() returns()
func (_Contract *ContractTransactor) WithdrawUnlocked(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contract.contract.Transact(opts, "withdrawUnlocked")
}

// WithdrawUnlocked is a paid mutator transaction binding the contract method 0x6fcd112b.
//
// Solidity: function withdrawUnlocked() returns()
func (_Contract *ContractSession) WithdrawUnlocked() (*types.Transaction, error) {
	return _Contract.Contract.WithdrawUnlocked(&_Contract.TransactOpts)
}

// WithdrawUnlocked is a paid mutator transaction binding the contract method 0x6fcd112b.
//
// Solidity: function withdrawUnlocked() returns()
func (_Contract *ContractTransactorSession) WithdrawUnlocked() (*types.Transaction, error) {
	return _Contract.Contract.WithdrawUnlocked(&_Contract.TransactOpts)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() returns()
func (_Contract *ContractTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _Contract.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() returns()
func (_Contract *ContractSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _Contract.Contract.Fallback(&_Contract.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() returns()
func (_Contract *ContractTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _Contract.Contract.Fallback(&_Contract.TransactOpts, calldata)
}

// ContractAdminChangedIterator is returned from FilterAdminChanged and is used to iterate over the raw logs and unpacked data for AdminChanged events raised by the Contract contract.
type ContractAdminChangedIterator struct {
	Event *ContractAdminChanged // Event containing the contract specifics and raw log

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
func (it *ContractAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractAdminChanged)
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
		it.Event = new(ContractAdminChanged)
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
func (it *ContractAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractAdminChanged represents a AdminChanged event raised by the Contract contract.
type ContractAdminChanged struct {
	PreviousAdmin common.Address
	NewAdmin      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterAdminChanged is a free log retrieval operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_Contract *ContractFilterer) FilterAdminChanged(opts *bind.FilterOpts) (*ContractAdminChangedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "AdminChanged")
	if err != nil {
		return nil, err
	}
	return &ContractAdminChangedIterator{contract: _Contract.contract, event: "AdminChanged", logs: logs, sub: sub}, nil
}

// WatchAdminChanged is a free log subscription operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_Contract *ContractFilterer) WatchAdminChanged(opts *bind.WatchOpts, sink chan<- *ContractAdminChanged) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "AdminChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractAdminChanged)
				if err := _Contract.contract.UnpackLog(event, "AdminChanged", log); err != nil {
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

// ParseAdminChanged is a log parse operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_Contract *ContractFilterer) ParseAdminChanged(log types.Log) (*ContractAdminChanged, error) {
	event := new(ContractAdminChanged)
	if err := _Contract.contract.UnpackLog(event, "AdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractBeaconUpgradedIterator is returned from FilterBeaconUpgraded and is used to iterate over the raw logs and unpacked data for BeaconUpgraded events raised by the Contract contract.
type ContractBeaconUpgradedIterator struct {
	Event *ContractBeaconUpgraded // Event containing the contract specifics and raw log

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
func (it *ContractBeaconUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractBeaconUpgraded)
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
		it.Event = new(ContractBeaconUpgraded)
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
func (it *ContractBeaconUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractBeaconUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractBeaconUpgraded represents a BeaconUpgraded event raised by the Contract contract.
type ContractBeaconUpgraded struct {
	Beacon common.Address
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterBeaconUpgraded is a free log retrieval operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_Contract *ContractFilterer) FilterBeaconUpgraded(opts *bind.FilterOpts, beacon []common.Address) (*ContractBeaconUpgradedIterator, error) {

	var beaconRule []interface{}
	for _, beaconItem := range beacon {
		beaconRule = append(beaconRule, beaconItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "BeaconUpgraded", beaconRule)
	if err != nil {
		return nil, err
	}
	return &ContractBeaconUpgradedIterator{contract: _Contract.contract, event: "BeaconUpgraded", logs: logs, sub: sub}, nil
}

// WatchBeaconUpgraded is a free log subscription operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_Contract *ContractFilterer) WatchBeaconUpgraded(opts *bind.WatchOpts, sink chan<- *ContractBeaconUpgraded, beacon []common.Address) (event.Subscription, error) {

	var beaconRule []interface{}
	for _, beaconItem := range beacon {
		beaconRule = append(beaconRule, beaconItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "BeaconUpgraded", beaconRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractBeaconUpgraded)
				if err := _Contract.contract.UnpackLog(event, "BeaconUpgraded", log); err != nil {
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

// ParseBeaconUpgraded is a log parse operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_Contract *ContractFilterer) ParseBeaconUpgraded(log types.Log) (*ContractBeaconUpgraded, error) {
	event := new(ContractBeaconUpgraded)
	if err := _Contract.contract.UnpackLog(event, "BeaconUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterBalanceUpdatedIterator is returned from FilterClusterBalanceUpdated and is used to iterate over the raw logs and unpacked data for ClusterBalanceUpdated events raised by the Contract contract.
type ContractClusterBalanceUpdatedIterator struct {
	Event *ContractClusterBalanceUpdated // Event containing the contract specifics and raw log

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
func (it *ContractClusterBalanceUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterBalanceUpdated)
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
		it.Event = new(ContractClusterBalanceUpdated)
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
func (it *ContractClusterBalanceUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterBalanceUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterBalanceUpdated represents a ClusterBalanceUpdated event raised by the Contract contract.
type ContractClusterBalanceUpdated struct {
	Owner            common.Address
	OperatorIds      []uint64
	BlockNum         uint64
	EffectiveBalance uint32
	Cluster          ISSVNetworkCoreCluster
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterClusterBalanceUpdated is a free log retrieval operation binding the contract event 0x0cc301d13799ab915796eed462da724756a969f8e05d69ad2405eb3da7036b55.
//
// Solidity: event ClusterBalanceUpdated(address indexed owner, uint64[] operatorIds, uint64 indexed blockNum, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterBalanceUpdated(opts *bind.FilterOpts, owner []common.Address, blockNum []uint64) (*ContractClusterBalanceUpdatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterBalanceUpdated", ownerRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterBalanceUpdatedIterator{contract: _Contract.contract, event: "ClusterBalanceUpdated", logs: logs, sub: sub}, nil
}

// WatchClusterBalanceUpdated is a free log subscription operation binding the contract event 0x0cc301d13799ab915796eed462da724756a969f8e05d69ad2405eb3da7036b55.
//
// Solidity: event ClusterBalanceUpdated(address indexed owner, uint64[] operatorIds, uint64 indexed blockNum, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterBalanceUpdated(opts *bind.WatchOpts, sink chan<- *ContractClusterBalanceUpdated, owner []common.Address, blockNum []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterBalanceUpdated", ownerRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterBalanceUpdated)
				if err := _Contract.contract.UnpackLog(event, "ClusterBalanceUpdated", log); err != nil {
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

// ParseClusterBalanceUpdated is a log parse operation binding the contract event 0x0cc301d13799ab915796eed462da724756a969f8e05d69ad2405eb3da7036b55.
//
// Solidity: event ClusterBalanceUpdated(address indexed owner, uint64[] operatorIds, uint64 indexed blockNum, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) ParseClusterBalanceUpdated(log types.Log) (*ContractClusterBalanceUpdated, error) {
	event := new(ContractClusterBalanceUpdated)
	if err := _Contract.contract.UnpackLog(event, "ClusterBalanceUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterDepositedIterator is returned from FilterClusterDeposited and is used to iterate over the raw logs and unpacked data for ClusterDeposited events raised by the Contract contract.
type ContractClusterDepositedIterator struct {
	Event *ContractClusterDeposited // Event containing the contract specifics and raw log

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
func (it *ContractClusterDepositedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterDeposited)
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
		it.Event = new(ContractClusterDeposited)
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
func (it *ContractClusterDepositedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterDepositedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterDeposited represents a ClusterDeposited event raised by the Contract contract.
type ContractClusterDeposited struct {
	Owner       common.Address
	OperatorIds []uint64
	Value       *big.Int
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterDeposited is a free log retrieval operation binding the contract event 0x2bac1912f2481d12f0df08647c06bee174967c62d3a03cbc078eb215dc1bd9a2.
//
// Solidity: event ClusterDeposited(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterDeposited(opts *bind.FilterOpts, owner []common.Address) (*ContractClusterDepositedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterDeposited", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterDepositedIterator{contract: _Contract.contract, event: "ClusterDeposited", logs: logs, sub: sub}, nil
}

// WatchClusterDeposited is a free log subscription operation binding the contract event 0x2bac1912f2481d12f0df08647c06bee174967c62d3a03cbc078eb215dc1bd9a2.
//
// Solidity: event ClusterDeposited(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterDeposited(opts *bind.WatchOpts, sink chan<- *ContractClusterDeposited, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterDeposited", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterDeposited)
				if err := _Contract.contract.UnpackLog(event, "ClusterDeposited", log); err != nil {
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

// ParseClusterDeposited is a log parse operation binding the contract event 0x2bac1912f2481d12f0df08647c06bee174967c62d3a03cbc078eb215dc1bd9a2.
//
// Solidity: event ClusterDeposited(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) ParseClusterDeposited(log types.Log) (*ContractClusterDeposited, error) {
	event := new(ContractClusterDeposited)
	if err := _Contract.contract.UnpackLog(event, "ClusterDeposited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterLiquidatedIterator is returned from FilterClusterLiquidated and is used to iterate over the raw logs and unpacked data for ClusterLiquidated events raised by the Contract contract.
type ContractClusterLiquidatedIterator struct {
	Event *ContractClusterLiquidated // Event containing the contract specifics and raw log

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
func (it *ContractClusterLiquidatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterLiquidated)
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
		it.Event = new(ContractClusterLiquidated)
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
func (it *ContractClusterLiquidatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterLiquidatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterLiquidated represents a ClusterLiquidated event raised by the Contract contract.
type ContractClusterLiquidated struct {
	Owner       common.Address
	OperatorIds []uint64
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterLiquidated is a free log retrieval operation binding the contract event 0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688.
//
// Solidity: event ClusterLiquidated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterLiquidated(opts *bind.FilterOpts, owner []common.Address) (*ContractClusterLiquidatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterLiquidated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterLiquidatedIterator{contract: _Contract.contract, event: "ClusterLiquidated", logs: logs, sub: sub}, nil
}

// WatchClusterLiquidated is a free log subscription operation binding the contract event 0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688.
//
// Solidity: event ClusterLiquidated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterLiquidated(opts *bind.WatchOpts, sink chan<- *ContractClusterLiquidated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterLiquidated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterLiquidated)
				if err := _Contract.contract.UnpackLog(event, "ClusterLiquidated", log); err != nil {
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
func (_Contract *ContractFilterer) ParseClusterLiquidated(log types.Log) (*ContractClusterLiquidated, error) {
	event := new(ContractClusterLiquidated)
	if err := _Contract.contract.UnpackLog(event, "ClusterLiquidated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterMigratedToETHIterator is returned from FilterClusterMigratedToETH and is used to iterate over the raw logs and unpacked data for ClusterMigratedToETH events raised by the Contract contract.
type ContractClusterMigratedToETHIterator struct {
	Event *ContractClusterMigratedToETH // Event containing the contract specifics and raw log

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
func (it *ContractClusterMigratedToETHIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterMigratedToETH)
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
		it.Event = new(ContractClusterMigratedToETH)
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
func (it *ContractClusterMigratedToETHIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterMigratedToETHIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterMigratedToETH represents a ClusterMigratedToETH event raised by the Contract contract.
type ContractClusterMigratedToETH struct {
	Owner            common.Address
	OperatorIds      []uint64
	EthDeposited     *big.Int
	SsvRefunded      *big.Int
	EffectiveBalance uint32
	Cluster          ISSVNetworkCoreCluster
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterClusterMigratedToETH is a free log retrieval operation binding the contract event 0x6dc71e7231318932a8a7f99bac76f94ffe24cac147843e0c285ce4cf290c803a.
//
// Solidity: event ClusterMigratedToETH(address indexed owner, uint64[] operatorIds, uint256 ethDeposited, uint256 ssvRefunded, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterMigratedToETH(opts *bind.FilterOpts, owner []common.Address) (*ContractClusterMigratedToETHIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterMigratedToETH", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterMigratedToETHIterator{contract: _Contract.contract, event: "ClusterMigratedToETH", logs: logs, sub: sub}, nil
}

// WatchClusterMigratedToETH is a free log subscription operation binding the contract event 0x6dc71e7231318932a8a7f99bac76f94ffe24cac147843e0c285ce4cf290c803a.
//
// Solidity: event ClusterMigratedToETH(address indexed owner, uint64[] operatorIds, uint256 ethDeposited, uint256 ssvRefunded, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterMigratedToETH(opts *bind.WatchOpts, sink chan<- *ContractClusterMigratedToETH, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterMigratedToETH", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterMigratedToETH)
				if err := _Contract.contract.UnpackLog(event, "ClusterMigratedToETH", log); err != nil {
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

// ParseClusterMigratedToETH is a log parse operation binding the contract event 0x6dc71e7231318932a8a7f99bac76f94ffe24cac147843e0c285ce4cf290c803a.
//
// Solidity: event ClusterMigratedToETH(address indexed owner, uint64[] operatorIds, uint256 ethDeposited, uint256 ssvRefunded, uint32 effectiveBalance, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) ParseClusterMigratedToETH(log types.Log) (*ContractClusterMigratedToETH, error) {
	event := new(ContractClusterMigratedToETH)
	if err := _Contract.contract.UnpackLog(event, "ClusterMigratedToETH", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterReactivatedIterator is returned from FilterClusterReactivated and is used to iterate over the raw logs and unpacked data for ClusterReactivated events raised by the Contract contract.
type ContractClusterReactivatedIterator struct {
	Event *ContractClusterReactivated // Event containing the contract specifics and raw log

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
func (it *ContractClusterReactivatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterReactivated)
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
		it.Event = new(ContractClusterReactivated)
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
func (it *ContractClusterReactivatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterReactivatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterReactivated represents a ClusterReactivated event raised by the Contract contract.
type ContractClusterReactivated struct {
	Owner       common.Address
	OperatorIds []uint64
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterReactivated is a free log retrieval operation binding the contract event 0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859.
//
// Solidity: event ClusterReactivated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterReactivated(opts *bind.FilterOpts, owner []common.Address) (*ContractClusterReactivatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterReactivated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterReactivatedIterator{contract: _Contract.contract, event: "ClusterReactivated", logs: logs, sub: sub}, nil
}

// WatchClusterReactivated is a free log subscription operation binding the contract event 0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859.
//
// Solidity: event ClusterReactivated(address indexed owner, uint64[] operatorIds, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterReactivated(opts *bind.WatchOpts, sink chan<- *ContractClusterReactivated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterReactivated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterReactivated)
				if err := _Contract.contract.UnpackLog(event, "ClusterReactivated", log); err != nil {
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
func (_Contract *ContractFilterer) ParseClusterReactivated(log types.Log) (*ContractClusterReactivated, error) {
	event := new(ContractClusterReactivated)
	if err := _Contract.contract.UnpackLog(event, "ClusterReactivated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractClusterWithdrawnIterator is returned from FilterClusterWithdrawn and is used to iterate over the raw logs and unpacked data for ClusterWithdrawn events raised by the Contract contract.
type ContractClusterWithdrawnIterator struct {
	Event *ContractClusterWithdrawn // Event containing the contract specifics and raw log

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
func (it *ContractClusterWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractClusterWithdrawn)
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
		it.Event = new(ContractClusterWithdrawn)
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
func (it *ContractClusterWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractClusterWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractClusterWithdrawn represents a ClusterWithdrawn event raised by the Contract contract.
type ContractClusterWithdrawn struct {
	Owner       common.Address
	OperatorIds []uint64
	Value       *big.Int
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterClusterWithdrawn is a free log retrieval operation binding the contract event 0x39d1320bbda24947e77f3560661323384aa0a1cb9d5e040e617e5cbf50b6dbe0.
//
// Solidity: event ClusterWithdrawn(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterClusterWithdrawn(opts *bind.FilterOpts, owner []common.Address) (*ContractClusterWithdrawnIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ClusterWithdrawn", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractClusterWithdrawnIterator{contract: _Contract.contract, event: "ClusterWithdrawn", logs: logs, sub: sub}, nil
}

// WatchClusterWithdrawn is a free log subscription operation binding the contract event 0x39d1320bbda24947e77f3560661323384aa0a1cb9d5e040e617e5cbf50b6dbe0.
//
// Solidity: event ClusterWithdrawn(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchClusterWithdrawn(opts *bind.WatchOpts, sink chan<- *ContractClusterWithdrawn, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ClusterWithdrawn", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractClusterWithdrawn)
				if err := _Contract.contract.UnpackLog(event, "ClusterWithdrawn", log); err != nil {
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

// ParseClusterWithdrawn is a log parse operation binding the contract event 0x39d1320bbda24947e77f3560661323384aa0a1cb9d5e040e617e5cbf50b6dbe0.
//
// Solidity: event ClusterWithdrawn(address indexed owner, uint64[] operatorIds, uint256 value, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) ParseClusterWithdrawn(log types.Log) (*ContractClusterWithdrawn, error) {
	event := new(ContractClusterWithdrawn)
	if err := _Contract.contract.UnpackLog(event, "ClusterWithdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractCooldownDurationUpdatedIterator is returned from FilterCooldownDurationUpdated and is used to iterate over the raw logs and unpacked data for CooldownDurationUpdated events raised by the Contract contract.
type ContractCooldownDurationUpdatedIterator struct {
	Event *ContractCooldownDurationUpdated // Event containing the contract specifics and raw log

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
func (it *ContractCooldownDurationUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractCooldownDurationUpdated)
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
		it.Event = new(ContractCooldownDurationUpdated)
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
func (it *ContractCooldownDurationUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractCooldownDurationUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractCooldownDurationUpdated represents a CooldownDurationUpdated event raised by the Contract contract.
type ContractCooldownDurationUpdated struct {
	NewCooldownDuration uint64
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterCooldownDurationUpdated is a free log retrieval operation binding the contract event 0xe865acf13176ceb6a83a16f925f9f67fd03c94ca1af6160d78a7f8a2914253e2.
//
// Solidity: event CooldownDurationUpdated(uint64 newCooldownDuration)
func (_Contract *ContractFilterer) FilterCooldownDurationUpdated(opts *bind.FilterOpts) (*ContractCooldownDurationUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "CooldownDurationUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractCooldownDurationUpdatedIterator{contract: _Contract.contract, event: "CooldownDurationUpdated", logs: logs, sub: sub}, nil
}

// WatchCooldownDurationUpdated is a free log subscription operation binding the contract event 0xe865acf13176ceb6a83a16f925f9f67fd03c94ca1af6160d78a7f8a2914253e2.
//
// Solidity: event CooldownDurationUpdated(uint64 newCooldownDuration)
func (_Contract *ContractFilterer) WatchCooldownDurationUpdated(opts *bind.WatchOpts, sink chan<- *ContractCooldownDurationUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "CooldownDurationUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractCooldownDurationUpdated)
				if err := _Contract.contract.UnpackLog(event, "CooldownDurationUpdated", log); err != nil {
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

// ParseCooldownDurationUpdated is a log parse operation binding the contract event 0xe865acf13176ceb6a83a16f925f9f67fd03c94ca1af6160d78a7f8a2914253e2.
//
// Solidity: event CooldownDurationUpdated(uint64 newCooldownDuration)
func (_Contract *ContractFilterer) ParseCooldownDurationUpdated(log types.Log) (*ContractCooldownDurationUpdated, error) {
	event := new(ContractCooldownDurationUpdated)
	if err := _Contract.contract.UnpackLog(event, "CooldownDurationUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractDeclareOperatorFeePeriodUpdatedIterator is returned from FilterDeclareOperatorFeePeriodUpdated and is used to iterate over the raw logs and unpacked data for DeclareOperatorFeePeriodUpdated events raised by the Contract contract.
type ContractDeclareOperatorFeePeriodUpdatedIterator struct {
	Event *ContractDeclareOperatorFeePeriodUpdated // Event containing the contract specifics and raw log

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
func (it *ContractDeclareOperatorFeePeriodUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractDeclareOperatorFeePeriodUpdated)
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
		it.Event = new(ContractDeclareOperatorFeePeriodUpdated)
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
func (it *ContractDeclareOperatorFeePeriodUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractDeclareOperatorFeePeriodUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractDeclareOperatorFeePeriodUpdated represents a DeclareOperatorFeePeriodUpdated event raised by the Contract contract.
type ContractDeclareOperatorFeePeriodUpdated struct {
	Value uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterDeclareOperatorFeePeriodUpdated is a free log retrieval operation binding the contract event 0x5fbd75d987b37490f91aa1909db948e7ff14c6ffb495b2f8e0b2334da9b192f1.
//
// Solidity: event DeclareOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) FilterDeclareOperatorFeePeriodUpdated(opts *bind.FilterOpts) (*ContractDeclareOperatorFeePeriodUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "DeclareOperatorFeePeriodUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractDeclareOperatorFeePeriodUpdatedIterator{contract: _Contract.contract, event: "DeclareOperatorFeePeriodUpdated", logs: logs, sub: sub}, nil
}

// WatchDeclareOperatorFeePeriodUpdated is a free log subscription operation binding the contract event 0x5fbd75d987b37490f91aa1909db948e7ff14c6ffb495b2f8e0b2334da9b192f1.
//
// Solidity: event DeclareOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) WatchDeclareOperatorFeePeriodUpdated(opts *bind.WatchOpts, sink chan<- *ContractDeclareOperatorFeePeriodUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "DeclareOperatorFeePeriodUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractDeclareOperatorFeePeriodUpdated)
				if err := _Contract.contract.UnpackLog(event, "DeclareOperatorFeePeriodUpdated", log); err != nil {
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

// ParseDeclareOperatorFeePeriodUpdated is a log parse operation binding the contract event 0x5fbd75d987b37490f91aa1909db948e7ff14c6ffb495b2f8e0b2334da9b192f1.
//
// Solidity: event DeclareOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) ParseDeclareOperatorFeePeriodUpdated(log types.Log) (*ContractDeclareOperatorFeePeriodUpdated, error) {
	event := new(ContractDeclareOperatorFeePeriodUpdated)
	if err := _Contract.contract.UnpackLog(event, "DeclareOperatorFeePeriodUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractERC20RescuedIterator is returned from FilterERC20Rescued and is used to iterate over the raw logs and unpacked data for ERC20Rescued events raised by the Contract contract.
type ContractERC20RescuedIterator struct {
	Event *ContractERC20Rescued // Event containing the contract specifics and raw log

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
func (it *ContractERC20RescuedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractERC20Rescued)
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
		it.Event = new(ContractERC20Rescued)
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
func (it *ContractERC20RescuedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractERC20RescuedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractERC20Rescued represents a ERC20Rescued event raised by the Contract contract.
type ContractERC20Rescued struct {
	Token  common.Address
	To     common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterERC20Rescued is a free log retrieval operation binding the contract event 0x8bbfbb5d7fcacf6fc74005cdede0635561638507f576c95f7f294c22141be2e5.
//
// Solidity: event ERC20Rescued(address indexed token, address indexed to, uint256 amount)
func (_Contract *ContractFilterer) FilterERC20Rescued(opts *bind.FilterOpts, token []common.Address, to []common.Address) (*ContractERC20RescuedIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ERC20Rescued", tokenRule, toRule)
	if err != nil {
		return nil, err
	}
	return &ContractERC20RescuedIterator{contract: _Contract.contract, event: "ERC20Rescued", logs: logs, sub: sub}, nil
}

// WatchERC20Rescued is a free log subscription operation binding the contract event 0x8bbfbb5d7fcacf6fc74005cdede0635561638507f576c95f7f294c22141be2e5.
//
// Solidity: event ERC20Rescued(address indexed token, address indexed to, uint256 amount)
func (_Contract *ContractFilterer) WatchERC20Rescued(opts *bind.WatchOpts, sink chan<- *ContractERC20Rescued, token []common.Address, to []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ERC20Rescued", tokenRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractERC20Rescued)
				if err := _Contract.contract.UnpackLog(event, "ERC20Rescued", log); err != nil {
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

// ParseERC20Rescued is a log parse operation binding the contract event 0x8bbfbb5d7fcacf6fc74005cdede0635561638507f576c95f7f294c22141be2e5.
//
// Solidity: event ERC20Rescued(address indexed token, address indexed to, uint256 amount)
func (_Contract *ContractFilterer) ParseERC20Rescued(log types.Log) (*ContractERC20Rescued, error) {
	event := new(ContractERC20Rescued)
	if err := _Contract.contract.UnpackLog(event, "ERC20Rescued", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractExecuteOperatorFeePeriodUpdatedIterator is returned from FilterExecuteOperatorFeePeriodUpdated and is used to iterate over the raw logs and unpacked data for ExecuteOperatorFeePeriodUpdated events raised by the Contract contract.
type ContractExecuteOperatorFeePeriodUpdatedIterator struct {
	Event *ContractExecuteOperatorFeePeriodUpdated // Event containing the contract specifics and raw log

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
func (it *ContractExecuteOperatorFeePeriodUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractExecuteOperatorFeePeriodUpdated)
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
		it.Event = new(ContractExecuteOperatorFeePeriodUpdated)
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
func (it *ContractExecuteOperatorFeePeriodUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractExecuteOperatorFeePeriodUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractExecuteOperatorFeePeriodUpdated represents a ExecuteOperatorFeePeriodUpdated event raised by the Contract contract.
type ContractExecuteOperatorFeePeriodUpdated struct {
	Value uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterExecuteOperatorFeePeriodUpdated is a free log retrieval operation binding the contract event 0xf6b8a2b45d0a60381de51a7b980c4660d9e5b82db6e07a4d342bfc17a6ff96bf.
//
// Solidity: event ExecuteOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) FilterExecuteOperatorFeePeriodUpdated(opts *bind.FilterOpts) (*ContractExecuteOperatorFeePeriodUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ExecuteOperatorFeePeriodUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractExecuteOperatorFeePeriodUpdatedIterator{contract: _Contract.contract, event: "ExecuteOperatorFeePeriodUpdated", logs: logs, sub: sub}, nil
}

// WatchExecuteOperatorFeePeriodUpdated is a free log subscription operation binding the contract event 0xf6b8a2b45d0a60381de51a7b980c4660d9e5b82db6e07a4d342bfc17a6ff96bf.
//
// Solidity: event ExecuteOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) WatchExecuteOperatorFeePeriodUpdated(opts *bind.WatchOpts, sink chan<- *ContractExecuteOperatorFeePeriodUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ExecuteOperatorFeePeriodUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractExecuteOperatorFeePeriodUpdated)
				if err := _Contract.contract.UnpackLog(event, "ExecuteOperatorFeePeriodUpdated", log); err != nil {
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

// ParseExecuteOperatorFeePeriodUpdated is a log parse operation binding the contract event 0xf6b8a2b45d0a60381de51a7b980c4660d9e5b82db6e07a4d342bfc17a6ff96bf.
//
// Solidity: event ExecuteOperatorFeePeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) ParseExecuteOperatorFeePeriodUpdated(log types.Log) (*ContractExecuteOperatorFeePeriodUpdated, error) {
	event := new(ContractExecuteOperatorFeePeriodUpdated)
	if err := _Contract.contract.UnpackLog(event, "ExecuteOperatorFeePeriodUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractFeeRecipientAddressUpdatedIterator is returned from FilterFeeRecipientAddressUpdated and is used to iterate over the raw logs and unpacked data for FeeRecipientAddressUpdated events raised by the Contract contract.
type ContractFeeRecipientAddressUpdatedIterator struct {
	Event *ContractFeeRecipientAddressUpdated // Event containing the contract specifics and raw log

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
func (it *ContractFeeRecipientAddressUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractFeeRecipientAddressUpdated)
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
		it.Event = new(ContractFeeRecipientAddressUpdated)
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
func (it *ContractFeeRecipientAddressUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractFeeRecipientAddressUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractFeeRecipientAddressUpdated represents a FeeRecipientAddressUpdated event raised by the Contract contract.
type ContractFeeRecipientAddressUpdated struct {
	Owner            common.Address
	RecipientAddress common.Address
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterFeeRecipientAddressUpdated is a free log retrieval operation binding the contract event 0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548.
//
// Solidity: event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress)
func (_Contract *ContractFilterer) FilterFeeRecipientAddressUpdated(opts *bind.FilterOpts, owner []common.Address) (*ContractFeeRecipientAddressUpdatedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "FeeRecipientAddressUpdated", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractFeeRecipientAddressUpdatedIterator{contract: _Contract.contract, event: "FeeRecipientAddressUpdated", logs: logs, sub: sub}, nil
}

// WatchFeeRecipientAddressUpdated is a free log subscription operation binding the contract event 0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548.
//
// Solidity: event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress)
func (_Contract *ContractFilterer) WatchFeeRecipientAddressUpdated(opts *bind.WatchOpts, sink chan<- *ContractFeeRecipientAddressUpdated, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "FeeRecipientAddressUpdated", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractFeeRecipientAddressUpdated)
				if err := _Contract.contract.UnpackLog(event, "FeeRecipientAddressUpdated", log); err != nil {
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
func (_Contract *ContractFilterer) ParseFeeRecipientAddressUpdated(log types.Log) (*ContractFeeRecipientAddressUpdated, error) {
	event := new(ContractFeeRecipientAddressUpdated)
	if err := _Contract.contract.UnpackLog(event, "FeeRecipientAddressUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractFeesSyncedIterator is returned from FilterFeesSynced and is used to iterate over the raw logs and unpacked data for FeesSynced events raised by the Contract contract.
type ContractFeesSyncedIterator struct {
	Event *ContractFeesSynced // Event containing the contract specifics and raw log

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
func (it *ContractFeesSyncedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractFeesSynced)
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
		it.Event = new(ContractFeesSynced)
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
func (it *ContractFeesSyncedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractFeesSyncedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractFeesSynced represents a FeesSynced event raised by the Contract contract.
type ContractFeesSynced struct {
	NewFeesWei     *big.Int
	AccEthPerShare *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterFeesSynced is a free log retrieval operation binding the contract event 0x9386d77e0bcb586105f5c2bee7cabc987f85e7c518f34e580fab935df5d3a9c6.
//
// Solidity: event FeesSynced(uint256 newFeesWei, uint256 accEthPerShare)
func (_Contract *ContractFilterer) FilterFeesSynced(opts *bind.FilterOpts) (*ContractFeesSyncedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "FeesSynced")
	if err != nil {
		return nil, err
	}
	return &ContractFeesSyncedIterator{contract: _Contract.contract, event: "FeesSynced", logs: logs, sub: sub}, nil
}

// WatchFeesSynced is a free log subscription operation binding the contract event 0x9386d77e0bcb586105f5c2bee7cabc987f85e7c518f34e580fab935df5d3a9c6.
//
// Solidity: event FeesSynced(uint256 newFeesWei, uint256 accEthPerShare)
func (_Contract *ContractFilterer) WatchFeesSynced(opts *bind.WatchOpts, sink chan<- *ContractFeesSynced) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "FeesSynced")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractFeesSynced)
				if err := _Contract.contract.UnpackLog(event, "FeesSynced", log); err != nil {
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

// ParseFeesSynced is a log parse operation binding the contract event 0x9386d77e0bcb586105f5c2bee7cabc987f85e7c518f34e580fab935df5d3a9c6.
//
// Solidity: event FeesSynced(uint256 newFeesWei, uint256 accEthPerShare)
func (_Contract *ContractFilterer) ParseFeesSynced(log types.Log) (*ContractFeesSynced, error) {
	event := new(ContractFeesSynced)
	if err := _Contract.contract.UnpackLog(event, "FeesSynced", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the Contract contract.
type ContractInitializedIterator struct {
	Event *ContractInitialized // Event containing the contract specifics and raw log

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
func (it *ContractInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractInitialized)
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
		it.Event = new(ContractInitialized)
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
func (it *ContractInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractInitialized represents a Initialized event raised by the Contract contract.
type ContractInitialized struct {
	Version uint8
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_Contract *ContractFilterer) FilterInitialized(opts *bind.FilterOpts) (*ContractInitializedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &ContractInitializedIterator{contract: _Contract.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_Contract *ContractFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *ContractInitialized) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractInitialized)
				if err := _Contract.contract.UnpackLog(event, "Initialized", log); err != nil {
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

// ParseInitialized is a log parse operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_Contract *ContractFilterer) ParseInitialized(log types.Log) (*ContractInitialized, error) {
	event := new(ContractInitialized)
	if err := _Contract.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractLiquidationThresholdPeriodSSVUpdatedIterator is returned from FilterLiquidationThresholdPeriodSSVUpdated and is used to iterate over the raw logs and unpacked data for LiquidationThresholdPeriodSSVUpdated events raised by the Contract contract.
type ContractLiquidationThresholdPeriodSSVUpdatedIterator struct {
	Event *ContractLiquidationThresholdPeriodSSVUpdated // Event containing the contract specifics and raw log

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
func (it *ContractLiquidationThresholdPeriodSSVUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractLiquidationThresholdPeriodSSVUpdated)
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
		it.Event = new(ContractLiquidationThresholdPeriodSSVUpdated)
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
func (it *ContractLiquidationThresholdPeriodSSVUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractLiquidationThresholdPeriodSSVUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractLiquidationThresholdPeriodSSVUpdated represents a LiquidationThresholdPeriodSSVUpdated event raised by the Contract contract.
type ContractLiquidationThresholdPeriodSSVUpdated struct {
	Value uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterLiquidationThresholdPeriodSSVUpdated is a free log retrieval operation binding the contract event 0xc968a767c53be943fe324987597efaba682968c894cab4291313662c8b99691d.
//
// Solidity: event LiquidationThresholdPeriodSSVUpdated(uint64 value)
func (_Contract *ContractFilterer) FilterLiquidationThresholdPeriodSSVUpdated(opts *bind.FilterOpts) (*ContractLiquidationThresholdPeriodSSVUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "LiquidationThresholdPeriodSSVUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractLiquidationThresholdPeriodSSVUpdatedIterator{contract: _Contract.contract, event: "LiquidationThresholdPeriodSSVUpdated", logs: logs, sub: sub}, nil
}

// WatchLiquidationThresholdPeriodSSVUpdated is a free log subscription operation binding the contract event 0xc968a767c53be943fe324987597efaba682968c894cab4291313662c8b99691d.
//
// Solidity: event LiquidationThresholdPeriodSSVUpdated(uint64 value)
func (_Contract *ContractFilterer) WatchLiquidationThresholdPeriodSSVUpdated(opts *bind.WatchOpts, sink chan<- *ContractLiquidationThresholdPeriodSSVUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "LiquidationThresholdPeriodSSVUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractLiquidationThresholdPeriodSSVUpdated)
				if err := _Contract.contract.UnpackLog(event, "LiquidationThresholdPeriodSSVUpdated", log); err != nil {
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

// ParseLiquidationThresholdPeriodSSVUpdated is a log parse operation binding the contract event 0xc968a767c53be943fe324987597efaba682968c894cab4291313662c8b99691d.
//
// Solidity: event LiquidationThresholdPeriodSSVUpdated(uint64 value)
func (_Contract *ContractFilterer) ParseLiquidationThresholdPeriodSSVUpdated(log types.Log) (*ContractLiquidationThresholdPeriodSSVUpdated, error) {
	event := new(ContractLiquidationThresholdPeriodSSVUpdated)
	if err := _Contract.contract.UnpackLog(event, "LiquidationThresholdPeriodSSVUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractLiquidationThresholdPeriodUpdatedIterator is returned from FilterLiquidationThresholdPeriodUpdated and is used to iterate over the raw logs and unpacked data for LiquidationThresholdPeriodUpdated events raised by the Contract contract.
type ContractLiquidationThresholdPeriodUpdatedIterator struct {
	Event *ContractLiquidationThresholdPeriodUpdated // Event containing the contract specifics and raw log

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
func (it *ContractLiquidationThresholdPeriodUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractLiquidationThresholdPeriodUpdated)
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
		it.Event = new(ContractLiquidationThresholdPeriodUpdated)
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
func (it *ContractLiquidationThresholdPeriodUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractLiquidationThresholdPeriodUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractLiquidationThresholdPeriodUpdated represents a LiquidationThresholdPeriodUpdated event raised by the Contract contract.
type ContractLiquidationThresholdPeriodUpdated struct {
	Value uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterLiquidationThresholdPeriodUpdated is a free log retrieval operation binding the contract event 0x42af14411036d7a50e5e92daf825781450fc8fac8fb65cbdb04720ff08efb84f.
//
// Solidity: event LiquidationThresholdPeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) FilterLiquidationThresholdPeriodUpdated(opts *bind.FilterOpts) (*ContractLiquidationThresholdPeriodUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "LiquidationThresholdPeriodUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractLiquidationThresholdPeriodUpdatedIterator{contract: _Contract.contract, event: "LiquidationThresholdPeriodUpdated", logs: logs, sub: sub}, nil
}

// WatchLiquidationThresholdPeriodUpdated is a free log subscription operation binding the contract event 0x42af14411036d7a50e5e92daf825781450fc8fac8fb65cbdb04720ff08efb84f.
//
// Solidity: event LiquidationThresholdPeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) WatchLiquidationThresholdPeriodUpdated(opts *bind.WatchOpts, sink chan<- *ContractLiquidationThresholdPeriodUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "LiquidationThresholdPeriodUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractLiquidationThresholdPeriodUpdated)
				if err := _Contract.contract.UnpackLog(event, "LiquidationThresholdPeriodUpdated", log); err != nil {
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

// ParseLiquidationThresholdPeriodUpdated is a log parse operation binding the contract event 0x42af14411036d7a50e5e92daf825781450fc8fac8fb65cbdb04720ff08efb84f.
//
// Solidity: event LiquidationThresholdPeriodUpdated(uint64 value)
func (_Contract *ContractFilterer) ParseLiquidationThresholdPeriodUpdated(log types.Log) (*ContractLiquidationThresholdPeriodUpdated, error) {
	event := new(ContractLiquidationThresholdPeriodUpdated)
	if err := _Contract.contract.UnpackLog(event, "LiquidationThresholdPeriodUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractMinBlocksBetweenUpdatesUpdatedIterator is returned from FilterMinBlocksBetweenUpdatesUpdated and is used to iterate over the raw logs and unpacked data for MinBlocksBetweenUpdatesUpdated events raised by the Contract contract.
type ContractMinBlocksBetweenUpdatesUpdatedIterator struct {
	Event *ContractMinBlocksBetweenUpdatesUpdated // Event containing the contract specifics and raw log

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
func (it *ContractMinBlocksBetweenUpdatesUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractMinBlocksBetweenUpdatesUpdated)
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
		it.Event = new(ContractMinBlocksBetweenUpdatesUpdated)
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
func (it *ContractMinBlocksBetweenUpdatesUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractMinBlocksBetweenUpdatesUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractMinBlocksBetweenUpdatesUpdated represents a MinBlocksBetweenUpdatesUpdated event raised by the Contract contract.
type ContractMinBlocksBetweenUpdatesUpdated struct {
	NewMinBlocksBetweenUpdates uint32
	Raw                        types.Log // Blockchain specific contextual infos
}

// FilterMinBlocksBetweenUpdatesUpdated is a free log retrieval operation binding the contract event 0x8794fa1a5292ace029a96ff5db5c03006669acdf358814c3f716ac098b99092e.
//
// Solidity: event MinBlocksBetweenUpdatesUpdated(uint32 newMinBlocksBetweenUpdates)
func (_Contract *ContractFilterer) FilterMinBlocksBetweenUpdatesUpdated(opts *bind.FilterOpts) (*ContractMinBlocksBetweenUpdatesUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "MinBlocksBetweenUpdatesUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractMinBlocksBetweenUpdatesUpdatedIterator{contract: _Contract.contract, event: "MinBlocksBetweenUpdatesUpdated", logs: logs, sub: sub}, nil
}

// WatchMinBlocksBetweenUpdatesUpdated is a free log subscription operation binding the contract event 0x8794fa1a5292ace029a96ff5db5c03006669acdf358814c3f716ac098b99092e.
//
// Solidity: event MinBlocksBetweenUpdatesUpdated(uint32 newMinBlocksBetweenUpdates)
func (_Contract *ContractFilterer) WatchMinBlocksBetweenUpdatesUpdated(opts *bind.WatchOpts, sink chan<- *ContractMinBlocksBetweenUpdatesUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "MinBlocksBetweenUpdatesUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractMinBlocksBetweenUpdatesUpdated)
				if err := _Contract.contract.UnpackLog(event, "MinBlocksBetweenUpdatesUpdated", log); err != nil {
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

// ParseMinBlocksBetweenUpdatesUpdated is a log parse operation binding the contract event 0x8794fa1a5292ace029a96ff5db5c03006669acdf358814c3f716ac098b99092e.
//
// Solidity: event MinBlocksBetweenUpdatesUpdated(uint32 newMinBlocksBetweenUpdates)
func (_Contract *ContractFilterer) ParseMinBlocksBetweenUpdatesUpdated(log types.Log) (*ContractMinBlocksBetweenUpdatesUpdated, error) {
	event := new(ContractMinBlocksBetweenUpdatesUpdated)
	if err := _Contract.contract.UnpackLog(event, "MinBlocksBetweenUpdatesUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractMinimumLiquidationCollateralSSVUpdatedIterator is returned from FilterMinimumLiquidationCollateralSSVUpdated and is used to iterate over the raw logs and unpacked data for MinimumLiquidationCollateralSSVUpdated events raised by the Contract contract.
type ContractMinimumLiquidationCollateralSSVUpdatedIterator struct {
	Event *ContractMinimumLiquidationCollateralSSVUpdated // Event containing the contract specifics and raw log

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
func (it *ContractMinimumLiquidationCollateralSSVUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractMinimumLiquidationCollateralSSVUpdated)
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
		it.Event = new(ContractMinimumLiquidationCollateralSSVUpdated)
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
func (it *ContractMinimumLiquidationCollateralSSVUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractMinimumLiquidationCollateralSSVUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractMinimumLiquidationCollateralSSVUpdated represents a MinimumLiquidationCollateralSSVUpdated event raised by the Contract contract.
type ContractMinimumLiquidationCollateralSSVUpdated struct {
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterMinimumLiquidationCollateralSSVUpdated is a free log retrieval operation binding the contract event 0xc6e0ba7aedc5595dfb313cf8e5bc03109e9f71dc644c019f9acb86fc054ed454.
//
// Solidity: event MinimumLiquidationCollateralSSVUpdated(uint256 value)
func (_Contract *ContractFilterer) FilterMinimumLiquidationCollateralSSVUpdated(opts *bind.FilterOpts) (*ContractMinimumLiquidationCollateralSSVUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "MinimumLiquidationCollateralSSVUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractMinimumLiquidationCollateralSSVUpdatedIterator{contract: _Contract.contract, event: "MinimumLiquidationCollateralSSVUpdated", logs: logs, sub: sub}, nil
}

// WatchMinimumLiquidationCollateralSSVUpdated is a free log subscription operation binding the contract event 0xc6e0ba7aedc5595dfb313cf8e5bc03109e9f71dc644c019f9acb86fc054ed454.
//
// Solidity: event MinimumLiquidationCollateralSSVUpdated(uint256 value)
func (_Contract *ContractFilterer) WatchMinimumLiquidationCollateralSSVUpdated(opts *bind.WatchOpts, sink chan<- *ContractMinimumLiquidationCollateralSSVUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "MinimumLiquidationCollateralSSVUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractMinimumLiquidationCollateralSSVUpdated)
				if err := _Contract.contract.UnpackLog(event, "MinimumLiquidationCollateralSSVUpdated", log); err != nil {
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

// ParseMinimumLiquidationCollateralSSVUpdated is a log parse operation binding the contract event 0xc6e0ba7aedc5595dfb313cf8e5bc03109e9f71dc644c019f9acb86fc054ed454.
//
// Solidity: event MinimumLiquidationCollateralSSVUpdated(uint256 value)
func (_Contract *ContractFilterer) ParseMinimumLiquidationCollateralSSVUpdated(log types.Log) (*ContractMinimumLiquidationCollateralSSVUpdated, error) {
	event := new(ContractMinimumLiquidationCollateralSSVUpdated)
	if err := _Contract.contract.UnpackLog(event, "MinimumLiquidationCollateralSSVUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractMinimumLiquidationCollateralUpdatedIterator is returned from FilterMinimumLiquidationCollateralUpdated and is used to iterate over the raw logs and unpacked data for MinimumLiquidationCollateralUpdated events raised by the Contract contract.
type ContractMinimumLiquidationCollateralUpdatedIterator struct {
	Event *ContractMinimumLiquidationCollateralUpdated // Event containing the contract specifics and raw log

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
func (it *ContractMinimumLiquidationCollateralUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractMinimumLiquidationCollateralUpdated)
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
		it.Event = new(ContractMinimumLiquidationCollateralUpdated)
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
func (it *ContractMinimumLiquidationCollateralUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractMinimumLiquidationCollateralUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractMinimumLiquidationCollateralUpdated represents a MinimumLiquidationCollateralUpdated event raised by the Contract contract.
type ContractMinimumLiquidationCollateralUpdated struct {
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterMinimumLiquidationCollateralUpdated is a free log retrieval operation binding the contract event 0xd363ab4392efaf967a89d8616cba1ff0c6f05a04c2f214671be365f0fab05960.
//
// Solidity: event MinimumLiquidationCollateralUpdated(uint256 value)
func (_Contract *ContractFilterer) FilterMinimumLiquidationCollateralUpdated(opts *bind.FilterOpts) (*ContractMinimumLiquidationCollateralUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "MinimumLiquidationCollateralUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractMinimumLiquidationCollateralUpdatedIterator{contract: _Contract.contract, event: "MinimumLiquidationCollateralUpdated", logs: logs, sub: sub}, nil
}

// WatchMinimumLiquidationCollateralUpdated is a free log subscription operation binding the contract event 0xd363ab4392efaf967a89d8616cba1ff0c6f05a04c2f214671be365f0fab05960.
//
// Solidity: event MinimumLiquidationCollateralUpdated(uint256 value)
func (_Contract *ContractFilterer) WatchMinimumLiquidationCollateralUpdated(opts *bind.WatchOpts, sink chan<- *ContractMinimumLiquidationCollateralUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "MinimumLiquidationCollateralUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractMinimumLiquidationCollateralUpdated)
				if err := _Contract.contract.UnpackLog(event, "MinimumLiquidationCollateralUpdated", log); err != nil {
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

// ParseMinimumLiquidationCollateralUpdated is a log parse operation binding the contract event 0xd363ab4392efaf967a89d8616cba1ff0c6f05a04c2f214671be365f0fab05960.
//
// Solidity: event MinimumLiquidationCollateralUpdated(uint256 value)
func (_Contract *ContractFilterer) ParseMinimumLiquidationCollateralUpdated(log types.Log) (*ContractMinimumLiquidationCollateralUpdated, error) {
	event := new(ContractMinimumLiquidationCollateralUpdated)
	if err := _Contract.contract.UnpackLog(event, "MinimumLiquidationCollateralUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractMinimumOperatorEthFeeUpdatedIterator is returned from FilterMinimumOperatorEthFeeUpdated and is used to iterate over the raw logs and unpacked data for MinimumOperatorEthFeeUpdated events raised by the Contract contract.
type ContractMinimumOperatorEthFeeUpdatedIterator struct {
	Event *ContractMinimumOperatorEthFeeUpdated // Event containing the contract specifics and raw log

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
func (it *ContractMinimumOperatorEthFeeUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractMinimumOperatorEthFeeUpdated)
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
		it.Event = new(ContractMinimumOperatorEthFeeUpdated)
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
func (it *ContractMinimumOperatorEthFeeUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractMinimumOperatorEthFeeUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractMinimumOperatorEthFeeUpdated represents a MinimumOperatorEthFeeUpdated event raised by the Contract contract.
type ContractMinimumOperatorEthFeeUpdated struct {
	MinFee *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterMinimumOperatorEthFeeUpdated is a free log retrieval operation binding the contract event 0x18d44c1ee4bcb1ecb7d57ca44a74ede64c7c9accda084b97d466e6a0998329b3.
//
// Solidity: event MinimumOperatorEthFeeUpdated(uint256 minFee)
func (_Contract *ContractFilterer) FilterMinimumOperatorEthFeeUpdated(opts *bind.FilterOpts) (*ContractMinimumOperatorEthFeeUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "MinimumOperatorEthFeeUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractMinimumOperatorEthFeeUpdatedIterator{contract: _Contract.contract, event: "MinimumOperatorEthFeeUpdated", logs: logs, sub: sub}, nil
}

// WatchMinimumOperatorEthFeeUpdated is a free log subscription operation binding the contract event 0x18d44c1ee4bcb1ecb7d57ca44a74ede64c7c9accda084b97d466e6a0998329b3.
//
// Solidity: event MinimumOperatorEthFeeUpdated(uint256 minFee)
func (_Contract *ContractFilterer) WatchMinimumOperatorEthFeeUpdated(opts *bind.WatchOpts, sink chan<- *ContractMinimumOperatorEthFeeUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "MinimumOperatorEthFeeUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractMinimumOperatorEthFeeUpdated)
				if err := _Contract.contract.UnpackLog(event, "MinimumOperatorEthFeeUpdated", log); err != nil {
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

// ParseMinimumOperatorEthFeeUpdated is a log parse operation binding the contract event 0x18d44c1ee4bcb1ecb7d57ca44a74ede64c7c9accda084b97d466e6a0998329b3.
//
// Solidity: event MinimumOperatorEthFeeUpdated(uint256 minFee)
func (_Contract *ContractFilterer) ParseMinimumOperatorEthFeeUpdated(log types.Log) (*ContractMinimumOperatorEthFeeUpdated, error) {
	event := new(ContractMinimumOperatorEthFeeUpdated)
	if err := _Contract.contract.UnpackLog(event, "MinimumOperatorEthFeeUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractModuleUpgradedIterator is returned from FilterModuleUpgraded and is used to iterate over the raw logs and unpacked data for ModuleUpgraded events raised by the Contract contract.
type ContractModuleUpgradedIterator struct {
	Event *ContractModuleUpgraded // Event containing the contract specifics and raw log

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
func (it *ContractModuleUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractModuleUpgraded)
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
		it.Event = new(ContractModuleUpgraded)
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
func (it *ContractModuleUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractModuleUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractModuleUpgraded represents a ModuleUpgraded event raised by the Contract contract.
type ContractModuleUpgraded struct {
	ModuleId      uint8
	ModuleAddress common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterModuleUpgraded is a free log retrieval operation binding the contract event 0xfdf54bf052398eb41c923eb1bd596351c5e72b99959d1ca529a7f13c0a2503d7.
//
// Solidity: event ModuleUpgraded(uint8 indexed moduleId, address moduleAddress)
func (_Contract *ContractFilterer) FilterModuleUpgraded(opts *bind.FilterOpts, moduleId []uint8) (*ContractModuleUpgradedIterator, error) {

	var moduleIdRule []interface{}
	for _, moduleIdItem := range moduleId {
		moduleIdRule = append(moduleIdRule, moduleIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ModuleUpgraded", moduleIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractModuleUpgradedIterator{contract: _Contract.contract, event: "ModuleUpgraded", logs: logs, sub: sub}, nil
}

// WatchModuleUpgraded is a free log subscription operation binding the contract event 0xfdf54bf052398eb41c923eb1bd596351c5e72b99959d1ca529a7f13c0a2503d7.
//
// Solidity: event ModuleUpgraded(uint8 indexed moduleId, address moduleAddress)
func (_Contract *ContractFilterer) WatchModuleUpgraded(opts *bind.WatchOpts, sink chan<- *ContractModuleUpgraded, moduleId []uint8) (event.Subscription, error) {

	var moduleIdRule []interface{}
	for _, moduleIdItem := range moduleId {
		moduleIdRule = append(moduleIdRule, moduleIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ModuleUpgraded", moduleIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractModuleUpgraded)
				if err := _Contract.contract.UnpackLog(event, "ModuleUpgraded", log); err != nil {
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

// ParseModuleUpgraded is a log parse operation binding the contract event 0xfdf54bf052398eb41c923eb1bd596351c5e72b99959d1ca529a7f13c0a2503d7.
//
// Solidity: event ModuleUpgraded(uint8 indexed moduleId, address moduleAddress)
func (_Contract *ContractFilterer) ParseModuleUpgraded(log types.Log) (*ContractModuleUpgraded, error) {
	event := new(ContractModuleUpgraded)
	if err := _Contract.contract.UnpackLog(event, "ModuleUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractNetworkEarningsWithdrawnIterator is returned from FilterNetworkEarningsWithdrawn and is used to iterate over the raw logs and unpacked data for NetworkEarningsWithdrawn events raised by the Contract contract.
type ContractNetworkEarningsWithdrawnIterator struct {
	Event *ContractNetworkEarningsWithdrawn // Event containing the contract specifics and raw log

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
func (it *ContractNetworkEarningsWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractNetworkEarningsWithdrawn)
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
		it.Event = new(ContractNetworkEarningsWithdrawn)
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
func (it *ContractNetworkEarningsWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractNetworkEarningsWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractNetworkEarningsWithdrawn represents a NetworkEarningsWithdrawn event raised by the Contract contract.
type ContractNetworkEarningsWithdrawn struct {
	Value     *big.Int
	Recipient common.Address
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterNetworkEarningsWithdrawn is a free log retrieval operation binding the contract event 0x370342c3bb9245e20bffe6dced02ba2fceca979701f881d5adc72d838e83f1c5.
//
// Solidity: event NetworkEarningsWithdrawn(uint256 value, address recipient)
func (_Contract *ContractFilterer) FilterNetworkEarningsWithdrawn(opts *bind.FilterOpts) (*ContractNetworkEarningsWithdrawnIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "NetworkEarningsWithdrawn")
	if err != nil {
		return nil, err
	}
	return &ContractNetworkEarningsWithdrawnIterator{contract: _Contract.contract, event: "NetworkEarningsWithdrawn", logs: logs, sub: sub}, nil
}

// WatchNetworkEarningsWithdrawn is a free log subscription operation binding the contract event 0x370342c3bb9245e20bffe6dced02ba2fceca979701f881d5adc72d838e83f1c5.
//
// Solidity: event NetworkEarningsWithdrawn(uint256 value, address recipient)
func (_Contract *ContractFilterer) WatchNetworkEarningsWithdrawn(opts *bind.WatchOpts, sink chan<- *ContractNetworkEarningsWithdrawn) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "NetworkEarningsWithdrawn")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractNetworkEarningsWithdrawn)
				if err := _Contract.contract.UnpackLog(event, "NetworkEarningsWithdrawn", log); err != nil {
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

// ParseNetworkEarningsWithdrawn is a log parse operation binding the contract event 0x370342c3bb9245e20bffe6dced02ba2fceca979701f881d5adc72d838e83f1c5.
//
// Solidity: event NetworkEarningsWithdrawn(uint256 value, address recipient)
func (_Contract *ContractFilterer) ParseNetworkEarningsWithdrawn(log types.Log) (*ContractNetworkEarningsWithdrawn, error) {
	event := new(ContractNetworkEarningsWithdrawn)
	if err := _Contract.contract.UnpackLog(event, "NetworkEarningsWithdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractNetworkFeeUpdatedIterator is returned from FilterNetworkFeeUpdated and is used to iterate over the raw logs and unpacked data for NetworkFeeUpdated events raised by the Contract contract.
type ContractNetworkFeeUpdatedIterator struct {
	Event *ContractNetworkFeeUpdated // Event containing the contract specifics and raw log

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
func (it *ContractNetworkFeeUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractNetworkFeeUpdated)
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
		it.Event = new(ContractNetworkFeeUpdated)
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
func (it *ContractNetworkFeeUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractNetworkFeeUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractNetworkFeeUpdated represents a NetworkFeeUpdated event raised by the Contract contract.
type ContractNetworkFeeUpdated struct {
	OldFee *big.Int
	NewFee *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterNetworkFeeUpdated is a free log retrieval operation binding the contract event 0x8f49a76c5d617bd72673d92d3a019ff8f04f204536aae7a3d10e7ca85603f3cc.
//
// Solidity: event NetworkFeeUpdated(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) FilterNetworkFeeUpdated(opts *bind.FilterOpts) (*ContractNetworkFeeUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "NetworkFeeUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractNetworkFeeUpdatedIterator{contract: _Contract.contract, event: "NetworkFeeUpdated", logs: logs, sub: sub}, nil
}

// WatchNetworkFeeUpdated is a free log subscription operation binding the contract event 0x8f49a76c5d617bd72673d92d3a019ff8f04f204536aae7a3d10e7ca85603f3cc.
//
// Solidity: event NetworkFeeUpdated(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) WatchNetworkFeeUpdated(opts *bind.WatchOpts, sink chan<- *ContractNetworkFeeUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "NetworkFeeUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractNetworkFeeUpdated)
				if err := _Contract.contract.UnpackLog(event, "NetworkFeeUpdated", log); err != nil {
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

// ParseNetworkFeeUpdated is a log parse operation binding the contract event 0x8f49a76c5d617bd72673d92d3a019ff8f04f204536aae7a3d10e7ca85603f3cc.
//
// Solidity: event NetworkFeeUpdated(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) ParseNetworkFeeUpdated(log types.Log) (*ContractNetworkFeeUpdated, error) {
	event := new(ContractNetworkFeeUpdated)
	if err := _Contract.contract.UnpackLog(event, "NetworkFeeUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractNetworkFeeUpdatedSSVIterator is returned from FilterNetworkFeeUpdatedSSV and is used to iterate over the raw logs and unpacked data for NetworkFeeUpdatedSSV events raised by the Contract contract.
type ContractNetworkFeeUpdatedSSVIterator struct {
	Event *ContractNetworkFeeUpdatedSSV // Event containing the contract specifics and raw log

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
func (it *ContractNetworkFeeUpdatedSSVIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractNetworkFeeUpdatedSSV)
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
		it.Event = new(ContractNetworkFeeUpdatedSSV)
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
func (it *ContractNetworkFeeUpdatedSSVIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractNetworkFeeUpdatedSSVIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractNetworkFeeUpdatedSSV represents a NetworkFeeUpdatedSSV event raised by the Contract contract.
type ContractNetworkFeeUpdatedSSV struct {
	OldFee *big.Int
	NewFee *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterNetworkFeeUpdatedSSV is a free log retrieval operation binding the contract event 0x67b4581d242b51c956246a35859809bc259dfd07d622b4bc0a9858641a6297fb.
//
// Solidity: event NetworkFeeUpdatedSSV(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) FilterNetworkFeeUpdatedSSV(opts *bind.FilterOpts) (*ContractNetworkFeeUpdatedSSVIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "NetworkFeeUpdatedSSV")
	if err != nil {
		return nil, err
	}
	return &ContractNetworkFeeUpdatedSSVIterator{contract: _Contract.contract, event: "NetworkFeeUpdatedSSV", logs: logs, sub: sub}, nil
}

// WatchNetworkFeeUpdatedSSV is a free log subscription operation binding the contract event 0x67b4581d242b51c956246a35859809bc259dfd07d622b4bc0a9858641a6297fb.
//
// Solidity: event NetworkFeeUpdatedSSV(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) WatchNetworkFeeUpdatedSSV(opts *bind.WatchOpts, sink chan<- *ContractNetworkFeeUpdatedSSV) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "NetworkFeeUpdatedSSV")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractNetworkFeeUpdatedSSV)
				if err := _Contract.contract.UnpackLog(event, "NetworkFeeUpdatedSSV", log); err != nil {
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

// ParseNetworkFeeUpdatedSSV is a log parse operation binding the contract event 0x67b4581d242b51c956246a35859809bc259dfd07d622b4bc0a9858641a6297fb.
//
// Solidity: event NetworkFeeUpdatedSSV(uint256 oldFee, uint256 newFee)
func (_Contract *ContractFilterer) ParseNetworkFeeUpdatedSSV(log types.Log) (*ContractNetworkFeeUpdatedSSV, error) {
	event := new(ContractNetworkFeeUpdatedSSV)
	if err := _Contract.contract.UnpackLog(event, "NetworkFeeUpdatedSSV", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorAddedIterator is returned from FilterOperatorAdded and is used to iterate over the raw logs and unpacked data for OperatorAdded events raised by the Contract contract.
type ContractOperatorAddedIterator struct {
	Event *ContractOperatorAdded // Event containing the contract specifics and raw log

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
func (it *ContractOperatorAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorAdded)
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
		it.Event = new(ContractOperatorAdded)
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
func (it *ContractOperatorAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorAdded represents a OperatorAdded event raised by the Contract contract.
type ContractOperatorAdded struct {
	OperatorId uint64
	Owner      common.Address
	PublicKey  []byte
	Fee        *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorAdded is a free log retrieval operation binding the contract event 0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4.
//
// Solidity: event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee)
func (_Contract *ContractFilterer) FilterOperatorAdded(opts *bind.FilterOpts, operatorId []uint64, owner []common.Address) (*ContractOperatorAddedIterator, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorAdded", operatorIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorAddedIterator{contract: _Contract.contract, event: "OperatorAdded", logs: logs, sub: sub}, nil
}

// WatchOperatorAdded is a free log subscription operation binding the contract event 0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4.
//
// Solidity: event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee)
func (_Contract *ContractFilterer) WatchOperatorAdded(opts *bind.WatchOpts, sink chan<- *ContractOperatorAdded, operatorId []uint64, owner []common.Address) (event.Subscription, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorAdded", operatorIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorAdded)
				if err := _Contract.contract.UnpackLog(event, "OperatorAdded", log); err != nil {
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
func (_Contract *ContractFilterer) ParseOperatorAdded(log types.Log) (*ContractOperatorAdded, error) {
	event := new(ContractOperatorAdded)
	if err := _Contract.contract.UnpackLog(event, "OperatorAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorFeeDeclarationCancelledIterator is returned from FilterOperatorFeeDeclarationCancelled and is used to iterate over the raw logs and unpacked data for OperatorFeeDeclarationCancelled events raised by the Contract contract.
type ContractOperatorFeeDeclarationCancelledIterator struct {
	Event *ContractOperatorFeeDeclarationCancelled // Event containing the contract specifics and raw log

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
func (it *ContractOperatorFeeDeclarationCancelledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorFeeDeclarationCancelled)
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
		it.Event = new(ContractOperatorFeeDeclarationCancelled)
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
func (it *ContractOperatorFeeDeclarationCancelledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorFeeDeclarationCancelledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorFeeDeclarationCancelled represents a OperatorFeeDeclarationCancelled event raised by the Contract contract.
type ContractOperatorFeeDeclarationCancelled struct {
	Owner      common.Address
	OperatorId uint64
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorFeeDeclarationCancelled is a free log retrieval operation binding the contract event 0x5055fa347441172447637c015e80a3ee748b9382212ceb5dca5a3683298fd6f3.
//
// Solidity: event OperatorFeeDeclarationCancelled(address indexed owner, uint64 indexed operatorId)
func (_Contract *ContractFilterer) FilterOperatorFeeDeclarationCancelled(opts *bind.FilterOpts, owner []common.Address, operatorId []uint64) (*ContractOperatorFeeDeclarationCancelledIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorFeeDeclarationCancelled", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorFeeDeclarationCancelledIterator{contract: _Contract.contract, event: "OperatorFeeDeclarationCancelled", logs: logs, sub: sub}, nil
}

// WatchOperatorFeeDeclarationCancelled is a free log subscription operation binding the contract event 0x5055fa347441172447637c015e80a3ee748b9382212ceb5dca5a3683298fd6f3.
//
// Solidity: event OperatorFeeDeclarationCancelled(address indexed owner, uint64 indexed operatorId)
func (_Contract *ContractFilterer) WatchOperatorFeeDeclarationCancelled(opts *bind.WatchOpts, sink chan<- *ContractOperatorFeeDeclarationCancelled, owner []common.Address, operatorId []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorFeeDeclarationCancelled", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorFeeDeclarationCancelled)
				if err := _Contract.contract.UnpackLog(event, "OperatorFeeDeclarationCancelled", log); err != nil {
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

// ParseOperatorFeeDeclarationCancelled is a log parse operation binding the contract event 0x5055fa347441172447637c015e80a3ee748b9382212ceb5dca5a3683298fd6f3.
//
// Solidity: event OperatorFeeDeclarationCancelled(address indexed owner, uint64 indexed operatorId)
func (_Contract *ContractFilterer) ParseOperatorFeeDeclarationCancelled(log types.Log) (*ContractOperatorFeeDeclarationCancelled, error) {
	event := new(ContractOperatorFeeDeclarationCancelled)
	if err := _Contract.contract.UnpackLog(event, "OperatorFeeDeclarationCancelled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorFeeDeclaredIterator is returned from FilterOperatorFeeDeclared and is used to iterate over the raw logs and unpacked data for OperatorFeeDeclared events raised by the Contract contract.
type ContractOperatorFeeDeclaredIterator struct {
	Event *ContractOperatorFeeDeclared // Event containing the contract specifics and raw log

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
func (it *ContractOperatorFeeDeclaredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorFeeDeclared)
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
		it.Event = new(ContractOperatorFeeDeclared)
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
func (it *ContractOperatorFeeDeclaredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorFeeDeclaredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorFeeDeclared represents a OperatorFeeDeclared event raised by the Contract contract.
type ContractOperatorFeeDeclared struct {
	Owner       common.Address
	OperatorId  uint64
	BlockNumber *big.Int
	Fee         *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterOperatorFeeDeclared is a free log retrieval operation binding the contract event 0x796204296f2eb56d7432fa85961e9750d0cb21741873ebf7077e28263e327358.
//
// Solidity: event OperatorFeeDeclared(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) FilterOperatorFeeDeclared(opts *bind.FilterOpts, owner []common.Address, operatorId []uint64) (*ContractOperatorFeeDeclaredIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorFeeDeclared", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorFeeDeclaredIterator{contract: _Contract.contract, event: "OperatorFeeDeclared", logs: logs, sub: sub}, nil
}

// WatchOperatorFeeDeclared is a free log subscription operation binding the contract event 0x796204296f2eb56d7432fa85961e9750d0cb21741873ebf7077e28263e327358.
//
// Solidity: event OperatorFeeDeclared(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) WatchOperatorFeeDeclared(opts *bind.WatchOpts, sink chan<- *ContractOperatorFeeDeclared, owner []common.Address, operatorId []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorFeeDeclared", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorFeeDeclared)
				if err := _Contract.contract.UnpackLog(event, "OperatorFeeDeclared", log); err != nil {
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

// ParseOperatorFeeDeclared is a log parse operation binding the contract event 0x796204296f2eb56d7432fa85961e9750d0cb21741873ebf7077e28263e327358.
//
// Solidity: event OperatorFeeDeclared(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) ParseOperatorFeeDeclared(log types.Log) (*ContractOperatorFeeDeclared, error) {
	event := new(ContractOperatorFeeDeclared)
	if err := _Contract.contract.UnpackLog(event, "OperatorFeeDeclared", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorFeeExecutedIterator is returned from FilterOperatorFeeExecuted and is used to iterate over the raw logs and unpacked data for OperatorFeeExecuted events raised by the Contract contract.
type ContractOperatorFeeExecutedIterator struct {
	Event *ContractOperatorFeeExecuted // Event containing the contract specifics and raw log

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
func (it *ContractOperatorFeeExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorFeeExecuted)
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
		it.Event = new(ContractOperatorFeeExecuted)
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
func (it *ContractOperatorFeeExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorFeeExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorFeeExecuted represents a OperatorFeeExecuted event raised by the Contract contract.
type ContractOperatorFeeExecuted struct {
	Owner       common.Address
	OperatorId  uint64
	BlockNumber *big.Int
	Fee         *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterOperatorFeeExecuted is a free log retrieval operation binding the contract event 0x513e931ff778ed01e676d55880d8db185c29b0094546ff2b3e9f5b6920d16bef.
//
// Solidity: event OperatorFeeExecuted(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) FilterOperatorFeeExecuted(opts *bind.FilterOpts, owner []common.Address, operatorId []uint64) (*ContractOperatorFeeExecutedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorFeeExecuted", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorFeeExecutedIterator{contract: _Contract.contract, event: "OperatorFeeExecuted", logs: logs, sub: sub}, nil
}

// WatchOperatorFeeExecuted is a free log subscription operation binding the contract event 0x513e931ff778ed01e676d55880d8db185c29b0094546ff2b3e9f5b6920d16bef.
//
// Solidity: event OperatorFeeExecuted(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) WatchOperatorFeeExecuted(opts *bind.WatchOpts, sink chan<- *ContractOperatorFeeExecuted, owner []common.Address, operatorId []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorFeeExecuted", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorFeeExecuted)
				if err := _Contract.contract.UnpackLog(event, "OperatorFeeExecuted", log); err != nil {
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

// ParseOperatorFeeExecuted is a log parse operation binding the contract event 0x513e931ff778ed01e676d55880d8db185c29b0094546ff2b3e9f5b6920d16bef.
//
// Solidity: event OperatorFeeExecuted(address indexed owner, uint64 indexed operatorId, uint256 blockNumber, uint256 fee)
func (_Contract *ContractFilterer) ParseOperatorFeeExecuted(log types.Log) (*ContractOperatorFeeExecuted, error) {
	event := new(ContractOperatorFeeExecuted)
	if err := _Contract.contract.UnpackLog(event, "OperatorFeeExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorFeeIncreaseLimitUpdatedIterator is returned from FilterOperatorFeeIncreaseLimitUpdated and is used to iterate over the raw logs and unpacked data for OperatorFeeIncreaseLimitUpdated events raised by the Contract contract.
type ContractOperatorFeeIncreaseLimitUpdatedIterator struct {
	Event *ContractOperatorFeeIncreaseLimitUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorFeeIncreaseLimitUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorFeeIncreaseLimitUpdated)
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
		it.Event = new(ContractOperatorFeeIncreaseLimitUpdated)
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
func (it *ContractOperatorFeeIncreaseLimitUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorFeeIncreaseLimitUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorFeeIncreaseLimitUpdated represents a OperatorFeeIncreaseLimitUpdated event raised by the Contract contract.
type ContractOperatorFeeIncreaseLimitUpdated struct {
	Value uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterOperatorFeeIncreaseLimitUpdated is a free log retrieval operation binding the contract event 0x2fff7e5a48a4befc2c2be4d77e141f6d97907798977ce452429ec55c2658a342.
//
// Solidity: event OperatorFeeIncreaseLimitUpdated(uint64 value)
func (_Contract *ContractFilterer) FilterOperatorFeeIncreaseLimitUpdated(opts *bind.FilterOpts) (*ContractOperatorFeeIncreaseLimitUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorFeeIncreaseLimitUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorFeeIncreaseLimitUpdatedIterator{contract: _Contract.contract, event: "OperatorFeeIncreaseLimitUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorFeeIncreaseLimitUpdated is a free log subscription operation binding the contract event 0x2fff7e5a48a4befc2c2be4d77e141f6d97907798977ce452429ec55c2658a342.
//
// Solidity: event OperatorFeeIncreaseLimitUpdated(uint64 value)
func (_Contract *ContractFilterer) WatchOperatorFeeIncreaseLimitUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorFeeIncreaseLimitUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorFeeIncreaseLimitUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorFeeIncreaseLimitUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorFeeIncreaseLimitUpdated", log); err != nil {
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

// ParseOperatorFeeIncreaseLimitUpdated is a log parse operation binding the contract event 0x2fff7e5a48a4befc2c2be4d77e141f6d97907798977ce452429ec55c2658a342.
//
// Solidity: event OperatorFeeIncreaseLimitUpdated(uint64 value)
func (_Contract *ContractFilterer) ParseOperatorFeeIncreaseLimitUpdated(log types.Log) (*ContractOperatorFeeIncreaseLimitUpdated, error) {
	event := new(ContractOperatorFeeIncreaseLimitUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorFeeIncreaseLimitUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorMaximumFeeUpdatedIterator is returned from FilterOperatorMaximumFeeUpdated and is used to iterate over the raw logs and unpacked data for OperatorMaximumFeeUpdated events raised by the Contract contract.
type ContractOperatorMaximumFeeUpdatedIterator struct {
	Event *ContractOperatorMaximumFeeUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorMaximumFeeUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorMaximumFeeUpdated)
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
		it.Event = new(ContractOperatorMaximumFeeUpdated)
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
func (it *ContractOperatorMaximumFeeUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorMaximumFeeUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorMaximumFeeUpdated represents a OperatorMaximumFeeUpdated event raised by the Contract contract.
type ContractOperatorMaximumFeeUpdated struct {
	MaxFee *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterOperatorMaximumFeeUpdated is a free log retrieval operation binding the contract event 0xbb018dc682e13c04ee4a5608c89a9f287c402c1aec96588d6dd85478179b4bba.
//
// Solidity: event OperatorMaximumFeeUpdated(uint256 maxFee)
func (_Contract *ContractFilterer) FilterOperatorMaximumFeeUpdated(opts *bind.FilterOpts) (*ContractOperatorMaximumFeeUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorMaximumFeeUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorMaximumFeeUpdatedIterator{contract: _Contract.contract, event: "OperatorMaximumFeeUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorMaximumFeeUpdated is a free log subscription operation binding the contract event 0xbb018dc682e13c04ee4a5608c89a9f287c402c1aec96588d6dd85478179b4bba.
//
// Solidity: event OperatorMaximumFeeUpdated(uint256 maxFee)
func (_Contract *ContractFilterer) WatchOperatorMaximumFeeUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorMaximumFeeUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorMaximumFeeUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorMaximumFeeUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorMaximumFeeUpdated", log); err != nil {
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

// ParseOperatorMaximumFeeUpdated is a log parse operation binding the contract event 0xbb018dc682e13c04ee4a5608c89a9f287c402c1aec96588d6dd85478179b4bba.
//
// Solidity: event OperatorMaximumFeeUpdated(uint256 maxFee)
func (_Contract *ContractFilterer) ParseOperatorMaximumFeeUpdated(log types.Log) (*ContractOperatorMaximumFeeUpdated, error) {
	event := new(ContractOperatorMaximumFeeUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorMaximumFeeUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorMultipleWhitelistRemovedIterator is returned from FilterOperatorMultipleWhitelistRemoved and is used to iterate over the raw logs and unpacked data for OperatorMultipleWhitelistRemoved events raised by the Contract contract.
type ContractOperatorMultipleWhitelistRemovedIterator struct {
	Event *ContractOperatorMultipleWhitelistRemoved // Event containing the contract specifics and raw log

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
func (it *ContractOperatorMultipleWhitelistRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorMultipleWhitelistRemoved)
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
		it.Event = new(ContractOperatorMultipleWhitelistRemoved)
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
func (it *ContractOperatorMultipleWhitelistRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorMultipleWhitelistRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorMultipleWhitelistRemoved represents a OperatorMultipleWhitelistRemoved event raised by the Contract contract.
type ContractOperatorMultipleWhitelistRemoved struct {
	OperatorIds        []uint64
	WhitelistAddresses []common.Address
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterOperatorMultipleWhitelistRemoved is a free log retrieval operation binding the contract event 0x589a71ef5bb37432c8ce279a4afc32783592f1764c6fcb07e3c437e80c80ab2e.
//
// Solidity: event OperatorMultipleWhitelistRemoved(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) FilterOperatorMultipleWhitelistRemoved(opts *bind.FilterOpts) (*ContractOperatorMultipleWhitelistRemovedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorMultipleWhitelistRemoved")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorMultipleWhitelistRemovedIterator{contract: _Contract.contract, event: "OperatorMultipleWhitelistRemoved", logs: logs, sub: sub}, nil
}

// WatchOperatorMultipleWhitelistRemoved is a free log subscription operation binding the contract event 0x589a71ef5bb37432c8ce279a4afc32783592f1764c6fcb07e3c437e80c80ab2e.
//
// Solidity: event OperatorMultipleWhitelistRemoved(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) WatchOperatorMultipleWhitelistRemoved(opts *bind.WatchOpts, sink chan<- *ContractOperatorMultipleWhitelistRemoved) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorMultipleWhitelistRemoved")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorMultipleWhitelistRemoved)
				if err := _Contract.contract.UnpackLog(event, "OperatorMultipleWhitelistRemoved", log); err != nil {
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

// ParseOperatorMultipleWhitelistRemoved is a log parse operation binding the contract event 0x589a71ef5bb37432c8ce279a4afc32783592f1764c6fcb07e3c437e80c80ab2e.
//
// Solidity: event OperatorMultipleWhitelistRemoved(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) ParseOperatorMultipleWhitelistRemoved(log types.Log) (*ContractOperatorMultipleWhitelistRemoved, error) {
	event := new(ContractOperatorMultipleWhitelistRemoved)
	if err := _Contract.contract.UnpackLog(event, "OperatorMultipleWhitelistRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorMultipleWhitelistUpdatedIterator is returned from FilterOperatorMultipleWhitelistUpdated and is used to iterate over the raw logs and unpacked data for OperatorMultipleWhitelistUpdated events raised by the Contract contract.
type ContractOperatorMultipleWhitelistUpdatedIterator struct {
	Event *ContractOperatorMultipleWhitelistUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorMultipleWhitelistUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorMultipleWhitelistUpdated)
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
		it.Event = new(ContractOperatorMultipleWhitelistUpdated)
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
func (it *ContractOperatorMultipleWhitelistUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorMultipleWhitelistUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorMultipleWhitelistUpdated represents a OperatorMultipleWhitelistUpdated event raised by the Contract contract.
type ContractOperatorMultipleWhitelistUpdated struct {
	OperatorIds        []uint64
	WhitelistAddresses []common.Address
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterOperatorMultipleWhitelistUpdated is a free log retrieval operation binding the contract event 0x3d5869fa1ed68d6b7b5e2a1f44df8e1e7edd8ea7a6cc240e45c72e2eb3523962.
//
// Solidity: event OperatorMultipleWhitelistUpdated(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) FilterOperatorMultipleWhitelistUpdated(opts *bind.FilterOpts) (*ContractOperatorMultipleWhitelistUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorMultipleWhitelistUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorMultipleWhitelistUpdatedIterator{contract: _Contract.contract, event: "OperatorMultipleWhitelistUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorMultipleWhitelistUpdated is a free log subscription operation binding the contract event 0x3d5869fa1ed68d6b7b5e2a1f44df8e1e7edd8ea7a6cc240e45c72e2eb3523962.
//
// Solidity: event OperatorMultipleWhitelistUpdated(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) WatchOperatorMultipleWhitelistUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorMultipleWhitelistUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorMultipleWhitelistUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorMultipleWhitelistUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorMultipleWhitelistUpdated", log); err != nil {
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

// ParseOperatorMultipleWhitelistUpdated is a log parse operation binding the contract event 0x3d5869fa1ed68d6b7b5e2a1f44df8e1e7edd8ea7a6cc240e45c72e2eb3523962.
//
// Solidity: event OperatorMultipleWhitelistUpdated(uint64[] operatorIds, address[] whitelistAddresses)
func (_Contract *ContractFilterer) ParseOperatorMultipleWhitelistUpdated(log types.Log) (*ContractOperatorMultipleWhitelistUpdated, error) {
	event := new(ContractOperatorMultipleWhitelistUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorMultipleWhitelistUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorPrivacyStatusUpdatedIterator is returned from FilterOperatorPrivacyStatusUpdated and is used to iterate over the raw logs and unpacked data for OperatorPrivacyStatusUpdated events raised by the Contract contract.
type ContractOperatorPrivacyStatusUpdatedIterator struct {
	Event *ContractOperatorPrivacyStatusUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorPrivacyStatusUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorPrivacyStatusUpdated)
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
		it.Event = new(ContractOperatorPrivacyStatusUpdated)
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
func (it *ContractOperatorPrivacyStatusUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorPrivacyStatusUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorPrivacyStatusUpdated represents a OperatorPrivacyStatusUpdated event raised by the Contract contract.
type ContractOperatorPrivacyStatusUpdated struct {
	OperatorIds []uint64
	ToPrivate   bool
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterOperatorPrivacyStatusUpdated is a free log retrieval operation binding the contract event 0x7cae2703330c3f53308fb0fe3a9143f335997ba7e059b9ac8e4417ed8fbddbd3.
//
// Solidity: event OperatorPrivacyStatusUpdated(uint64[] operatorIds, bool toPrivate)
func (_Contract *ContractFilterer) FilterOperatorPrivacyStatusUpdated(opts *bind.FilterOpts) (*ContractOperatorPrivacyStatusUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorPrivacyStatusUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorPrivacyStatusUpdatedIterator{contract: _Contract.contract, event: "OperatorPrivacyStatusUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorPrivacyStatusUpdated is a free log subscription operation binding the contract event 0x7cae2703330c3f53308fb0fe3a9143f335997ba7e059b9ac8e4417ed8fbddbd3.
//
// Solidity: event OperatorPrivacyStatusUpdated(uint64[] operatorIds, bool toPrivate)
func (_Contract *ContractFilterer) WatchOperatorPrivacyStatusUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorPrivacyStatusUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorPrivacyStatusUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorPrivacyStatusUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorPrivacyStatusUpdated", log); err != nil {
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

// ParseOperatorPrivacyStatusUpdated is a log parse operation binding the contract event 0x7cae2703330c3f53308fb0fe3a9143f335997ba7e059b9ac8e4417ed8fbddbd3.
//
// Solidity: event OperatorPrivacyStatusUpdated(uint64[] operatorIds, bool toPrivate)
func (_Contract *ContractFilterer) ParseOperatorPrivacyStatusUpdated(log types.Log) (*ContractOperatorPrivacyStatusUpdated, error) {
	event := new(ContractOperatorPrivacyStatusUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorPrivacyStatusUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorRemovedIterator is returned from FilterOperatorRemoved and is used to iterate over the raw logs and unpacked data for OperatorRemoved events raised by the Contract contract.
type ContractOperatorRemovedIterator struct {
	Event *ContractOperatorRemoved // Event containing the contract specifics and raw log

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
func (it *ContractOperatorRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorRemoved)
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
		it.Event = new(ContractOperatorRemoved)
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
func (it *ContractOperatorRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorRemoved represents a OperatorRemoved event raised by the Contract contract.
type ContractOperatorRemoved struct {
	OperatorId uint64
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorRemoved is a free log retrieval operation binding the contract event 0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e.
//
// Solidity: event OperatorRemoved(uint64 indexed operatorId)
func (_Contract *ContractFilterer) FilterOperatorRemoved(opts *bind.FilterOpts, operatorId []uint64) (*ContractOperatorRemovedIterator, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorRemoved", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorRemovedIterator{contract: _Contract.contract, event: "OperatorRemoved", logs: logs, sub: sub}, nil
}

// WatchOperatorRemoved is a free log subscription operation binding the contract event 0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e.
//
// Solidity: event OperatorRemoved(uint64 indexed operatorId)
func (_Contract *ContractFilterer) WatchOperatorRemoved(opts *bind.WatchOpts, sink chan<- *ContractOperatorRemoved, operatorId []uint64) (event.Subscription, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorRemoved", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorRemoved)
				if err := _Contract.contract.UnpackLog(event, "OperatorRemoved", log); err != nil {
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
func (_Contract *ContractFilterer) ParseOperatorRemoved(log types.Log) (*ContractOperatorRemoved, error) {
	event := new(ContractOperatorRemoved)
	if err := _Contract.contract.UnpackLog(event, "OperatorRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorWhitelistUpdatedIterator is returned from FilterOperatorWhitelistUpdated and is used to iterate over the raw logs and unpacked data for OperatorWhitelistUpdated events raised by the Contract contract.
type ContractOperatorWhitelistUpdatedIterator struct {
	Event *ContractOperatorWhitelistUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorWhitelistUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorWhitelistUpdated)
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
		it.Event = new(ContractOperatorWhitelistUpdated)
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
func (it *ContractOperatorWhitelistUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorWhitelistUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorWhitelistUpdated represents a OperatorWhitelistUpdated event raised by the Contract contract.
type ContractOperatorWhitelistUpdated struct {
	OperatorId  uint64
	Whitelisted common.Address
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterOperatorWhitelistUpdated is a free log retrieval operation binding the contract event 0x29f72634ccb172103f8c542da23de7f6cf9bce724c5bb91bd6f3a516b14c63fe.
//
// Solidity: event OperatorWhitelistUpdated(uint64 indexed operatorId, address whitelisted)
func (_Contract *ContractFilterer) FilterOperatorWhitelistUpdated(opts *bind.FilterOpts, operatorId []uint64) (*ContractOperatorWhitelistUpdatedIterator, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorWhitelistUpdated", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorWhitelistUpdatedIterator{contract: _Contract.contract, event: "OperatorWhitelistUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorWhitelistUpdated is a free log subscription operation binding the contract event 0x29f72634ccb172103f8c542da23de7f6cf9bce724c5bb91bd6f3a516b14c63fe.
//
// Solidity: event OperatorWhitelistUpdated(uint64 indexed operatorId, address whitelisted)
func (_Contract *ContractFilterer) WatchOperatorWhitelistUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorWhitelistUpdated, operatorId []uint64) (event.Subscription, error) {

	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorWhitelistUpdated", operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorWhitelistUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorWhitelistUpdated", log); err != nil {
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

// ParseOperatorWhitelistUpdated is a log parse operation binding the contract event 0x29f72634ccb172103f8c542da23de7f6cf9bce724c5bb91bd6f3a516b14c63fe.
//
// Solidity: event OperatorWhitelistUpdated(uint64 indexed operatorId, address whitelisted)
func (_Contract *ContractFilterer) ParseOperatorWhitelistUpdated(log types.Log) (*ContractOperatorWhitelistUpdated, error) {
	event := new(ContractOperatorWhitelistUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorWhitelistUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorWhitelistingContractUpdatedIterator is returned from FilterOperatorWhitelistingContractUpdated and is used to iterate over the raw logs and unpacked data for OperatorWhitelistingContractUpdated events raised by the Contract contract.
type ContractOperatorWhitelistingContractUpdatedIterator struct {
	Event *ContractOperatorWhitelistingContractUpdated // Event containing the contract specifics and raw log

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
func (it *ContractOperatorWhitelistingContractUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorWhitelistingContractUpdated)
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
		it.Event = new(ContractOperatorWhitelistingContractUpdated)
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
func (it *ContractOperatorWhitelistingContractUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorWhitelistingContractUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorWhitelistingContractUpdated represents a OperatorWhitelistingContractUpdated event raised by the Contract contract.
type ContractOperatorWhitelistingContractUpdated struct {
	OperatorIds          []uint64
	WhitelistingContract common.Address
	Raw                  types.Log // Blockchain specific contextual infos
}

// FilterOperatorWhitelistingContractUpdated is a free log retrieval operation binding the contract event 0xf41d8ca981ff900f6db7f71d7e2ae866eae8e4327d23e5c692c13a6c43b39c3d.
//
// Solidity: event OperatorWhitelistingContractUpdated(uint64[] operatorIds, address whitelistingContract)
func (_Contract *ContractFilterer) FilterOperatorWhitelistingContractUpdated(opts *bind.FilterOpts) (*ContractOperatorWhitelistingContractUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorWhitelistingContractUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractOperatorWhitelistingContractUpdatedIterator{contract: _Contract.contract, event: "OperatorWhitelistingContractUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorWhitelistingContractUpdated is a free log subscription operation binding the contract event 0xf41d8ca981ff900f6db7f71d7e2ae866eae8e4327d23e5c692c13a6c43b39c3d.
//
// Solidity: event OperatorWhitelistingContractUpdated(uint64[] operatorIds, address whitelistingContract)
func (_Contract *ContractFilterer) WatchOperatorWhitelistingContractUpdated(opts *bind.WatchOpts, sink chan<- *ContractOperatorWhitelistingContractUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorWhitelistingContractUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorWhitelistingContractUpdated)
				if err := _Contract.contract.UnpackLog(event, "OperatorWhitelistingContractUpdated", log); err != nil {
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

// ParseOperatorWhitelistingContractUpdated is a log parse operation binding the contract event 0xf41d8ca981ff900f6db7f71d7e2ae866eae8e4327d23e5c692c13a6c43b39c3d.
//
// Solidity: event OperatorWhitelistingContractUpdated(uint64[] operatorIds, address whitelistingContract)
func (_Contract *ContractFilterer) ParseOperatorWhitelistingContractUpdated(log types.Log) (*ContractOperatorWhitelistingContractUpdated, error) {
	event := new(ContractOperatorWhitelistingContractUpdated)
	if err := _Contract.contract.UnpackLog(event, "OperatorWhitelistingContractUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorWithdrawnIterator is returned from FilterOperatorWithdrawn and is used to iterate over the raw logs and unpacked data for OperatorWithdrawn events raised by the Contract contract.
type ContractOperatorWithdrawnIterator struct {
	Event *ContractOperatorWithdrawn // Event containing the contract specifics and raw log

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
func (it *ContractOperatorWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorWithdrawn)
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
		it.Event = new(ContractOperatorWithdrawn)
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
func (it *ContractOperatorWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorWithdrawn represents a OperatorWithdrawn event raised by the Contract contract.
type ContractOperatorWithdrawn struct {
	Owner      common.Address
	OperatorId uint64
	Value      *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorWithdrawn is a free log retrieval operation binding the contract event 0x178bf78bdd8914b8483d640b4a4f84e20943b5eb6b639b7474286364c7651d60.
//
// Solidity: event OperatorWithdrawn(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) FilterOperatorWithdrawn(opts *bind.FilterOpts, owner []common.Address, operatorId []uint64) (*ContractOperatorWithdrawnIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorWithdrawn", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorWithdrawnIterator{contract: _Contract.contract, event: "OperatorWithdrawn", logs: logs, sub: sub}, nil
}

// WatchOperatorWithdrawn is a free log subscription operation binding the contract event 0x178bf78bdd8914b8483d640b4a4f84e20943b5eb6b639b7474286364c7651d60.
//
// Solidity: event OperatorWithdrawn(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) WatchOperatorWithdrawn(opts *bind.WatchOpts, sink chan<- *ContractOperatorWithdrawn, owner []common.Address, operatorId []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorWithdrawn", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorWithdrawn)
				if err := _Contract.contract.UnpackLog(event, "OperatorWithdrawn", log); err != nil {
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

// ParseOperatorWithdrawn is a log parse operation binding the contract event 0x178bf78bdd8914b8483d640b4a4f84e20943b5eb6b639b7474286364c7651d60.
//
// Solidity: event OperatorWithdrawn(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) ParseOperatorWithdrawn(log types.Log) (*ContractOperatorWithdrawn, error) {
	event := new(ContractOperatorWithdrawn)
	if err := _Contract.contract.UnpackLog(event, "OperatorWithdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOperatorWithdrawnSSVIterator is returned from FilterOperatorWithdrawnSSV and is used to iterate over the raw logs and unpacked data for OperatorWithdrawnSSV events raised by the Contract contract.
type ContractOperatorWithdrawnSSVIterator struct {
	Event *ContractOperatorWithdrawnSSV // Event containing the contract specifics and raw log

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
func (it *ContractOperatorWithdrawnSSVIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOperatorWithdrawnSSV)
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
		it.Event = new(ContractOperatorWithdrawnSSV)
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
func (it *ContractOperatorWithdrawnSSVIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOperatorWithdrawnSSVIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOperatorWithdrawnSSV represents a OperatorWithdrawnSSV event raised by the Contract contract.
type ContractOperatorWithdrawnSSV struct {
	Owner      common.Address
	OperatorId uint64
	Value      *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterOperatorWithdrawnSSV is a free log retrieval operation binding the contract event 0x8ce3fab60deb285381bb700bc8406adc165eba737126273b52f7dd5280576f91.
//
// Solidity: event OperatorWithdrawnSSV(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) FilterOperatorWithdrawnSSV(opts *bind.FilterOpts, owner []common.Address, operatorId []uint64) (*ContractOperatorWithdrawnSSVIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OperatorWithdrawnSSV", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractOperatorWithdrawnSSVIterator{contract: _Contract.contract, event: "OperatorWithdrawnSSV", logs: logs, sub: sub}, nil
}

// WatchOperatorWithdrawnSSV is a free log subscription operation binding the contract event 0x8ce3fab60deb285381bb700bc8406adc165eba737126273b52f7dd5280576f91.
//
// Solidity: event OperatorWithdrawnSSV(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) WatchOperatorWithdrawnSSV(opts *bind.WatchOpts, sink chan<- *ContractOperatorWithdrawnSSV, owner []common.Address, operatorId []uint64) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var operatorIdRule []interface{}
	for _, operatorIdItem := range operatorId {
		operatorIdRule = append(operatorIdRule, operatorIdItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OperatorWithdrawnSSV", ownerRule, operatorIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOperatorWithdrawnSSV)
				if err := _Contract.contract.UnpackLog(event, "OperatorWithdrawnSSV", log); err != nil {
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

// ParseOperatorWithdrawnSSV is a log parse operation binding the contract event 0x8ce3fab60deb285381bb700bc8406adc165eba737126273b52f7dd5280576f91.
//
// Solidity: event OperatorWithdrawnSSV(address indexed owner, uint64 indexed operatorId, uint256 value)
func (_Contract *ContractFilterer) ParseOperatorWithdrawnSSV(log types.Log) (*ContractOperatorWithdrawnSSV, error) {
	event := new(ContractOperatorWithdrawnSSV)
	if err := _Contract.contract.UnpackLog(event, "OperatorWithdrawnSSV", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOracleReplacedIterator is returned from FilterOracleReplaced and is used to iterate over the raw logs and unpacked data for OracleReplaced events raised by the Contract contract.
type ContractOracleReplacedIterator struct {
	Event *ContractOracleReplaced // Event containing the contract specifics and raw log

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
func (it *ContractOracleReplacedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOracleReplaced)
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
		it.Event = new(ContractOracleReplaced)
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
func (it *ContractOracleReplacedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOracleReplacedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOracleReplaced represents a OracleReplaced event raised by the Contract contract.
type ContractOracleReplaced struct {
	OracleId  uint32
	OldOracle common.Address
	NewOracle common.Address
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterOracleReplaced is a free log retrieval operation binding the contract event 0x6113130a2400e4e0980410314b8d41a250e526ca56d039ffd91278aeac44c468.
//
// Solidity: event OracleReplaced(uint32 indexed oracleId, address indexed oldOracle, address indexed newOracle)
func (_Contract *ContractFilterer) FilterOracleReplaced(opts *bind.FilterOpts, oracleId []uint32, oldOracle []common.Address, newOracle []common.Address) (*ContractOracleReplacedIterator, error) {

	var oracleIdRule []interface{}
	for _, oracleIdItem := range oracleId {
		oracleIdRule = append(oracleIdRule, oracleIdItem)
	}
	var oldOracleRule []interface{}
	for _, oldOracleItem := range oldOracle {
		oldOracleRule = append(oldOracleRule, oldOracleItem)
	}
	var newOracleRule []interface{}
	for _, newOracleItem := range newOracle {
		newOracleRule = append(newOracleRule, newOracleItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OracleReplaced", oracleIdRule, oldOracleRule, newOracleRule)
	if err != nil {
		return nil, err
	}
	return &ContractOracleReplacedIterator{contract: _Contract.contract, event: "OracleReplaced", logs: logs, sub: sub}, nil
}

// WatchOracleReplaced is a free log subscription operation binding the contract event 0x6113130a2400e4e0980410314b8d41a250e526ca56d039ffd91278aeac44c468.
//
// Solidity: event OracleReplaced(uint32 indexed oracleId, address indexed oldOracle, address indexed newOracle)
func (_Contract *ContractFilterer) WatchOracleReplaced(opts *bind.WatchOpts, sink chan<- *ContractOracleReplaced, oracleId []uint32, oldOracle []common.Address, newOracle []common.Address) (event.Subscription, error) {

	var oracleIdRule []interface{}
	for _, oracleIdItem := range oracleId {
		oracleIdRule = append(oracleIdRule, oracleIdItem)
	}
	var oldOracleRule []interface{}
	for _, oldOracleItem := range oldOracle {
		oldOracleRule = append(oldOracleRule, oldOracleItem)
	}
	var newOracleRule []interface{}
	for _, newOracleItem := range newOracle {
		newOracleRule = append(newOracleRule, newOracleItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OracleReplaced", oracleIdRule, oldOracleRule, newOracleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOracleReplaced)
				if err := _Contract.contract.UnpackLog(event, "OracleReplaced", log); err != nil {
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

// ParseOracleReplaced is a log parse operation binding the contract event 0x6113130a2400e4e0980410314b8d41a250e526ca56d039ffd91278aeac44c468.
//
// Solidity: event OracleReplaced(uint32 indexed oracleId, address indexed oldOracle, address indexed newOracle)
func (_Contract *ContractFilterer) ParseOracleReplaced(log types.Log) (*ContractOracleReplaced, error) {
	event := new(ContractOracleReplaced)
	if err := _Contract.contract.UnpackLog(event, "OracleReplaced", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOwnershipTransferStartedIterator is returned from FilterOwnershipTransferStarted and is used to iterate over the raw logs and unpacked data for OwnershipTransferStarted events raised by the Contract contract.
type ContractOwnershipTransferStartedIterator struct {
	Event *ContractOwnershipTransferStarted // Event containing the contract specifics and raw log

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
func (it *ContractOwnershipTransferStartedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOwnershipTransferStarted)
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
		it.Event = new(ContractOwnershipTransferStarted)
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
func (it *ContractOwnershipTransferStartedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOwnershipTransferStartedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOwnershipTransferStarted represents a OwnershipTransferStarted event raised by the Contract contract.
type ContractOwnershipTransferStarted struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferStarted is a free log retrieval operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) FilterOwnershipTransferStarted(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ContractOwnershipTransferStartedIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ContractOwnershipTransferStartedIterator{contract: _Contract.contract, event: "OwnershipTransferStarted", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferStarted is a free log subscription operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) WatchOwnershipTransferStarted(opts *bind.WatchOpts, sink chan<- *ContractOwnershipTransferStarted, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOwnershipTransferStarted)
				if err := _Contract.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
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

// ParseOwnershipTransferStarted is a log parse operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) ParseOwnershipTransferStarted(log types.Log) (*ContractOwnershipTransferStarted, error) {
	event := new(ContractOwnershipTransferStarted)
	if err := _Contract.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the Contract contract.
type ContractOwnershipTransferredIterator struct {
	Event *ContractOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *ContractOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractOwnershipTransferred)
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
		it.Event = new(ContractOwnershipTransferred)
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
func (it *ContractOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractOwnershipTransferred represents a OwnershipTransferred event raised by the Contract contract.
type ContractOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ContractOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ContractOwnershipTransferredIterator{contract: _Contract.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ContractOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractOwnershipTransferred)
				if err := _Contract.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_Contract *ContractFilterer) ParseOwnershipTransferred(log types.Log) (*ContractOwnershipTransferred, error) {
	event := new(ContractOwnershipTransferred)
	if err := _Contract.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractQuorumUpdatedIterator is returned from FilterQuorumUpdated and is used to iterate over the raw logs and unpacked data for QuorumUpdated events raised by the Contract contract.
type ContractQuorumUpdatedIterator struct {
	Event *ContractQuorumUpdated // Event containing the contract specifics and raw log

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
func (it *ContractQuorumUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractQuorumUpdated)
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
		it.Event = new(ContractQuorumUpdated)
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
func (it *ContractQuorumUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractQuorumUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractQuorumUpdated represents a QuorumUpdated event raised by the Contract contract.
type ContractQuorumUpdated struct {
	NewQuorum uint16
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterQuorumUpdated is a free log retrieval operation binding the contract event 0x791dc99be5109fa13cd62186968189a4f65235f0a4aea35cdf15c585bbcbfdaf.
//
// Solidity: event QuorumUpdated(uint16 newQuorum)
func (_Contract *ContractFilterer) FilterQuorumUpdated(opts *bind.FilterOpts) (*ContractQuorumUpdatedIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "QuorumUpdated")
	if err != nil {
		return nil, err
	}
	return &ContractQuorumUpdatedIterator{contract: _Contract.contract, event: "QuorumUpdated", logs: logs, sub: sub}, nil
}

// WatchQuorumUpdated is a free log subscription operation binding the contract event 0x791dc99be5109fa13cd62186968189a4f65235f0a4aea35cdf15c585bbcbfdaf.
//
// Solidity: event QuorumUpdated(uint16 newQuorum)
func (_Contract *ContractFilterer) WatchQuorumUpdated(opts *bind.WatchOpts, sink chan<- *ContractQuorumUpdated) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "QuorumUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractQuorumUpdated)
				if err := _Contract.contract.UnpackLog(event, "QuorumUpdated", log); err != nil {
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

// ParseQuorumUpdated is a log parse operation binding the contract event 0x791dc99be5109fa13cd62186968189a4f65235f0a4aea35cdf15c585bbcbfdaf.
//
// Solidity: event QuorumUpdated(uint16 newQuorum)
func (_Contract *ContractFilterer) ParseQuorumUpdated(log types.Log) (*ContractQuorumUpdated, error) {
	event := new(ContractQuorumUpdated)
	if err := _Contract.contract.UnpackLog(event, "QuorumUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractRewardsClaimedIterator is returned from FilterRewardsClaimed and is used to iterate over the raw logs and unpacked data for RewardsClaimed events raised by the Contract contract.
type ContractRewardsClaimedIterator struct {
	Event *ContractRewardsClaimed // Event containing the contract specifics and raw log

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
func (it *ContractRewardsClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractRewardsClaimed)
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
		it.Event = new(ContractRewardsClaimed)
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
func (it *ContractRewardsClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractRewardsClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractRewardsClaimed represents a RewardsClaimed event raised by the Contract contract.
type ContractRewardsClaimed struct {
	User   common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterRewardsClaimed is a free log retrieval operation binding the contract event 0xfc30cddea38e2bf4d6ea7d3f9ed3b6ad7f176419f4963bd81318067a4aee73fe.
//
// Solidity: event RewardsClaimed(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) FilterRewardsClaimed(opts *bind.FilterOpts, user []common.Address) (*ContractRewardsClaimedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "RewardsClaimed", userRule)
	if err != nil {
		return nil, err
	}
	return &ContractRewardsClaimedIterator{contract: _Contract.contract, event: "RewardsClaimed", logs: logs, sub: sub}, nil
}

// WatchRewardsClaimed is a free log subscription operation binding the contract event 0xfc30cddea38e2bf4d6ea7d3f9ed3b6ad7f176419f4963bd81318067a4aee73fe.
//
// Solidity: event RewardsClaimed(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) WatchRewardsClaimed(opts *bind.WatchOpts, sink chan<- *ContractRewardsClaimed, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "RewardsClaimed", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractRewardsClaimed)
				if err := _Contract.contract.UnpackLog(event, "RewardsClaimed", log); err != nil {
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

// ParseRewardsClaimed is a log parse operation binding the contract event 0xfc30cddea38e2bf4d6ea7d3f9ed3b6ad7f176419f4963bd81318067a4aee73fe.
//
// Solidity: event RewardsClaimed(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) ParseRewardsClaimed(log types.Log) (*ContractRewardsClaimed, error) {
	event := new(ContractRewardsClaimed)
	if err := _Contract.contract.UnpackLog(event, "RewardsClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractRewardsSettledIterator is returned from FilterRewardsSettled and is used to iterate over the raw logs and unpacked data for RewardsSettled events raised by the Contract contract.
type ContractRewardsSettledIterator struct {
	Event *ContractRewardsSettled // Event containing the contract specifics and raw log

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
func (it *ContractRewardsSettledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractRewardsSettled)
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
		it.Event = new(ContractRewardsSettled)
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
func (it *ContractRewardsSettledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractRewardsSettledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractRewardsSettled represents a RewardsSettled event raised by the Contract contract.
type ContractRewardsSettled struct {
	User      common.Address
	Pending   *big.Int
	Accrued   *big.Int
	UserIndex *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterRewardsSettled is a free log retrieval operation binding the contract event 0x4e38a3eebabe29eaad79f76c94599208a54f204cbd7d3907ac066df5afcb5057.
//
// Solidity: event RewardsSettled(address indexed user, uint256 pending, uint256 accrued, uint256 userIndex)
func (_Contract *ContractFilterer) FilterRewardsSettled(opts *bind.FilterOpts, user []common.Address) (*ContractRewardsSettledIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "RewardsSettled", userRule)
	if err != nil {
		return nil, err
	}
	return &ContractRewardsSettledIterator{contract: _Contract.contract, event: "RewardsSettled", logs: logs, sub: sub}, nil
}

// WatchRewardsSettled is a free log subscription operation binding the contract event 0x4e38a3eebabe29eaad79f76c94599208a54f204cbd7d3907ac066df5afcb5057.
//
// Solidity: event RewardsSettled(address indexed user, uint256 pending, uint256 accrued, uint256 userIndex)
func (_Contract *ContractFilterer) WatchRewardsSettled(opts *bind.WatchOpts, sink chan<- *ContractRewardsSettled, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "RewardsSettled", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractRewardsSettled)
				if err := _Contract.contract.UnpackLog(event, "RewardsSettled", log); err != nil {
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

// ParseRewardsSettled is a log parse operation binding the contract event 0x4e38a3eebabe29eaad79f76c94599208a54f204cbd7d3907ac066df5afcb5057.
//
// Solidity: event RewardsSettled(address indexed user, uint256 pending, uint256 accrued, uint256 userIndex)
func (_Contract *ContractFilterer) ParseRewardsSettled(log types.Log) (*ContractRewardsSettled, error) {
	event := new(ContractRewardsSettled)
	if err := _Contract.contract.UnpackLog(event, "RewardsSettled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractRootCommittedIterator is returned from FilterRootCommitted and is used to iterate over the raw logs and unpacked data for RootCommitted events raised by the Contract contract.
type ContractRootCommittedIterator struct {
	Event *ContractRootCommitted // Event containing the contract specifics and raw log

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
func (it *ContractRootCommittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractRootCommitted)
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
		it.Event = new(ContractRootCommitted)
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
func (it *ContractRootCommittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractRootCommittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractRootCommitted represents a RootCommitted event raised by the Contract contract.
type ContractRootCommitted struct {
	MerkleRoot [32]byte
	BlockNum   uint64
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRootCommitted is a free log retrieval operation binding the contract event 0x6965b9c8509097ad68ba0eb17fcb9436f4a72a057fe5ac556737580bce80d840.
//
// Solidity: event RootCommitted(bytes32 indexed merkleRoot, uint64 indexed blockNum)
func (_Contract *ContractFilterer) FilterRootCommitted(opts *bind.FilterOpts, merkleRoot [][32]byte, blockNum []uint64) (*ContractRootCommittedIterator, error) {

	var merkleRootRule []interface{}
	for _, merkleRootItem := range merkleRoot {
		merkleRootRule = append(merkleRootRule, merkleRootItem)
	}
	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "RootCommitted", merkleRootRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return &ContractRootCommittedIterator{contract: _Contract.contract, event: "RootCommitted", logs: logs, sub: sub}, nil
}

// WatchRootCommitted is a free log subscription operation binding the contract event 0x6965b9c8509097ad68ba0eb17fcb9436f4a72a057fe5ac556737580bce80d840.
//
// Solidity: event RootCommitted(bytes32 indexed merkleRoot, uint64 indexed blockNum)
func (_Contract *ContractFilterer) WatchRootCommitted(opts *bind.WatchOpts, sink chan<- *ContractRootCommitted, merkleRoot [][32]byte, blockNum []uint64) (event.Subscription, error) {

	var merkleRootRule []interface{}
	for _, merkleRootItem := range merkleRoot {
		merkleRootRule = append(merkleRootRule, merkleRootItem)
	}
	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "RootCommitted", merkleRootRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractRootCommitted)
				if err := _Contract.contract.UnpackLog(event, "RootCommitted", log); err != nil {
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

// ParseRootCommitted is a log parse operation binding the contract event 0x6965b9c8509097ad68ba0eb17fcb9436f4a72a057fe5ac556737580bce80d840.
//
// Solidity: event RootCommitted(bytes32 indexed merkleRoot, uint64 indexed blockNum)
func (_Contract *ContractFilterer) ParseRootCommitted(log types.Log) (*ContractRootCommitted, error) {
	event := new(ContractRootCommitted)
	if err := _Contract.contract.UnpackLog(event, "RootCommitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractSSVNetworkUpgradeBlockIterator is returned from FilterSSVNetworkUpgradeBlock and is used to iterate over the raw logs and unpacked data for SSVNetworkUpgradeBlock events raised by the Contract contract.
type ContractSSVNetworkUpgradeBlockIterator struct {
	Event *ContractSSVNetworkUpgradeBlock // Event containing the contract specifics and raw log

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
func (it *ContractSSVNetworkUpgradeBlockIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractSSVNetworkUpgradeBlock)
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
		it.Event = new(ContractSSVNetworkUpgradeBlock)
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
func (it *ContractSSVNetworkUpgradeBlockIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractSSVNetworkUpgradeBlockIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractSSVNetworkUpgradeBlock represents a SSVNetworkUpgradeBlock event raised by the Contract contract.
type ContractSSVNetworkUpgradeBlock struct {
	Version     string
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterSSVNetworkUpgradeBlock is a free log retrieval operation binding the contract event 0xe733b31bc9141b87ce3fb540e27a03b4711c1214ec06490c8e68daf82bc33911.
//
// Solidity: event SSVNetworkUpgradeBlock(string version, uint256 blockNumber)
func (_Contract *ContractFilterer) FilterSSVNetworkUpgradeBlock(opts *bind.FilterOpts) (*ContractSSVNetworkUpgradeBlockIterator, error) {

	logs, sub, err := _Contract.contract.FilterLogs(opts, "SSVNetworkUpgradeBlock")
	if err != nil {
		return nil, err
	}
	return &ContractSSVNetworkUpgradeBlockIterator{contract: _Contract.contract, event: "SSVNetworkUpgradeBlock", logs: logs, sub: sub}, nil
}

// WatchSSVNetworkUpgradeBlock is a free log subscription operation binding the contract event 0xe733b31bc9141b87ce3fb540e27a03b4711c1214ec06490c8e68daf82bc33911.
//
// Solidity: event SSVNetworkUpgradeBlock(string version, uint256 blockNumber)
func (_Contract *ContractFilterer) WatchSSVNetworkUpgradeBlock(opts *bind.WatchOpts, sink chan<- *ContractSSVNetworkUpgradeBlock) (event.Subscription, error) {

	logs, sub, err := _Contract.contract.WatchLogs(opts, "SSVNetworkUpgradeBlock")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractSSVNetworkUpgradeBlock)
				if err := _Contract.contract.UnpackLog(event, "SSVNetworkUpgradeBlock", log); err != nil {
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

// ParseSSVNetworkUpgradeBlock is a log parse operation binding the contract event 0xe733b31bc9141b87ce3fb540e27a03b4711c1214ec06490c8e68daf82bc33911.
//
// Solidity: event SSVNetworkUpgradeBlock(string version, uint256 blockNumber)
func (_Contract *ContractFilterer) ParseSSVNetworkUpgradeBlock(log types.Log) (*ContractSSVNetworkUpgradeBlock, error) {
	event := new(ContractSSVNetworkUpgradeBlock)
	if err := _Contract.contract.UnpackLog(event, "SSVNetworkUpgradeBlock", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractStakedIterator is returned from FilterStaked and is used to iterate over the raw logs and unpacked data for Staked events raised by the Contract contract.
type ContractStakedIterator struct {
	Event *ContractStaked // Event containing the contract specifics and raw log

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
func (it *ContractStakedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractStaked)
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
		it.Event = new(ContractStaked)
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
func (it *ContractStakedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractStakedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractStaked represents a Staked event raised by the Contract contract.
type ContractStaked struct {
	User   common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterStaked is a free log retrieval operation binding the contract event 0x9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d.
//
// Solidity: event Staked(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) FilterStaked(opts *bind.FilterOpts, user []common.Address) (*ContractStakedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "Staked", userRule)
	if err != nil {
		return nil, err
	}
	return &ContractStakedIterator{contract: _Contract.contract, event: "Staked", logs: logs, sub: sub}, nil
}

// WatchStaked is a free log subscription operation binding the contract event 0x9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d.
//
// Solidity: event Staked(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) WatchStaked(opts *bind.WatchOpts, sink chan<- *ContractStaked, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "Staked", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractStaked)
				if err := _Contract.contract.UnpackLog(event, "Staked", log); err != nil {
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

// ParseStaked is a log parse operation binding the contract event 0x9e71bc8eea02a63969f509818f2dafb9254532904319f9dbda79b67bd34a5f3d.
//
// Solidity: event Staked(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) ParseStaked(log types.Log) (*ContractStaked, error) {
	event := new(ContractStaked)
	if err := _Contract.contract.UnpackLog(event, "Staked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractUnstakeRequestedIterator is returned from FilterUnstakeRequested and is used to iterate over the raw logs and unpacked data for UnstakeRequested events raised by the Contract contract.
type ContractUnstakeRequestedIterator struct {
	Event *ContractUnstakeRequested // Event containing the contract specifics and raw log

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
func (it *ContractUnstakeRequestedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractUnstakeRequested)
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
		it.Event = new(ContractUnstakeRequested)
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
func (it *ContractUnstakeRequestedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractUnstakeRequestedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractUnstakeRequested represents a UnstakeRequested event raised by the Contract contract.
type ContractUnstakeRequested struct {
	User       common.Address
	Amount     *big.Int
	UnlockTime *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterUnstakeRequested is a free log retrieval operation binding the contract event 0x57e41df54512c76148b5ba9b643d149752b0d35e493b969bd017d0a3fe5228cf.
//
// Solidity: event UnstakeRequested(address indexed user, uint256 amount, uint256 unlockTime)
func (_Contract *ContractFilterer) FilterUnstakeRequested(opts *bind.FilterOpts, user []common.Address) (*ContractUnstakeRequestedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "UnstakeRequested", userRule)
	if err != nil {
		return nil, err
	}
	return &ContractUnstakeRequestedIterator{contract: _Contract.contract, event: "UnstakeRequested", logs: logs, sub: sub}, nil
}

// WatchUnstakeRequested is a free log subscription operation binding the contract event 0x57e41df54512c76148b5ba9b643d149752b0d35e493b969bd017d0a3fe5228cf.
//
// Solidity: event UnstakeRequested(address indexed user, uint256 amount, uint256 unlockTime)
func (_Contract *ContractFilterer) WatchUnstakeRequested(opts *bind.WatchOpts, sink chan<- *ContractUnstakeRequested, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "UnstakeRequested", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractUnstakeRequested)
				if err := _Contract.contract.UnpackLog(event, "UnstakeRequested", log); err != nil {
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

// ParseUnstakeRequested is a log parse operation binding the contract event 0x57e41df54512c76148b5ba9b643d149752b0d35e493b969bd017d0a3fe5228cf.
//
// Solidity: event UnstakeRequested(address indexed user, uint256 amount, uint256 unlockTime)
func (_Contract *ContractFilterer) ParseUnstakeRequested(log types.Log) (*ContractUnstakeRequested, error) {
	event := new(ContractUnstakeRequested)
	if err := _Contract.contract.UnpackLog(event, "UnstakeRequested", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractUnstakedWithdrawnIterator is returned from FilterUnstakedWithdrawn and is used to iterate over the raw logs and unpacked data for UnstakedWithdrawn events raised by the Contract contract.
type ContractUnstakedWithdrawnIterator struct {
	Event *ContractUnstakedWithdrawn // Event containing the contract specifics and raw log

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
func (it *ContractUnstakedWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractUnstakedWithdrawn)
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
		it.Event = new(ContractUnstakedWithdrawn)
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
func (it *ContractUnstakedWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractUnstakedWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractUnstakedWithdrawn represents a UnstakedWithdrawn event raised by the Contract contract.
type ContractUnstakedWithdrawn struct {
	User   common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterUnstakedWithdrawn is a free log retrieval operation binding the contract event 0x8fe9b0a74e05eaabda056a095be40fb09405d83f5b2c07d7823d1eb655b927b8.
//
// Solidity: event UnstakedWithdrawn(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) FilterUnstakedWithdrawn(opts *bind.FilterOpts, user []common.Address) (*ContractUnstakedWithdrawnIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "UnstakedWithdrawn", userRule)
	if err != nil {
		return nil, err
	}
	return &ContractUnstakedWithdrawnIterator{contract: _Contract.contract, event: "UnstakedWithdrawn", logs: logs, sub: sub}, nil
}

// WatchUnstakedWithdrawn is a free log subscription operation binding the contract event 0x8fe9b0a74e05eaabda056a095be40fb09405d83f5b2c07d7823d1eb655b927b8.
//
// Solidity: event UnstakedWithdrawn(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) WatchUnstakedWithdrawn(opts *bind.WatchOpts, sink chan<- *ContractUnstakedWithdrawn, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "UnstakedWithdrawn", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractUnstakedWithdrawn)
				if err := _Contract.contract.UnpackLog(event, "UnstakedWithdrawn", log); err != nil {
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

// ParseUnstakedWithdrawn is a log parse operation binding the contract event 0x8fe9b0a74e05eaabda056a095be40fb09405d83f5b2c07d7823d1eb655b927b8.
//
// Solidity: event UnstakedWithdrawn(address indexed user, uint256 amount)
func (_Contract *ContractFilterer) ParseUnstakedWithdrawn(log types.Log) (*ContractUnstakedWithdrawn, error) {
	event := new(ContractUnstakedWithdrawn)
	if err := _Contract.contract.UnpackLog(event, "UnstakedWithdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the Contract contract.
type ContractUpgradedIterator struct {
	Event *ContractUpgraded // Event containing the contract specifics and raw log

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
func (it *ContractUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractUpgraded)
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
		it.Event = new(ContractUpgraded)
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
func (it *ContractUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractUpgraded represents a Upgraded event raised by the Contract contract.
type ContractUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_Contract *ContractFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*ContractUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &ContractUpgradedIterator{contract: _Contract.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_Contract *ContractFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *ContractUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractUpgraded)
				if err := _Contract.contract.UnpackLog(event, "Upgraded", log); err != nil {
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

// ParseUpgraded is a log parse operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_Contract *ContractFilterer) ParseUpgraded(log types.Log) (*ContractUpgraded, error) {
	event := new(ContractUpgraded)
	if err := _Contract.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractValidatorAddedIterator is returned from FilterValidatorAdded and is used to iterate over the raw logs and unpacked data for ValidatorAdded events raised by the Contract contract.
type ContractValidatorAddedIterator struct {
	Event *ContractValidatorAdded // Event containing the contract specifics and raw log

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
func (it *ContractValidatorAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractValidatorAdded)
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
		it.Event = new(ContractValidatorAdded)
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
func (it *ContractValidatorAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractValidatorAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractValidatorAdded represents a ValidatorAdded event raised by the Contract contract.
type ContractValidatorAdded struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Shares      []byte
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorAdded is a free log retrieval operation binding the contract event 0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5.
//
// Solidity: event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterValidatorAdded(opts *bind.FilterOpts, owner []common.Address) (*ContractValidatorAddedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ValidatorAdded", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractValidatorAddedIterator{contract: _Contract.contract, event: "ValidatorAdded", logs: logs, sub: sub}, nil
}

// WatchValidatorAdded is a free log subscription operation binding the contract event 0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5.
//
// Solidity: event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchValidatorAdded(opts *bind.WatchOpts, sink chan<- *ContractValidatorAdded, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ValidatorAdded", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractValidatorAdded)
				if err := _Contract.contract.UnpackLog(event, "ValidatorAdded", log); err != nil {
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
func (_Contract *ContractFilterer) ParseValidatorAdded(log types.Log) (*ContractValidatorAdded, error) {
	event := new(ContractValidatorAdded)
	if err := _Contract.contract.UnpackLog(event, "ValidatorAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractValidatorExitedIterator is returned from FilterValidatorExited and is used to iterate over the raw logs and unpacked data for ValidatorExited events raised by the Contract contract.
type ContractValidatorExitedIterator struct {
	Event *ContractValidatorExited // Event containing the contract specifics and raw log

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
func (it *ContractValidatorExitedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractValidatorExited)
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
		it.Event = new(ContractValidatorExited)
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
func (it *ContractValidatorExitedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractValidatorExitedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractValidatorExited represents a ValidatorExited event raised by the Contract contract.
type ContractValidatorExited struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorExited is a free log retrieval operation binding the contract event 0xb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c.
//
// Solidity: event ValidatorExited(address indexed owner, uint64[] operatorIds, bytes publicKey)
func (_Contract *ContractFilterer) FilterValidatorExited(opts *bind.FilterOpts, owner []common.Address) (*ContractValidatorExitedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ValidatorExited", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractValidatorExitedIterator{contract: _Contract.contract, event: "ValidatorExited", logs: logs, sub: sub}, nil
}

// WatchValidatorExited is a free log subscription operation binding the contract event 0xb4b20ffb2eb1f020be3df600b2287914f50c07003526d3a9d89a9dd12351828c.
//
// Solidity: event ValidatorExited(address indexed owner, uint64[] operatorIds, bytes publicKey)
func (_Contract *ContractFilterer) WatchValidatorExited(opts *bind.WatchOpts, sink chan<- *ContractValidatorExited, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ValidatorExited", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractValidatorExited)
				if err := _Contract.contract.UnpackLog(event, "ValidatorExited", log); err != nil {
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
func (_Contract *ContractFilterer) ParseValidatorExited(log types.Log) (*ContractValidatorExited, error) {
	event := new(ContractValidatorExited)
	if err := _Contract.contract.UnpackLog(event, "ValidatorExited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractValidatorRemovedIterator is returned from FilterValidatorRemoved and is used to iterate over the raw logs and unpacked data for ValidatorRemoved events raised by the Contract contract.
type ContractValidatorRemovedIterator struct {
	Event *ContractValidatorRemoved // Event containing the contract specifics and raw log

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
func (it *ContractValidatorRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractValidatorRemoved)
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
		it.Event = new(ContractValidatorRemoved)
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
func (it *ContractValidatorRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractValidatorRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractValidatorRemoved represents a ValidatorRemoved event raised by the Contract contract.
type ContractValidatorRemoved struct {
	Owner       common.Address
	OperatorIds []uint64
	PublicKey   []byte
	Cluster     ISSVNetworkCoreCluster
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterValidatorRemoved is a free log retrieval operation binding the contract event 0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e.
//
// Solidity: event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) FilterValidatorRemoved(opts *bind.FilterOpts, owner []common.Address) (*ContractValidatorRemovedIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "ValidatorRemoved", ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractValidatorRemovedIterator{contract: _Contract.contract, event: "ValidatorRemoved", logs: logs, sub: sub}, nil
}

// WatchValidatorRemoved is a free log subscription operation binding the contract event 0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e.
//
// Solidity: event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, (uint32,uint64,uint64,bool,uint256) cluster)
func (_Contract *ContractFilterer) WatchValidatorRemoved(opts *bind.WatchOpts, sink chan<- *ContractValidatorRemoved, owner []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "ValidatorRemoved", ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractValidatorRemoved)
				if err := _Contract.contract.UnpackLog(event, "ValidatorRemoved", log); err != nil {
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
func (_Contract *ContractFilterer) ParseValidatorRemoved(log types.Log) (*ContractValidatorRemoved, error) {
	event := new(ContractValidatorRemoved)
	if err := _Contract.contract.UnpackLog(event, "ValidatorRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ContractWeightedRootProposedIterator is returned from FilterWeightedRootProposed and is used to iterate over the raw logs and unpacked data for WeightedRootProposed events raised by the Contract contract.
type ContractWeightedRootProposedIterator struct {
	Event *ContractWeightedRootProposed // Event containing the contract specifics and raw log

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
func (it *ContractWeightedRootProposedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractWeightedRootProposed)
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
		it.Event = new(ContractWeightedRootProposed)
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
func (it *ContractWeightedRootProposedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractWeightedRootProposedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractWeightedRootProposed represents a WeightedRootProposed event raised by the Contract contract.
type ContractWeightedRootProposed struct {
	MerkleRoot        [32]byte
	BlockNum          uint64
	AccumulatedWeight *big.Int
	Quorum            *big.Int
	OracleId          uint32
	Oracle            common.Address
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterWeightedRootProposed is a free log retrieval operation binding the contract event 0xc0516bad4be527b56ad33b7087029ffee230e390e2f2d1076597820c9e170b67.
//
// Solidity: event WeightedRootProposed(bytes32 indexed merkleRoot, uint64 indexed blockNum, uint256 accumulatedWeight, uint256 quorum, uint32 oracleId, address oracle)
func (_Contract *ContractFilterer) FilterWeightedRootProposed(opts *bind.FilterOpts, merkleRoot [][32]byte, blockNum []uint64) (*ContractWeightedRootProposedIterator, error) {

	var merkleRootRule []interface{}
	for _, merkleRootItem := range merkleRoot {
		merkleRootRule = append(merkleRootRule, merkleRootItem)
	}
	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.FilterLogs(opts, "WeightedRootProposed", merkleRootRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return &ContractWeightedRootProposedIterator{contract: _Contract.contract, event: "WeightedRootProposed", logs: logs, sub: sub}, nil
}

// WatchWeightedRootProposed is a free log subscription operation binding the contract event 0xc0516bad4be527b56ad33b7087029ffee230e390e2f2d1076597820c9e170b67.
//
// Solidity: event WeightedRootProposed(bytes32 indexed merkleRoot, uint64 indexed blockNum, uint256 accumulatedWeight, uint256 quorum, uint32 oracleId, address oracle)
func (_Contract *ContractFilterer) WatchWeightedRootProposed(opts *bind.WatchOpts, sink chan<- *ContractWeightedRootProposed, merkleRoot [][32]byte, blockNum []uint64) (event.Subscription, error) {

	var merkleRootRule []interface{}
	for _, merkleRootItem := range merkleRoot {
		merkleRootRule = append(merkleRootRule, merkleRootItem)
	}
	var blockNumRule []interface{}
	for _, blockNumItem := range blockNum {
		blockNumRule = append(blockNumRule, blockNumItem)
	}

	logs, sub, err := _Contract.contract.WatchLogs(opts, "WeightedRootProposed", merkleRootRule, blockNumRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractWeightedRootProposed)
				if err := _Contract.contract.UnpackLog(event, "WeightedRootProposed", log); err != nil {
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

// ParseWeightedRootProposed is a log parse operation binding the contract event 0xc0516bad4be527b56ad33b7087029ffee230e390e2f2d1076597820c9e170b67.
//
// Solidity: event WeightedRootProposed(bytes32 indexed merkleRoot, uint64 indexed blockNum, uint256 accumulatedWeight, uint256 quorum, uint32 oracleId, address oracle)
func (_Contract *ContractFilterer) ParseWeightedRootProposed(log types.Log) (*ContractWeightedRootProposed, error) {
	event := new(ContractWeightedRootProposed)
	if err := _Contract.contract.UnpackLog(event, "WeightedRootProposed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
