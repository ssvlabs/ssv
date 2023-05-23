package abiparser

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

// MalformedEventError is returned when event is malformed
type MalformedEventError struct {
	Err error
}

func (e *MalformedEventError) Error() string {
	return e.Err.Error()
}

// Event names
const (
	OperatorAdded              = "OperatorAdded"
	OperatorRemoved            = "OperatorRemoved"
	ValidatorAdded             = "ValidatorAdded"
	ValidatorRemoved           = "ValidatorRemoved"
	ClusterLiquidated          = "ClusterLiquidated"
	ClusterReactivated         = "ClusterReactivated"
	FeeRecipientAddressUpdated = "FeeRecipientAddressUpdated"
)

// b64 encrypted key length is 256
const encryptedKeyLength = 256

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	OperatorId uint64         // indexed
	Owner      common.Address // indexed
	PublicKey  []byte
	Fee        *big.Int
}

// OperatorRemovedEvent struct represents event received by the smart contract
type OperatorRemovedEvent struct {
	OperatorId uint64 // indexed
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	Owner           common.Address // indexed
	OperatorIds     []uint64
	PublicKey       []byte
	Shares          []byte
	SharePublicKeys [][]byte
	EncryptedKeys   [][]byte
	Cluster         Cluster
}

// ValidatorRemovedEvent struct represents event received by the smart contract
type ValidatorRemovedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	PublicKey   []byte
	Cluster     Cluster
}

// ClusterLiquidatedEvent struct represents event received by the smart contract
type ClusterLiquidatedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	Cluster     Cluster
}

// ClusterReactivatedEvent struct represents event received by the smart contract
type ClusterReactivatedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	Cluster     Cluster
}

// FeeRecipientAddressUpdatedEvent struct represents event received by the smart contract
type FeeRecipientAddressUpdatedEvent struct {
	Owner            common.Address // indexed
	RecipientAddress common.Address
}

type Cluster struct {
	ValidatorCount  uint32
	NetworkFeeIndex uint64
	Index           uint64
	Active          bool
	Balance         *big.Int
}

// AbiV1 parsing events from v1 abi contract
type AbiV1 struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v1 *AbiV1) ParseOperatorAddedEvent(log types.Log, contractAbi abi.ABI) (*OperatorAddedEvent, error) {
	var event OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&event, OperatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}
	pubKey, err := unpackField(event.PublicKey)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read %s event operator public key", OperatorAdded)
	}
	event.PublicKey = pubKey

	if len(log.Topics) < 3 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorAdded),
		}
	}
	event.OperatorId = log.Topics[1].Big().Uint64()
	event.Owner = common.HexToAddress(log.Topics[2].Hex())

	return &event, nil
}

// ParseOperatorRemovedEvent parses OperatorRemovedEvent
func (v1 *AbiV1) ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*OperatorRemovedEvent, error) {
	var event OperatorRemovedEvent
	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorRemoved),
		}
	}
	event.OperatorId = log.Topics[1].Big().Uint64()

	return &event, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v1 *AbiV1) ParseValidatorAddedEvent(log types.Log, contractAbi abi.ABI, nonce registrystorage.Nonce) (*ValidatorAddedEvent, error) {
	var event ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&event, ValidatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	// Calculate the expected length of constructed shares based on the number of operator IDs,
	// signature length, public key length, and encrypted key length.
	operatorCount := len(event.OperatorIds)
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	if sharesExpectedLength != len(event.Shares) {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event shares length is not correct", ValidatorAdded),
		}
	}

	event.SharePublicKeys = splitBytes(event.Shares[signatureOffset:pubKeysOffset], phase0.PublicKeyLength)
	event.EncryptedKeys = splitBytes(event.Shares[pubKeysOffset:], len(event.Shares[pubKeysOffset:])/operatorCount)

	// todo(align-contract-v0.3.1-rc.0) false positive?
	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorAdded),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	sig := event.Shares[:signatureOffset]
	err = verifySignature(&event, sig, nonce)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event signature verifcation failed", ValidatorAdded),
		}
	}

	return &event, nil
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (v1 *AbiV1) ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	var event ValidatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&event, ValidatorRemoved, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorRemoved),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseClusterLiquidatedEvent parses ClusterLiquidatedEvent
func (v1 *AbiV1) ParseClusterLiquidatedEvent(log types.Log, contractAbi abi.ABI) (*ClusterLiquidatedEvent, error) {
	var event ClusterLiquidatedEvent
	err := contractAbi.UnpackIntoInterface(&event, ClusterLiquidated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ClusterLiquidated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseClusterReactivatedEvent parses ClusterReactivatedEvent
func (v1 *AbiV1) ParseClusterReactivatedEvent(log types.Log, contractAbi abi.ABI) (*ClusterReactivatedEvent, error) {
	var event ClusterReactivatedEvent
	err := contractAbi.UnpackIntoInterface(&event, ClusterReactivated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ClusterReactivated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseFeeRecipientAddressUpdatedEvent parses FeeRecipientAddressUpdatedEvent
func (v1 *AbiV1) ParseFeeRecipientAddressUpdatedEvent(log types.Log, contractAbi abi.ABI) (*FeeRecipientAddressUpdatedEvent, error) {
	var event FeeRecipientAddressUpdatedEvent
	err := contractAbi.UnpackIntoInterface(&event, FeeRecipientAddressUpdated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", FeeRecipientAddressUpdated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

func unpackField(fieldBytes []byte) ([]byte, error) {
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	outField, err := outAbi.Unpack("method", fieldBytes)
	if err != nil {
		return nil, &MalformedEventError{
			Err: err,
		}
	}

	unpacked, ok := outField[0].([]byte)
	if !ok {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not cast OperatorPublicKey"),
		}
	}

	return unpacked, nil
}

func getOutAbi() (abi.ABI, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "bytes"}]}]`
	return abi.JSON(strings.NewReader(def))
}

func splitBytes(buf []byte, lim int) [][]byte {
	var chunk []byte
	chunks := make([][]byte, 0, len(buf)/lim+1)
	for len(buf) >= lim {
		chunk, buf = buf[:lim], buf[lim:]
		chunks = append(chunks, chunk)
	}
	if len(buf) > 0 {
		chunks = append(chunks, buf[:])
	}
	return chunks
}

// todo(align-contract-v0.3.1-rc.0): move to crypto package in ssv protocol?
// verify signature of the ValidatorAddedEvent shares data
func verifySignature(event *ValidatorAddedEvent, sig []byte, nonce registrystorage.Nonce) error {
	data := fmt.Sprintf("%s:%d", event.Owner.String(), nonce)
	hash := crypto.Keccak256([]byte(data))

	sign := &bls.Sign{}
	if err := sign.Deserialize(sig); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(event.PublicKey); err != nil {
		return errors.Wrap(err, "failed to deserialize public key")
	}

	if res := sign.VerifyByte(pk, hash); !res {
		return errors.New("failed to verify signature")
	}

	return nil
}
