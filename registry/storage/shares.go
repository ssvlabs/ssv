package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate go tool -modfile=../../tool.mod sszgen -path ./shares.go --objs Share

// sharesPrefix specifies the prefix used for storing Share(s) in the DB.
// During database migrations, records often need to be moved to a different prefix,
// which is why the version evolves over time.
var sharesPrefix = []byte("shares_v2/")

// pubkeyIndexMapping is a prefix for the pubkey to index mapping.
// since the churn of validators is low, we can use an append only mapping
var pubkeyIndexMapping = []byte("val_pki")

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
	UpdateValidatorsMetadata(metadataMap beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error)
}

type sharesStorage struct {
	db             basedb.Database
	storagePrefix  []byte
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

// Share represents a Share stored in DB. It's either our own share (share belongs to our
// operator) in which case it has non-empty SharePubKey, or a "generic" representation of some
// validator (who's shares are managed by other operators).
// Note, SSZ encodings generator has some limitations in terms of what types it supports, hence
// we define a bunch of own types here to satisfy it, see more on this here:
// https://github.com/ferranbt/fastssz/issues/179#issuecomment-2454371820
// Warning: SSZ encoding generator v0.1.4 has a bug related to ignoring the inline struct declarations
// (e.g. 'Name, Address []byte')
// GitHub issue: (https://github.com/ferranbt/fastssz/issues/188)
type Share struct {
	ValidatorIndex      uint64
	ValidatorPubKey     []byte             `ssz-size:"48"`
	SharePubKey         []byte             `ssz-max:"48"` // empty for not own shares
	Committee           []*storageOperator `ssz-max:"13"`
	DomainType          [4]byte            `ssz-size:"4"`
	FeeRecipientAddress [addressLength]byte
	Graffiti            []byte `ssz-max:"32"`
	Status              uint64
	ActivationEpoch     uint64
	ExitEpoch           uint64
	OwnerAddress        [addressLength]byte
	Liquidated          bool
}

type storageOperator struct {
	OperatorID uint64
	PubKey     []byte `ssz-max:"48"`
}

// Encode encodes Share using ssz.
func (s *Share) Encode() ([]byte, error) {
	result, err := s.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("marshal ssz: %w", err)
	}
	return result, nil
}

// Decode decodes Share using ssz.
func (s *Share) Decode(data []byte) error {
	if len(data) > types.MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), types.MaxAllowedShareSize)
	}
	if err := s.UnmarshalSSZ(data); err != nil {
		return fmt.Errorf("decode Share: %w", err)
	}
	return nil
}

func NewSharesStorage(beaconCfg networkconfig.Beacon, db basedb.Database, prefix []byte) (Shares, ValidatorStore, error) {
	storage := &sharesStorage{
		shares:        make(map[string]*types.SSVShare),
		db:            db,
		storagePrefix: prefix,
	}

	if err := storage.loadFromDB(); err != nil {
		return nil, nil, err
	}

	pubkeyIndexMapping, err := storage.loadPubkeyToIndexMappings()
	if err != nil {
		return nil, nil, fmt.Errorf("load validator pubkey index mapping: %w", err)
	}

	storage.validatorStore = newValidatorStore(
		func() []*types.SSVShare { return storage.List(nil) },
		func(pk []byte) (*types.SSVShare, bool) { return storage.Get(nil, pk) },
		pubkeyIndexMapping,
		beaconCfg,
	)
	if err := storage.validatorStore.handleSharesAdded(slices.Collect(maps.Values(storage.shares))...); err != nil {
		return nil, nil, err
	}
	return storage, storage.validatorStore, nil
}

func (s *sharesStorage) loadPubkeyToIndexMappings() (map[spectypes.ValidatorPK]phase0.ValidatorIndex, error) {
	m := make(map[spectypes.ValidatorPK]phase0.ValidatorIndex)

	prefix := PubkeyToIndexMappingDBKey(s.storagePrefix)

	err := s.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		var key spectypes.ValidatorPK
		if len(obj.Key) != len(key) {
			return fmt.Errorf("invalid validator PK: bad length: %d", len(obj.Key))
		}
		copy(key[:], obj.Key)
		m[key] = phase0.ValidatorIndex(binary.LittleEndian.Uint64(obj.Value))
		return nil
	})

	return m, err
}

