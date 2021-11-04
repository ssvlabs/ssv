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
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

var (
	contractABI = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"bytes","name":"validatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"index","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"name":"OessAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"components":[{"internalType":"uint256","name":"index","type":"uint256"},{"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"indexed":false,"internalType":"struct ISSVNetwork.Oess[]","name":"oessList","type":"tuple[]"}],"name":"ValidatorAdded","type":"event"},{"inputs":[{"internalType":"string","name":"_name","type":"string"},{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"}],"name":"addOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"},{"internalType":"bytes[]","name":"_operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_encryptedKeys","type":"bytes[]"}],"name":"addValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"operatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"operators","outputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"address","name":"ownerAddress","type":"address"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"score","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"validatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
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

// Oess struct stands for operator encrypted secret share
type Oess struct {
	Index             *big.Int
	OperatorPublicKey []byte
	SharedPublicKey   []byte
	EncryptedKey      []byte
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey    []byte
	OwnerAddress common.Address
	OessList     []Oess
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Name           string
	PublicKey      []byte
	PaymentAddress common.Address
	OwnerAddress   common.Address
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to unpack OperatorAdded event")
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, false, err
	}
	pubKey, err := readOperatorPubKey(operatorAddedEvent.PublicKey, outAbi)
	if err != nil {
		return nil, false, err
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)
	logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", pubKey),
		zap.String("Payment Address", operatorAddedEvent.PaymentAddress.String()))
	var nodeOperatorPubKey string
	if operatorPrivateKey != nil {
		nodeOperatorPubKey, err = rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
	}
	isEventBelongsToOperator := strings.EqualFold(pubKey, nodeOperatorPubKey)
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

	for i := range validatorAddedEvent.OessList {
		validatorShare := &validatorAddedEvent.OessList[i]

		outAbi, err := getOutAbi()
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}
		operatorPublicKey, err := readOperatorPubKey(validatorShare.OperatorPublicKey, outAbi)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code
		if operatorPrivateKey == nil {
			continue
		}
		nodeOperatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
		if strings.EqualFold(operatorPublicKey, nodeOperatorPubKey) {
			out, err := outAbi.Unpack("method", validatorShare.EncryptedKey)
			if err != nil {
				return nil, false, errors.Wrap(err, "failed to unpack EncryptedKey")
			}

			if encryptedSharePrivateKey, ok := out[0].(string); ok {
				decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, encryptedSharePrivateKey)
				decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to decrypt share private key")
				}
				validatorShare.EncryptedKey = []byte(decryptedSharePrivateKey)
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
