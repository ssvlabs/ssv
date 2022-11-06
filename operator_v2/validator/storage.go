package validator

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(share *spectypes.Share) error
	SaveValidatorMetadata(metadata *types.ShareMetadata) error
	GetValidatorShare(key []byte) (*spectypes.Share, bool, error)
	GetValidatorMetadata(key []byte) (*types.ShareMetadata, bool, error)
	GetAllValidatorShares() ([]*spectypes.Share, error)
	GetAllValidatorMetadata() ([]*types.ShareMetadata, error)
	GetOperatorValidatorShares(operatorPubKey string, enabled bool) ([]*spectypes.Share, []*types.ShareMetadata, error)
	GetOperatorIDValidatorShares(operatorID uint32, enabled bool) ([]*spectypes.Share, []*types.ShareMetadata, error)
	GetValidatorMetadataByOwnerAddress(ownerAddress string) ([]*spectypes.Share, []*types.ShareMetadata, error)
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
func (s *Collection) SaveValidatorShare(share *spectypes.Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveShareUnsafe(share)
	if err != nil {
		return err
	}
	return nil
}

// SaveValidatorMetadata save validator metadata to db
func (s *Collection) SaveValidatorMetadata(metadata *types.ShareMetadata) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveMetadataUnsafe(metadata)
	if err != nil {
		return err
	}
	return nil
}

func (s *Collection) saveShareUnsafe(share *spectypes.Share) error {
	value, err := share.Encode()
	if err != nil {
		s.logger.Error("failed to serialize share", zap.Error(err))
		return err
	}
	return s.db.Set(sharePrefix(), share.ValidatorPubKey, value)
}

func (s *Collection) saveMetadataUnsafe(metadata *types.ShareMetadata) error {
	value, err := metadata.Serialize()
	if err != nil {
		s.logger.Error("failed to serialize metadata", zap.Error(err))
		return err
	}
	return s.db.Set(metadataPrefix(), metadata.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*spectypes.Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getShareUnsafe(key)
}

// GetValidatorMetadata by key
func (s *Collection) GetValidatorMetadata(key []byte) (*types.ShareMetadata, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getMetadataUnsafe(key)
}

func (s *Collection) getShareUnsafe(key []byte) (*spectypes.Share, bool, error) {
	obj, found, err := s.db.Get(sharePrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	value := &spectypes.Share{}
	err = value.Decode(obj.Value)
	return value, found, err
}

// GetValidatorShare by key
func (s *Collection) getMetadataUnsafe(key []byte) (*types.ShareMetadata, bool, error) {
	obj, found, err := s.db.Get(metadataPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	metadata, err := (&types.ShareMetadata{}).Deserialize(obj.Key, obj.Value)
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
func (s *Collection) GetAllValidatorShares() ([]*spectypes.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*spectypes.Share

	err := s.db.GetAll(sharePrefix(), func(i int, obj basedb.Obj) error {
		val := &spectypes.Share{}
		if err := val.Decode(obj.Value); err != nil {
			return errors.Wrap(err, "failed to deserialize share")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetAllValidatorMetadata returns all metadata
func (s *Collection) GetAllValidatorMetadata() ([]*types.ShareMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.ShareMetadata

	err := s.db.GetAll(metadataPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&types.ShareMetadata{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize metadata")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetOperatorValidatorShares returns all not liquidated validator shares belongs to operator
// TODO: check regards returning a slice of public keys instead of share objects
func (s *Collection) GetOperatorValidatorShares(operatorPubKey string, enabled bool) ([]*spectypes.Share, []*types.ShareMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*spectypes.Share
	var metadataList []*types.ShareMetadata

	err := s.db.GetAll(metadataPrefix(), func(i int, metadataObj basedb.Obj) error {
		metadataVal, err := (&types.ShareMetadata{}).Deserialize(metadataObj.Key, metadataObj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize metadata")
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

				shareVal := &spectypes.Share{}
				if err := shareVal.Decode(shareObj.Value); err != nil {
					return fmt.Errorf("failed to deserialize share: %w", err)
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
func (s *Collection) GetOperatorIDValidatorShares(operatorID uint32, enabled bool) ([]*spectypes.Share, []*types.ShareMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*spectypes.Share
	var metadataList []*types.ShareMetadata

	err := s.db.GetAll(sharePrefix(), func(i int, metadataObj basedb.Obj) error {
		metadataVal, err := (&types.ShareMetadata{}).Deserialize(metadataObj.Key, metadataObj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize metadata")
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

				shareVal := &spectypes.Share{}
				if err := shareVal.Decode(shareObj.Value); err != nil {
					return fmt.Errorf("failed to deserialize share: %w", err)
				}
				shareList = append(shareList, shareVal)
			}
		}
		return nil
	})

	return shareList, metadataList, err
}

// GetValidatorMetadataByOwnerAddress returns all validator metadata that belongs to owner address
func (s *Collection) GetValidatorMetadataByOwnerAddress(ownerAddress string) ([]*spectypes.Share, []*types.ShareMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var shareList []*spectypes.Share
	var metadataList []*types.ShareMetadata

	err := s.db.GetAll(metadataPrefix(), func(i int, obj basedb.Obj) error {
		metadataVal, err := (&types.ShareMetadata{}).Deserialize(obj.Key, obj.Value)
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
			shareVal := &spectypes.Share{}
			if err := shareVal.Decode(shareObj.Value); err != nil {
				return fmt.Errorf("failed to deserialize share: %w", err)
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
