package storage

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

//go:generate sszgen -path ./shares.go --objs storageShare

// sharesPrefix specifies the prefix used for storing Share(s) in DB.
// Note, previously gob-encoded Share(s) were stored with `shares` prefix, this has been
// changed in migration_5_change_share_format_from_gob_to_ssz.
var sharesPrefix = []byte("shares_ssz")

// SharesFilter is a function that filters shares.
type SharesFilter func(*types.SSVShare) bool

// SharesListFunc is a function that returns a filtered list of shares.
type SharesListFunc = func(filters ...SharesFilter) []*types.SSVShare

// Shares is the interface for managing shares.
type Shares interface {
	// Get returns the share for the given public key, or nil if not found.
	Get(txn basedb.Reader, pubKey []byte) (*types.SSVShare, bool)

	// List returns a list of shares, filtered by the given filters (if any).
	List(txn basedb.Reader, filters ...SharesFilter) []*types.SSVShare

	// Range calls the given function over each share.
	// If the function returns false, the iteration stops.
	Range(txn basedb.Reader, fn func(*types.SSVShare) bool)

	// Save saves the given shares.
	Save(txn basedb.ReadWriter, shares ...*types.SSVShare) error

	// Delete deletes the share for the given public key.
	Delete(txn basedb.ReadWriter, pubKey []byte) error

	// Drop deletes all shares.
	Drop() error

	// UpdateValidatorsMetadata updates the metadata of the given validators
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata) error
}

type sharesStorage struct {
	logger         *zap.Logger
	db             basedb.Database
	prefix         []byte
	shares         map[string]*types.SSVShare
	validatorStore *validatorStore
	// storageMtx serializes access to the database in order to avoid
	// re-creation of a deleted share during update metadata
	storageMtx sync.Mutex
	// memoryMtx allows more granular access to in-memory shares map
	// to minimize time spent in 'locked' mode when updating state
	memoryMtx sync.RWMutex
}

const addressLength = 20

// storageShare represents a Share stored in DB. SSZ encodings generator has some limitations
// in terms of what types it supports, hence we define a bunch of own types here to satisfy it,
// see more on this here: https://github.com/ferranbt/fastssz/issues/179#issuecomment-2454371820
type storageShare struct {
	ValidatorIndex        uint64
	ValidatorPubKey       []byte             `ssz-max:"48"`
	SharePubKey           []byte             `ssz-max:"48"`
	Committee             []*storageOperator `ssz-max:"13"`
	Quorum, PartialQuorum uint64
	DomainType            [4]byte `ssz-size:"4"`
	FeeRecipientAddress   [addressLength]byte
	Graffiti              []byte `ssz-max:"32"`

	Balance         uint64
	Status          uint64
	ActivationEpoch uint64
	OwnerAddress    [addressLength]byte
	Liquidated      bool
}

type storageOperator struct {
	OperatorID uint64
	PubKey     []byte `ssz-max:"48"`
}

// Encode encodes Share using ssz.
func (s *storageShare) Encode() ([]byte, error) {
	result, err := s.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("marshal ssz: %w", err)
	}
	return result, nil
}

// Decode decodes Share using ssz.
func (s *storageShare) Decode(data []byte) error {
	if len(data) > types.MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), types.MaxAllowedShareSize)
	}
	if err := s.UnmarshalSSZ(data); err != nil {
		return fmt.Errorf("decode storageShare: %w", err)
	}
	s.Quorum, s.PartialQuorum = types.ComputeQuorumAndPartialQuorum(uint64(len(s.Committee)))
	return nil
}

func NewSharesStorage(logger *zap.Logger, db basedb.Database, prefix []byte) (Shares, ValidatorStore, error) {
	storage := &sharesStorage{
		logger: logger,
		shares: make(map[string]*types.SSVShare),
		db:     db,
		prefix: prefix,
	}

	if err := storage.loadFromDB(); err != nil {
		return nil, nil, err
	}
	storage.validatorStore = newValidatorStore(
		func() []*types.SSVShare { return storage.List(nil) },
		func(pk []byte) (*types.SSVShare, bool) { return storage.Get(nil, pk) },
	)
	if err := storage.validatorStore.handleSharesAdded(maps.Values(storage.shares)...); err != nil {
		return nil, nil, err
	}
	return storage, storage.validatorStore, nil
}

// loadFromDB reads all shares from db.
func (s *sharesStorage) loadFromDB() error {
	// not locking since at this point nobody has the reference to this object
	return s.db.GetAll(append(s.prefix, sharesPrefix...), func(i int, obj basedb.Obj) error {
		val := &storageShare{}
		if err := val.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize share: %w", err)
		}

		share, err := s.storageShareToSpecShare(val)
		if err != nil {
			return fmt.Errorf("failed to convert storage share to spec share: %w", err)
		}

		s.shares[hex.EncodeToString(val.ValidatorPubKey[:])] = share
		return nil
	})
}

