package ekm

import (
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	highestAttPrefix      = prefix + "highest_att-"
	highestProposalPrefix = prefix + "highest_prop-"

	// GenesisVersionPrefix is the version prefix key of slashing protection DB
	GenesisVersionPrefix = "genesis_version"
	// GenesisVersion is the genesis version of the slashing protection DB
	GenesisVersion = "0x0"
)

type spStorage struct {
	db     basedb.Database
	logger *zap.Logger // struct logger is used because core.Storage does not support passing a logger
	lock   sync.RWMutex

	prefix []byte
}

func newSlashingProtectionStorage(db basedb.Database, logger *zap.Logger, prefix []byte) spStorage {
	return spStorage{
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
		return errors.New("pubKey must not be nil")
	}

	if attestation == nil {
		return errors.New("attestation data could not be nil")
	}

	data, err := attestation.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	return s.db.Set(s.objPrefix(highestAttPrefix), pubKey, data)
}

func (s *spStorage) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return nil, false, errors.New("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestAttPrefix), pubKey)
	if err != nil {
		return nil, found, errors.Wrap(err, "could not get highest attestation from db")
	}
	if !found {
		return nil, false, nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil, found, errors.Wrap(err, "highest attestation value is empty")
	}

	// decode
	ret := &phase0.AttestationData{}
	if err := ret.UnmarshalSSZ(obj.Value); err != nil {
		return nil, found, errors.Wrap(err, "could not unmarshal attestation data")
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
		return errors.New("pubKey must not be nil")
	}

	if slot == 0 {
		return errors.New("invalid proposal slot, slot could not be 0")
	}

	var data []byte
	data = ssz.MarshalUint64(data, uint64(slot))

	return s.db.Set(s.objPrefix(highestProposalPrefix), pubKey, data)
}

func (s *spStorage) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return 0, false, errors.New("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestProposalPrefix), pubKey)
	if err != nil {
		return 0, found, errors.Wrap(err, "could not get highest proposal from db")
	}
	if !found {
		return 0, found, nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return 0, found, errors.Wrap(err, "highest proposal value is empty")
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