func (s *sharesStorage) loadFromDB() error {
	return s.db.GetAll(SharesDBPrefix(s.storagePrefix), func(i int, obj basedb.Obj) error {
		val := &Share{}
		if err := val.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize share: %w", err)
		}

		share, err := ToSSVShare(val)
		if err != nil {
			return fmt.Errorf("failed to convert storage share to domain share: %w", err)
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
		return slices.Collect(maps.Values(s.shares))
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

func (s *sharesStorage) GetValidatorIndicesByPubkeys(vkeys []spectypes.ValidatorPK) (out []phase0.ValidatorIndex, err error) {
	var pubkeys = make([][]byte, 0, len(vkeys))

	for _, pk := range vkeys {
		pubkeys = append(pubkeys, pk[:])
	}

	prefix := PubkeyToIndexMappingDBKey(s.storagePrefix)

	err = s.db.GetMany(prefix, pubkeys, func(obj basedb.Obj) error {
		index := binary.LittleEndian.Uint64(obj.Value)
		out = append(out, phase0.ValidatorIndex(index))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get validator index by pubkey: %w", err)
	}

	return out, nil
}

func (s *sharesStorage) saveToDB(rw basedb.ReadWriter, shares ...*types.SSVShare) error {
	// save validator pubkey -> index mapping
	prefix := PubkeyToIndexMappingDBKey(s.storagePrefix)

	err := s.db.Using(rw).SetMany(prefix, len(shares), func(i int) (basedb.Obj, error) {
		vindex := shares[i].ValidatorIndex
		pubkey := shares[i].ValidatorPubKey

		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(vindex))

		return basedb.Obj{Key: pubkey[:], Value: b}, nil
	})
	if err != nil {
		return fmt.Errorf("save validator pubkey to index mapping: %w", err)
	}

	return s.db.Using(rw).SetMany(s.storagePrefix, len(shares), func(i int) (basedb.Obj, error) {
		share := FromSSVShare(shares[i])
		value, err := share.Encode()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("failed to serialize share: %w", err)
		}
		return basedb.Obj{Key: SharesDBKey(share.ValidatorPubKey[:]), Value: value}, nil
	})
}

func FromSSVShare(share *types.SSVShare) *Share {
	committee := make([]*storageOperator, len(share.Committee))
	for i, c := range share.Committee {
		committee[i] = &storageOperator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		}
	}

	return &Share{
		ValidatorIndex:      uint64(share.ValidatorIndex),
		ValidatorPubKey:     share.ValidatorPubKey[:],
		SharePubKey:         share.SharePubKey,
		Committee:           committee,
		DomainType:          share.DomainType,
		FeeRecipientAddress: share.FeeRecipientAddress,
		Graffiti:            share.Graffiti,
		OwnerAddress:        share.OwnerAddress,
		Liquidated:          share.Liquidated,
		Status:              uint64(share.Status), // nolint: gosec
		ActivationEpoch:     uint64(share.ActivationEpoch),
		ExitEpoch:           uint64(share.ExitEpoch),
	}
}

func ToSSVShare(stShare *Share) (*types.SSVShare, error) {
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

	domainShare := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(stShare.ValidatorIndex),
			ValidatorPubKey:     validatorPubKey,
			SharePubKey:         stShare.SharePubKey,
			Committee:           committee,
			DomainType:          stShare.DomainType,
			FeeRecipientAddress: stShare.FeeRecipientAddress,
			Graffiti:            stShare.Graffiti,
		},
		Status:          eth2apiv1.ValidatorState(stShare.Status), // nolint: gosec
		ActivationEpoch: phase0.Epoch(stShare.ActivationEpoch),
		ExitEpoch:       phase0.Epoch(stShare.ExitEpoch),
		OwnerAddress:    stShare.OwnerAddress,
		Liquidated:      stShare.Liquidated,
	}

	return domainShare, nil
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
	return s.db.Using(rw).Delete(s.storagePrefix, SharesDBKey(pubKey))
}

// UpdateValidatorsMetadata updates shares with provided validator metadata.
// It returns only metadata entries that actually changed the stored shares.
// The returned map is nil if no changes occurred.
func (s *sharesStorage) UpdateValidatorsMetadata(data beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error) {
	var (
		changedShares   []*types.SSVShare
		changedMetadata beacon.ValidatorMetadataMap
	)

	s.storageMtx.Lock()
	defer s.storageMtx.Unlock()

	err := func() error {
		// using a read-lock here even if we are writing to the share pointer
		// because it's the only place writes are happening
		// TODO: re-implemented in a safer maner in future iteration
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

			if metadata.Equals(share.BeaconMetadata()) {
				share.BeaconMetadataLastUpdated = time.Now()
				continue
			}

			// Update the share with the new metadata
			share.SetBeaconMetadata(metadata)
			changedShares = append(changedShares, share)

			if changedMetadata == nil {
				changedMetadata = make(beacon.ValidatorMetadataMap)
			}
			changedMetadata[pk] = metadata
		}

		if len(changedShares) == 0 {
			return nil
		}

		return s.validatorStore.handleSharesUpdated(changedShares...)
	}()
	if err != nil {
		return nil, err
	}

	if len(changedShares) > 0 {
		if err := s.saveToDB(nil, changedShares...); err != nil {
			return nil, err
		}
	}

	return changedMetadata, nil
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

	return s.db.DropPrefix(SharesDBPrefix(s.storagePrefix))
}

// SharesDBPrefix builds a DB prefix all share keys are stored under.
func SharesDBPrefix(storagePrefix []byte) []byte {
	return append(storagePrefix, sharesPrefix...)
}

// SharesDBKey builds share key using sharesPrefix & validator public key, e.g. "shares_ssz/0x00..01"
func SharesDBKey(pk []byte) []byte {
	return append(sharesPrefix, pk...)
}

// PubkeyToIndexMappingDBKey builds key using storage prefix followed by mapping prefix, e.g. "operator/val_pki"
func PubkeyToIndexMappingDBKey(storagePrefix []byte) []byte {
	return append(storagePrefix, pubkeyIndexMapping...)
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