func (s *sharesStorage) Get(_ basedb.Reader, pubKey []byte) (*types.SSVShare, bool) {
	s.memoryMtx.RLock()
	defer s.memoryMtx.RUnlock()

	return s.unsafeGet(pubKey)
}

func (s *sharesStorage) unsafeGet(pubKey []byte) (*types.SSVShare, bool) {
	share, found := s.shares[hex.EncodeToString(pubKey)]
	return share, found
}

func (s *sharesStorage) List(_ basedb.Reader, filters ...SharesFilter) []*types.SSVShare {
	s.memoryMtx.RLock()
	defer s.memoryMtx.RUnlock()

	if len(filters) == 0 {
		return maps.Values(s.shares)
	}

	var shares []*types.SSVShare
Shares:
	for _, share := range s.shares {
		for _, filter := range filters {
			if !filter(share) {
				continue Shares
			}
		}
		shares = append(shares, share)
	}
	return shares
}

func (s *sharesStorage) Range(_ basedb.Reader, fn func(*types.SSVShare) bool) {
	s.memoryMtx.RLock()
	defer s.memoryMtx.RUnlock()

	for _, share := range s.shares {
		if !fn(share) {
			break
		}
	}
}

func (s *sharesStorage) Save(rw basedb.ReadWriter, shares ...*types.SSVShare) error {
	if len(shares) == 0 {
		return nil
	}

	for _, share := range shares {
		if share == nil {
			return fmt.Errorf("nil share")
		}
	}

	s.storageMtx.Lock()
	defer s.storageMtx.Unlock()

	// Update in-memory.
	err := func() error {
		s.memoryMtx.Lock()
		defer s.memoryMtx.Unlock()

		updateShares := make([]*types.SSVShare, 0, len(shares))
		addShares := make([]*types.SSVShare, 0, len(shares))

		for _, share := range shares {
			key := hex.EncodeToString(share.ValidatorPubKey[:])

			// Update validatorStore indices.
			if _, ok := s.shares[key]; ok {
				updateShares = append(updateShares, share)
			} else {
				addShares = append(addShares, share)
			}
			s.shares[key] = share
		}

		if err := s.validatorStore.handleSharesUpdated(updateShares...); err != nil {
			return fmt.Errorf("handleSharesUpdated: %w", err)
		}

		if err := s.validatorStore.handleSharesAdded(addShares...); err != nil {
			return fmt.Errorf("handleSharesAdded: %w", err)
		}

		return nil
	}()
	if err != nil {
		return err
	}

	return s.saveToDB(rw, shares...)
}

func (s *sharesStorage) saveToDB(rw basedb.ReadWriter, shares ...*types.SSVShare) error {
	return s.db.Using(rw).SetMany(s.prefix, len(shares), func(i int) (basedb.Obj, error) {
		share := specShareToStorageShare(shares[i])
		value, err := share.Encode()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("failed to serialize share: %w", err)
		}
		return basedb.Obj{Key: s.storageKey(share.ValidatorPubKey[:]), Value: value}, nil
	})
}

func specShareToStorageShare(share *types.SSVShare) *storageShare {
	committee := make([]*storageOperator, len(share.Committee))
	for i, c := range share.Committee {
		committee[i] = &storageOperator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		}
	}
	quorum, partialQuorum := types.ComputeQuorumAndPartialQuorum(uint64(len(committee)))
	return &storageShare{
		ValidatorIndex:      uint64(share.ValidatorIndex),
		ValidatorPubKey:     share.ValidatorPubKey[:],
		SharePubKey:         share.SharePubKey,
		Committee:           committee,
		Quorum:              quorum,
		PartialQuorum:       partialQuorum,
		DomainType:          share.DomainType,
		FeeRecipientAddress: share.FeeRecipientAddress,
		Graffiti:            share.Graffiti,
		OwnerAddress:        share.OwnerAddress,
		Liquidated:          share.Liquidated,
		Balance:             uint64(share.Balance),
		Status:              uint64(share.Status), // nolint: gosec
		ActivationEpoch:     uint64(share.ActivationEpoch),
	}
}

