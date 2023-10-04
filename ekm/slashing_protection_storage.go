package ekm

import (
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	ssz "github.com/ferranbt/fastssz"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	highestAttPrefix      = prefix + "highest_att-"
	highestProposalPrefix = prefix + "highest_prop-"
)

// SPStorage is the interface for slashing protection storage.
type SPStorage interface {
	core.SlashingStore

	RemoveHighestAttestation(pubKey []byte) error
	RemoveHighestProposal(pubKey []byte) error
}

type spStorage struct {
	db     basedb.Database
	logger *zap.Logger // struct logger is used because core.Storage does not support passing a logger
	lock   sync.RWMutex

	prefix []byte
}

func NewSlashingProtectionStorage(db basedb.Database, logger *zap.Logger, prefix []byte) SPStorage {
	return &spStorage{
		db:     db,
		logger: logger.Named(logging.NameSlashingProtectionStorage).Named(fmt.Sprintf("%sstorage", prefix)),
		prefix: prefix,
		lock:   sync.RWMutex{},
	}
}

func (s *spStorage) objPrefix(obj string) []byte {
	return append(s.prefix, []byte(obj)...)
}

func (s *spStorage) SaveHighestAttestation(pubKey []byte, attestation *phase0.AttestationData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if pubKey == nil {
		return fmt.Errorf("public key could not be nil")
	}

	if attestation == nil {
		return fmt.Errorf("attestation data could not be nil")
	}

	data, err := attestation.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("failed to marshal attestation data: %w", err)
	}

	return s.db.Set(s.objPrefix(highestAttPrefix), pubKey, data)
}

func (s *spStorage) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return nil, false, fmt.Errorf("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestAttPrefix), pubKey)
	if err != nil {
		return nil, found, fmt.Errorf("could not get highest attestation from db: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil, found, fmt.Errorf("highest attestation value is empty")
	}

	// decode
	ret := &phase0.AttestationData{}
	if err := ret.UnmarshalSSZ(obj.Value); err != nil {
		return nil, found, fmt.Errorf("could not unmarshal highest attestation data: %w", err)
	}
	return ret, found, nil
}

func (s *spStorage) RemoveHighestAttestation(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestAttPrefix), pubKey)
}

func (s *spStorage) SaveHighestProposal(pubKey []byte, slot phase0.Slot) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if pubKey == nil {
		return fmt.Errorf("public key could not be nil")
	}

	if slot == 0 {
		return fmt.Errorf("invalid proposal slot, slot could not be 0")
	}

	data := ssz.MarshalUint64(nil, uint64(slot))

	return s.db.Set(s.objPrefix(highestProposalPrefix), pubKey, data)
}

func (s *spStorage) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return 0, false, fmt.Errorf("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestProposalPrefix), pubKey)
	if err != nil {
		return 0, found, fmt.Errorf("could not get highest proposal from db: %w", err)
	}
	if !found {
		return 0, found, nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return 0, found, fmt.Errorf("highest proposal value is empty")
	}

	// decode
	slot := ssz.UnmarshallUint64(obj.Value)
	return phase0.Slot(slot), found, nil
}

func (s *spStorage) RemoveHighestProposal(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestProposalPrefix), pubKey)
}
