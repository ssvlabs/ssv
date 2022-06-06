package storage

import (
	"encoding/binary"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"sync"
)

type ICollection interface {
	basedb.RegistryStore
	GetDkgRequest(id uint64) (*DkgRequest, bool, error)
	SaveDkgRequest(request *DkgRequest) error
	DeleteDkgRequest(id uint64) error
	ListDkgRequests(from int64, to int64) ([]DkgRequest, error)
}

func collectionPrefix() []byte {
	return []byte("dkg-")
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

// SaveDkgRequest save validator share to db
func (s *Collection) SaveDkgRequest(request *DkgRequest) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(request)
	if err != nil {
		return err
	}
	return nil
}

func (s *Collection) CleanRegistryData() error {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.db.RemoveAllByCollection(collectionPrefix())
}

func (s *Collection) GetDkgRequest(id uint64) (*DkgRequest, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(mkKey(id))
}

func (s *Collection) DeleteDkgRequest(id uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.db.Delete(collectionPrefix(), mkKey(id))
}

func (s *Collection) ListDkgRequests(from int64, to int64) ([]DkgRequest, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	var requests []DkgRequest
	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		var req DkgRequest
		req.Deserialize(obj)
		requests = append(requests, req)
		return nil
	})

	return requests, err
}

func (s *Collection) saveUnsafe(request *DkgRequest) error {
	value, err := request.Serialize()
	if err != nil {
		s.logger.Error("failed to serialize DkgRequest", zap.Error(err))
		return err
	}
	return s.db.Set(collectionPrefix(), mkKey(request.Id), value)
}

func (s *Collection) getUnsafe(key []byte) (*DkgRequest, bool, error) {
	obj, found, err := s.db.Get(collectionPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	request, err := (&DkgRequest{}).Deserialize(obj)
	return request, found, err
}

func mkKey(id uint64) []byte{
	bytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(bytes, id)
	return bytes
}
