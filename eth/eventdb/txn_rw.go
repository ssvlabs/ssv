package eventdb

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/common"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type RWTxn struct {
	ROTxn
}

func NewRWTxn(txn *badger.Txn) *RWTxn {
	return &RWTxn{ROTxn{txn: txn}}
}

func (t *RWTxn) SetLastProcessedBlock(block *big.Int) error {
	if err := t.txn.Set([]byte(storagePrefix+lastProcessedBlockKey), block.Bytes()); err != nil {
		return fmt.Errorf("set item: %w", err)
	}

	return nil
}

func (t *RWTxn) SaveOperatorData(operatorData *registrystorage.OperatorData) (bool, error) {
	_, err := t.txn.Get([]byte(fmt.Sprintf("%s%s/%d", storagePrefix, operatorsPrefix, operatorData.ID)))
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return false, fmt.Errorf("get item: %w", err)
	}
	if err == nil {
		return true, nil
	}

	jsonOperatorData, err := json.Marshal(operatorData)
	if err != nil {
		return false, fmt.Errorf("marshal operator data: %w", err)
	}

	if err := t.txn.Set([]byte(fmt.Sprintf("%s%s/%d", storagePrefix, operatorsPrefix, operatorData.ID)), jsonOperatorData); err != nil {
		return false, fmt.Errorf("set item: %w", err)
	}

	return false, nil
}

// SaveEventData saves event data and return it.
// if the event already exists return nil
func (t *RWTxn) SaveEventData(txHash ethcommon.Hash) error {
	_, err := t.txn.Get(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, eventsPrefix)), txHash.Bytes()...))
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return fmt.Errorf("get item: %w", err)
	}
	if err == nil {
		return nil
	}

	rawJSON, err := json.Marshal(&EventData{
		TxHash: txHash,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := t.txn.Set(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, eventsPrefix)), txHash.Bytes()...), rawJSON); err != nil {
		return fmt.Errorf("set item: %w", err)
	}

	return nil
}

func (t *RWTxn) BumpNonce(owner common.Address) error {
	recipientData, err := t.GetRecipientData(owner)
	if err != nil {
		return fmt.Errorf("get recipient data: %w", err)
	}

	if recipientData == nil {
		// Create a variable of type Nonce
		nonce := Nonce(0)

		// Create an instance of RecipientData and assign the Nonce and Owner address values
		recipientData = &RecipientData{
			Owner: owner,
			Nonce: &nonce, // Assign the address of nonceValue to Nonce field
		}
		copy(recipientData.FeeRecipient[:], owner.Bytes())
	}

	if recipientData.Nonce == nil {
		nonce := Nonce(0)
		recipientData.Nonce = &nonce
	} else if recipientData == nil {
		// Bump the nonce
		*recipientData.Nonce++
	}

	rawJSON, err := json.Marshal(recipientData)
	if err != nil {
		return errors.Wrap(err, "could not marshal recipient data")
	}

	if err := t.txn.Set(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, recipientsPrefix)), owner.Bytes()...), rawJSON); err != nil {
		return fmt.Errorf("set item: %w", err)
	}

	return nil
}

// SaveRecipientData saves recipient data and return it.
// if the recipient already exists and the fee didn't change return nil
func (t *RWTxn) SaveRecipientData(recipientData *RecipientData) (*RecipientData, error) {
	r, err := t.GetRecipientData(recipientData.Owner)
	if err != nil {
		return nil, fmt.Errorf("could not get recipient data: %w", err)
	}
	// same fee recipient
	if r != nil && r.FeeRecipient == recipientData.FeeRecipient {
		return nil, nil
	}

	rawJSON, err := json.Marshal(recipientData)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if err := t.txn.Set(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, recipientsPrefix)), recipientData.Owner.Bytes()...), rawJSON); err != nil {
		return nil, fmt.Errorf("set item: %w", err)
	}

	return recipientData, nil
}

func (t *RWTxn) SaveShares(shares ...*types.SSVShare) error {
	for _, share := range shares {
		encodedShare, err := share.Encode()
		if err != nil {
			return fmt.Errorf("encode share: %w", err)
		}
		if err := t.txn.Set(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, sharesPrefix)), share.ValidatorPubKey...), encodedShare); err != nil {
			return fmt.Errorf("set item: %w", err)
		}
	}

	return nil
}

func (t *RWTxn) DeleteShare(pubKey []byte) error {
	if err := t.txn.Delete(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, sharesPrefix)), pubKey...)); err != nil {
		return fmt.Errorf("set item: %w", err)
	}

	return nil
}
