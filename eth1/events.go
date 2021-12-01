package eth1

import (
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

var (
	//contractABI = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"bytes","name":"validatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"index","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"name":"OessAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"components":[{"internalType":"uint256","name":"index","type":"uint256"},{"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"indexed":false,"internalType":"struct ISSVNetwork.Oess[]","name":"oessList","type":"tuple[]"}],"name":"ValidatorAdded","type":"event"},{"inputs":[{"internalType":"string","name":"_name","type":"string"},{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"}],"name":"addOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"},{"internalType":"bytes[]","name":"_operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_encryptedKeys","type":"bytes[]"}],"name":"addValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"operatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"operators","outputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"address","name":"ownerAddress","type":"address"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"score","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"validatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
	contractABI = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"oldFee","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"newFee","type":"uint256"}],"name":"NetworkFeeUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorActivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorDeleted","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"fee","type":"uint256"}],"name":"OperatorFeeUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorInactivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"blockNumber","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"score","type":"uint256"}],"name":"OperatorScoreUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"previousOwner","type":"address"},{"indexed":true,"internalType":"address","name":"newOwner","type":"address"}],"name":"OwnershipTransferred","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"ValidatorActivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"bytes[]","name":"operatorPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"}],"name":"ValidatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"ValidatorDeleted","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"ValidatorInactivated","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"indexed":false,"internalType":"bytes[]","name":"operatorPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"indexed":false,"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"}],"name":"ValidatorUpdated","type":"event"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"activateOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"activateValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"addressNetworkFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"burnRate","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"deactivateOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"deactivateValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"deleteOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"deleteValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"deposit","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getNetworkTreasury","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"operatorPublicKey","type":"bytes"}],"name":"getOperatorCurrentFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"getOperatorsByOwnerAddress","outputs":[{"internalType":"bytes[]","name":"","type":"bytes[]"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"getOperatorsByValidator","outputs":[{"internalType":"bytes[]","name":"","type":"bytes[]"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"getValidatorsByOwnerAddress","outputs":[{"internalType":"bytes[]","name":"","type":"bytes[]"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"contract ISSVRegistry","name":"registryAddress","type":"address"},{"internalType":"contract IERC20","name":"token","type":"address"},{"internalType":"uint256","name":"minimumBlocksBeforeLiquidation","type":"uint256"},{"internalType":"uint256","name":"operatorMaxFeeIncrease","type":"uint256"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"liquidatable","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"liquidate","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address[]","name":"ownerAddresses","type":"address[]"}],"name":"liquidateAll","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"minimumBlocksBeforeLiquidation","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"networkFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"operatorEarningsOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"operatorMaxFeeIncrease","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"operators","outputs":[{"internalType":"string","name":"","type":"string"},{"internalType":"address","name":"","type":"address"},{"internalType":"bytes","name":"","type":"bytes"},{"internalType":"uint256","name":"","type":"uint256"},{"internalType":"bool","name":"","type":"bool"},{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"owner","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"registerOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"bytes[]","name":"operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"registerValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"renounceOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"test_operatorIndexOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"totalBalanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"totalEarningsOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"newOwner","type":"address"}],"name":"transferOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"minimumBlocksBeforeLiquidation","type":"uint256"}],"name":"updateMinimumBlocksBeforeLiquidation","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"updateNetworkFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"fee","type":"uint256"}],"name":"updateOperatorFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"operatorMaxFeeIncrease","type":"uint256"}],"name":"updateOperatorMaxFeeIncrease","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"score","type":"uint256"}],"name":"updateOperatorScore","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"bytes[]","name":"operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"encryptedKeys","type":"bytes[]"},{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"updateValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenAmount","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdrawNetworkFees","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
)

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
func ContractABI() string {
	return contractABI
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey          []byte
	OwnerAddress       common.Address
	OperatorPublicKeys [][]byte
	SharesPublicKeys   [][]byte
	EncryptedKeys      [][]byte
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Name         string
	PublicKey    []byte
	OwnerAddress common.Address
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to unpack OperatorAdded event")
	}

	hexPubkey := hex.EncodeToString(operatorAddedEvent.PublicKey)
	logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", hexPubkey),
		zap.String("Payment Address", operatorAddedEvent.OwnerAddress.String()))
	var nodeOperatorPubKey string
	if operatorPrivateKey != nil {
		nodeOperatorPubKey, err = rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
	}
	isEventBelongsToOperator := strings.EqualFold(hexPubkey, nodeOperatorPubKey)
	return &operatorAddedEvent, isEventBelongsToOperator, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, bool, error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, "ValidatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack ValidatorAdded event")
	}

	logger.Debug("ValidatorAdded Event",
		zap.String("Validator PublicKey", hex.EncodeToString(validatorAddedEvent.PublicKey)),
		zap.String("Owner Address", validatorAddedEvent.OwnerAddress.String()))

	var isEventBelongsToOperator bool

	for i, operatorPublicKey := range validatorAddedEvent.OperatorPublicKeys {
		outAbi, err := getOutAbi()
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}
		hexOperatorPublicKey, err := readOperatorPubKey(operatorPublicKey, outAbi)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		operatorPublicKey = []byte(hexOperatorPublicKey) // set for further use in code
		if operatorPrivateKey == nil {
			continue
		}
		nodeOperatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
		if strings.EqualFold(hexOperatorPublicKey, nodeOperatorPubKey) {
			out, err := outAbi.Unpack("method", validatorAddedEvent.EncryptedKeys[i])
			if err != nil {
				return nil, false, errors.Wrap(err, "failed to unpack EncryptedKey")
			}

			if encryptedSharePrivateKey, ok := out[0].(string); ok {
				decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, encryptedSharePrivateKey)
				decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to decrypt share private key")
				}
				validatorAddedEvent.EncryptedKeys[i] = []byte(decryptedSharePrivateKey)
				isEventBelongsToOperator = true
			}
		}
	}

	return &validatorAddedEvent, isEventBelongsToOperator, nil
}

func readOperatorPubKey(operatorPublicKey []byte, outAbi abi.ABI) (string, error) {
	outOperatorPublicKey, err := outAbi.Unpack("method", operatorPublicKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to unpack OperatorPublicKey")
	}

	if operatorPublicKey, ok := outOperatorPublicKey[0].(string); ok {
		return operatorPublicKey, nil
	}

	return "", errors.Wrap(err, "failed to read OperatorPublicKey")
}

func getOutAbi() (abi.ABI, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "string"}]}]`
	return abi.JSON(strings.NewReader(def))
}
