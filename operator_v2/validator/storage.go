package validator

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
	"github.com/bloxapp/ssv/protocol/v2/share"
	"github.com/bloxapp/ssv/storage/basedb"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(share *share.Share) error
	SaveValidatorMetadata(metadata *share.Metadata) error
	GetValidatorShare(key []byte) (*share.Share, bool, error)
	GetValidatorMetadata(key []byte) (*share.Metadata, bool, error)
	GetAllValidatorShares() ([]*share.Share, error)
	GetAllValidatorMetadata() ([]*share.Metadata, error)
	GetOperatorValidatorShares(operatorPubKey string, enabled bool) ([]*share.Share, []*share.Metadata, error)
	GetOperatorIDValidatorShares(operatorID uint32, enabled bool) ([]*share.Share, []*share.Metadata, error)
	GetValidatorMetadataByOwnerAddress(ownerAddress string) ([]*share.Share, []*share.Metadata, error)
	DeleteValidatorShare(key []byte) error
}

func sharePrefix() []byte {
	return []byte("share-")
}

func metadataPrefix() []byte {
	return []byte("metadata-")
}

// CollectionOptions struct
type CollectionOptions struct {
	DB     basedb.IDb
	Logger *zap.Logger
}

// Collection struct
type Collection struct {
	db     basedb.IDb
	logger *zap.Logger
	lock   sync.RWMutex
}

// NewCollection creates new share storage
func NewCollection(options CollectionOptions) ICollection {
	collection := Collection{
		db:     options.DB,
		logger: options.Logger,
		lock:   sync.RWMutex{},
	}
	return &collection
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(share *share.Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveShareUnsafe(share)
	if err != nil {
		return err
	}
	return nil
}

// SaveValidatorMetadata save validator metadata to db
func (s *Collection) SaveValidatorMetadata(metadata *share.Metadata) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveMetadataUnsafe(metadata)
	if err != nil {
		return err
	}
	return nil
}

func (s *Collection) saveShareUnsafe(share *share.Share) error {
	value, err := share.Serialize()
	if err != nil {
		s.logger.Error("failed to serialize share", zap.Error(err))
		return err
	}
	return s.db.Set(sharePrefix(), share.PublicKey.Serialize(), value)
}