func (s *sharesStorage) storageShareToSpecShare(stShare *storageShare) (*types.SSVShare, error) {
	committee := make([]*spectypes.ShareMember, len(stShare.Committee))
	for i, c := range stShare.Committee {
		committee[i] = &spectypes.ShareMember{
			Signer:      c.OperatorID,
			SharePubKey: c.PubKey,
		}
	}

	if len(stShare.ValidatorPubKey) != phase0.PublicKeyLength {
		return nil, fmt.Errorf("invalid ValidatorPubKey length: got %v, expected 48", len(stShare.ValidatorPubKey))
	}

	var validatorPubKey spectypes.ValidatorPK
	copy(validatorPubKey[:], stShare.ValidatorPubKey)

	specShare := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(stShare.ValidatorIndex),
			ValidatorPubKey:     validatorPubKey,
			SharePubKey:         stShare.SharePubKey,
			Committee:           committee,
			DomainType:          stShare.DomainType,
			FeeRecipientAddress: stShare.FeeRecipientAddress,
			Graffiti:            stShare.Graffiti,
		},
		Balance:         phase0.Gwei(stShare.Balance),
		Status:          eth2apiv1.ValidatorState(stShare.Status), // nolint: gosec
		ActivationEpoch: phase0.Epoch(stShare.ActivationEpoch),
		OwnerAddress:    stShare.OwnerAddress,
		Liquidated:      stShare.Liquidated,
	}

	return specShare, nil
}

var errShareNotFound = errors.New("share not found")

func (s *sharesStorage) Delete(rw basedb.ReadWriter, pubKey []byte) error {
	s.storageMtx.Lock()
	defer s.storageMtx.Unlock()

	err := func() error {
		s.memoryMtx.Lock()
		defer s.memoryMtx.Unlock()

		share, found := s.shares[hex.EncodeToString(pubKey)]
		if !found {
			return errShareNotFound
		}

		// Remove the share from local storage map
		delete(s.shares, hex.EncodeToString(pubKey))

		// Remove the share from the validator store. This method will handle its own locking.
		return s.validatorStore.handleShareRemoved(share)
	}()
	if errors.Is(err, errShareNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	// Delete the share from the database
	return s.db.Using(rw).Delete(s.prefix, s.storageKey(pubKey))
}

// UpdateValidatorsMetadata updates the metadata of the given validator
func (s *sharesStorage) UpdateValidatorsMetadata(data map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata) error {
	var shares []*types.SSVShare

	s.storageMtx.Lock()
	defer s.storageMtx.Unlock()

	err := func() error {
		// using a read-lock here even if we are writing to the share pointer
		// because it's the only place a write is happening
		// to be re-implemented in a a safer maner in future iteration
		s.memoryMtx.RLock()
		defer s.memoryMtx.RUnlock()

		for pk, metadata := range data {
			if metadata == nil {
				continue
			}
			share, exists := s.unsafeGet(pk[:])
			if !exists {
				continue
			}
			share.ValidatorIndex = metadata.Index
			share.Balance = metadata.Balance
			share.Status = metadata.Status
			share.ActivationEpoch = metadata.ActivationEpoch
			shares = append(shares, share)
		}

		return s.validatorStore.handleSharesUpdated(shares...)
	}()
	if err != nil {
		return err
	}

	return s.saveToDB(nil, shares...)
}

// Drop deletes all shares.
func (s *sharesStorage) Drop() error {
	s.storageMtx.Lock()
	defer s.storageMtx.Unlock()

	func() {
		s.memoryMtx.Lock()
		defer s.memoryMtx.Unlock()

		s.shares = make(map[string]*types.SSVShare)
		s.validatorStore.handleDrop()
	}()

	return s.db.DropPrefix(bytes.Join(
		[][]byte{s.prefix, sharesPrefix, []byte("/")},
		nil,
	))
}

// storageKey builds share key using sharesPrefix & validator public key, e.g. "shares_ssz/0x00..01"
func (s *sharesStorage) storageKey(pk []byte) []byte {
	return bytes.Join([][]byte{sharesPrefix, pk}, []byte("/"))
}

// ByOperatorID filters by operator ID.
func ByOperatorID(operatorID spectypes.OperatorID) SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.BelongsToOperator(operatorID)
	}
}

// ByNotLiquidated filters for not liquidated.
func ByNotLiquidated() SharesFilter {
	return func(share *types.SSVShare) bool {
		return !share.Liquidated
	}
}

// ByActiveValidator filters for active validators.
func ByActiveValidator() SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.HasBeaconMetadata()
	}
}

// ByAttesting filters for attesting validators.
func ByAttesting(epoch phase0.Epoch) SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.IsAttesting(epoch)
	}
}

// ByClusterIDHash filters by cluster id.
func ByClusterIDHash(clusterID []byte) SharesFilter {
	return func(share *types.SSVShare) bool {
		var operatorIDs []uint64
		for _, op := range share.Committee {
			operatorIDs = append(operatorIDs, op.Signer)
		}

		shareClusterID := types.ComputeClusterIDHash(share.OwnerAddress, operatorIDs)
		return bytes.Equal(shareClusterID, clusterID)
	}
}
