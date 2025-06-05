package migrations

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sanity-io/litter"
	"go.uber.org/zap"

	opstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// This migration changes share format used for storing share in DB from gob to ssz.
// Note, in general, migration(s) must behave as no-op (not error) when there is no
// data to be targeted - so that SSV node with "fresh" DB can operate just fine.
var migration_5_change_share_format_from_gob_to_ssz = Migration{
	Name: "migration_5_change_share_format_from_gob_to_ssz",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) (err error) {
		var sharesGOBTotal int

		defer func() {
			if err != nil {
				return // cannot complete migration successfully
			}
			// complete migration, this makes sure migration applies only once
			if err = completed(opt.Db); err != nil {
				err = fmt.Errorf("complete transaction: %w", err)
				return
			}
			logger.Info("migration completed", zap.Int("gob_shares_total", sharesGOBTotal))
		}()

		// sharesSSZEncoded is a bunch of updates this migration will need to perform, we cannot do them
		// all in a single transaction (because there is a limit on how large a single transaction can be)
		// so we'll use SetMany func that will split up the data we want to update into batches committing
		// each batch in a separate transaction. I guess that makes this migration non-atomic, but since
		// this migration is idempotent atomicity isn't required (we can re-apply it however many times
		// we like without "breaking" anything)
		sharesSSZEncoded := make([]basedb.Obj, 0)

		// sharesGOB maps share ID to GOB-encoded shares we already have stored in DB
		sharesGOB := make(map[string]*storageShareGOB)
		err = opt.Db.GetAll(append(opstorage.OperatorStoragePrefix, sharesPrefixGOB...), func(i int, obj basedb.Obj) error {
			shareGOB := &storageShareGOB{}
			if err := shareGOB.Decode(obj.Value); err != nil {
				return fmt.Errorf("decode gob share: %w", err)
			}
			sID := shareID(shareGOB.ValidatorPubKey)
			if _, ok := sharesGOB[sID]; ok {
				return fmt.Errorf("have already seen GOB share with the same share ID: %s", sID)
			}
			sharesGOB[sID] = shareGOB
			share, err := storageShareGOBToDomainShare(shareGOB)
			if err != nil {
				return fmt.Errorf("convert gob storage share to domain share: %w", err)
			}
			shareSSZ := storage.FromSSVShare(share)
			key := storage.SharesDBKey(shareSSZ.ValidatorPubKey[:])
			value, err := shareSSZ.Encode()
			if err != nil {
				return fmt.Errorf("encode ssz share: %w", err)
			}
			sharesSSZEncoded = append(sharesSSZEncoded, basedb.Obj{
				Key:   key,
				Value: value,
			})
			return nil
		})
		if err != nil {
			return fmt.Errorf("GetAll: %w", err)
		}

		sharesGOBTotal = len(sharesGOB)
		if sharesGOBTotal == 0 {
			return nil // we won't be creating any SSZ shares
		}

		if err := opt.Db.SetMany(opstorage.OperatorStoragePrefix, len(sharesSSZEncoded), func(i int) (basedb.Obj, error) {
			return sharesSSZEncoded[i], nil
		}); err != nil {
			return fmt.Errorf("SetMany: %w", err)
		}

		sharesSSZTotal := 0
		if err := opt.Db.GetAll(storage.SharesDBPrefix(opstorage.OperatorStoragePrefix), func(i int, obj basedb.Obj) error {
			shareSSZ := &storage.Share{}
			err := shareSSZ.Decode(obj.Value)
			if err != nil {
				return fmt.Errorf("decode ssz share: %w", err)
			}
			sID := shareID(shareSSZ.ValidatorPubKey)
			shareGOB, ok := sharesGOB[sID]
			if !ok {
				return fmt.Errorf("SSZ share %s doesn't have corresponding GOB share", sID)
			}
			if !matchGOBvsSSZ(shareGOB, shareSSZ) {
				return fmt.Errorf(
					"GOB share doesn't match corresponding SSZ share, GOB: %s, SSZ: %s",
					litter.Sdump(shareGOB),
					litter.Sdump(shareSSZ),
				)
			}
			sharesSSZTotal++
			return nil
		}); err != nil {
			return fmt.Errorf("GetMany: %w", err)
		}

		if sharesSSZTotal != sharesGOBTotal {
			return fmt.Errorf("total SSZ shares count %d doesn't match GOB shares count %d", sharesSSZTotal, sharesGOBTotal)
		}

		if err = opt.Db.DropPrefix(append(opstorage.OperatorStoragePrefix, sharesPrefixGOB...)); err != nil {
			err = fmt.Errorf("DropPrefix (GOB shares): %w", err)
			return
		}

		return nil
	},
}

func shareID(validatorPubkey []byte) string {
	return string(validatorPubkey)
}

func matchGOBvsSSZ(shareGOB *storageShareGOB, shareSSZ *storage.Share) bool {
	// note, ssz share no longer has OperatorID field
	if !bytes.Equal(shareGOB.ValidatorPubKey, shareSSZ.ValidatorPubKey) {
		return false
	}
	if !bytes.Equal(shareGOB.SharePubKey, shareSSZ.SharePubKey) {
		return false
	}
	if len(shareGOB.Committee) != len(shareSSZ.Committee) {
		return false
	}
	for i, committeeGOB := range shareGOB.Committee {
		committeeSSZ := shareSSZ.Committee[i]
		if committeeGOB.OperatorID != committeeSSZ.OperatorID {
			return false
		}
		if !bytes.Equal(committeeGOB.PubKey, committeeSSZ.PubKey) {
			return false
		}
	}
	if shareGOB.DomainType != shareSSZ.DomainType {
		return false
	}
	if shareGOB.FeeRecipientAddress != shareSSZ.FeeRecipientAddress {
		return false
	}
	if !bytes.Equal(shareGOB.Graffiti, shareSSZ.Graffiti) {
		return false
	}

	if shareGOB.OwnerAddress != shareSSZ.OwnerAddress {
		return false
	}
	if shareGOB.Liquidated != shareSSZ.Liquidated {
		return false
	}

	// finally, check Beacon metadata matches
	if shareGOB.BeaconMetadata == nil {
		if shareSSZ.ValidatorIndex != 0 {
			return false
		}
		if shareSSZ.Status != 0 {
			return false
		}
		if shareSSZ.ActivationEpoch != 0 {
			return false
		}
		// note, ssz share no longer has Balance field
		return true
	}
	if uint64(shareGOB.BeaconMetadata.Index) != shareSSZ.ValidatorIndex {
		return false
	}
	// #nosec G115 - never above max int
	if int(shareGOB.BeaconMetadata.Status) != int(shareSSZ.Status) {
		return false
	}
	if uint64(shareGOB.BeaconMetadata.ActivationEpoch) != shareSSZ.ActivationEpoch {
		return false
	}

	return true
}