func (s *Collection) saveMetadataUnsafe(metadata *share.Metadata) error {
	value, err := metadata.Serialize()
	if err != nil {
		s.logger.Error("failed to serialize metadata", zap.Error(err))
		return err
	}
	return s.db.Set(metadataPrefix(), metadata.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*share.Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getShareUnsafe(key)
}

// GetValidatorMetadata by key
func (s *Collection) GetValidatorMetadata(key []byte) (*share.Metadata, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getMetadataUnsafe(key)
}

func (s *Collection) getShareUnsafe(key []byte) (*share.Share, bool, error) {
	obj, found, err := s.db.Get(sharePrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	share, err := (&share.Share{}).Deserialize(obj.Key, obj.Value)
	return share, found, err
}

// GetValidatorShare by key
func (s *Collection) getMetadataUnsafe(key []byte) (*share.Metadata, bool, error) {
	obj, found, err := s.db.Get(metadataPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	metadata, err := (&share.Metadata{}).Deserialize(obj.Key, obj.Value)
	return metadata, found, err
}

// CleanRegistryData clears all registry data
func (s *Collection) CleanRegistryData() error {
	return s.cleanAllShares()
}

func (s *Collection) cleanAllShares() error {
	return s.db.RemoveAllByCollection(sharePrefix())
}

// GetAllValidatorShares returns all shares
func (s *Collection) GetAllValidatorShares() ([]*share.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*share.Share

	err := s.db.GetAll(sharePrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&share.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetAllValidatorMetadata returns all metadata
func (s *Collection) GetAllValidatorMetadata() ([]*share.Metadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*share.Metadata

	err := s.db.GetAll(metadataPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&share.Metadata{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetOperatorValidatorShares returns all not liquidated validator shares belongs to operator
// TODO: check regards returning a slice of public keys instead of share objects
func (s *Collection) GetOperatorValidatorShares(operatorPubKey string, enabled bool) ([]*share.Share, []*share.Metadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*share.Share
	var metadataList []*share.Metadata

	err := s.db.GetAll(metadataPrefix(), func(i int, metadataObj basedb.Obj) error {
		metadataVal, err := (&share.Metadata{}).Deserialize(metadataObj.Key, metadataObj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		if !metadataVal.Liquidated || !enabled {
			if ok := metadataVal.BelongsToOperator(operatorPubKey); ok {
				metadataList = append(metadataList, metadataVal)

				shareObj, found, err := s.db.Get(sharePrefix(), metadataVal.PublicKey.Serialize())
				if err != nil {
					return err
				}
				if !found {
					shareList = append(shareList, nil)
					return nil
				}
				shareVal, err := (&share.Share{}).Deserialize(shareObj.Key, shareObj.Value)
				if err != nil {
					return fmt.Errorf("failed to deserialize metadata: %w", err)
				}
				shareList = append(shareList, shareVal)
			}
		}
		return nil
	})

	return shareList, metadataList, err
}

// GetOperatorIDValidatorShares returns all not liquidated validator shares belongs to operator ID.
// TODO: check regards returning a slice of public keys instead of share objects
func (s *Collection) GetOperatorIDValidatorShares(operatorID uint32, enabled bool) ([]*share.Share, []*share.Metadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*share.Share
	var metadataList []*share.Metadata

	err := s.db.GetAll(sharePrefix(), func(i int, metadataObj basedb.Obj) error {
		metadataVal, err := (&share.Metadata{}).Deserialize(metadataObj.Key, metadataObj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		if !metadataVal.Liquidated || !enabled {
			if ok := metadataVal.BelongsToOperatorID(uint64(operatorID)); ok {
				metadataList = append(metadataList, metadataVal)

				shareObj, found, err := s.db.Get(sharePrefix(), metadataVal.PublicKey.Serialize())
				if err != nil {
					return err
				}
				if !found {
					shareList = append(shareList, nil)
					return nil
				}
				shareVal, err := (&share.Share{}).Deserialize(shareObj.Key, shareObj.Value)
				if err != nil {
					return fmt.Errorf("failed to deserialize metadata: %w", err)
				}
				shareList = append(shareList, shareVal)
			}
		}
		return nil
	})

	return shareList, metadataList, err
}

// GetValidatorMetadataByOwnerAddress returns all validator metadata that belongs to owner address
func (s *Collection) GetValidatorMetadataByOwnerAddress(ownerAddress string) ([]*share.Share, []*share.Metadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*share.Share
	var metadataList []*share.Metadata

	err := s.db.GetAll(metadataPrefix(), func(i int, obj basedb.Obj) error {
		metadataVal, err := (&share.Metadata{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		if strings.EqualFold(metadataVal.OwnerAddress, ownerAddress) {
			metadataList = append(metadataList, metadataVal)

			shareObj, found, err := s.db.Get(sharePrefix(), metadataVal.PublicKey.Serialize())
			if err != nil {
				return err
			}
			if !found {
				shareList = append(shareList, nil)
				return nil
			}
			shareVal, err := (&share.Share{}).Deserialize(shareObj.Key, shareObj.Value)
			if err != nil {
				return fmt.Errorf("failed to deserialize metadata: %w", err)
			}
			shareList = append(shareList, shareVal)
		}
		return nil
	})

	return shareList, metadataList, err
}

// DeleteValidatorShare removes validator share by key
func (s *Collection) DeleteValidatorShare(key []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.Delete(sharePrefix(), key); err != nil {
		return fmt.Errorf("delete share: %w", err)
	}
	if err := s.db.Delete(metadataPrefix(), key); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	return nil
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *Collection) UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key, err := hex.DecodeString(pk)
	if err != nil {
		return err
	}
	m, found, err := s.getMetadataUnsafe(key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	m.Stats = metadata
	return s.saveMetadataUnsafe(m)
}
