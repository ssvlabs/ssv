package migrations

import (
	"context"
	"fmt"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	opstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const migration_6_Name = "migration_6_share_exit_epoch"

var migration_6_share_exit_epoch = Migration{
	Name: migration_6_Name,
	Run: func(ctx context.Context, l *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		var (
			oldSharesFullPrefix = append(opstorage.OperatorStoragePrefix, oldSharesPrefix...)
			oldShares           = make(map[string]*migration_6_OldStorageShare)
			sharesToPersist     []basedb.Obj
		)

		logger := l.With(zap.String("migration_name", migration_6_Name))

		logger.Debug("fetching and converting all shares that need to be migrated")

		err := opt.Db.GetAll(oldSharesFullPrefix, func(i int, obj basedb.Obj) error {
			oldShare := &migration_6_OldStorageShare{}
			if err := oldShare.Decode(obj.Value); err != nil {
				return fmt.Errorf("error: '%w' decoding share: %v", err, obj.Value)
			}

			id := string(oldShare.ValidatorPubKey)
			if _, ok := oldShares[id]; ok {
				return fmt.Errorf("have already seen share with the same share ID: %s", id)
			}

			oldShares[id] = oldShare
			share := mapShare(oldShare)

			key := storage.SharesDBKey(share.ValidatorPubKey[:])
			value, err := share.Encode()
			if err != nil {
				return fmt.Errorf("error: '%w' encoding new share: %v", err, share)
			}

			sharesToPersist = append(sharesToPersist, basedb.Obj{
				Key:   key,
				Value: value,
			})

			return nil
		})
		if err != nil {
			return fmt.Errorf("error during 'GetAll' operation, err: '%w'", err)
		}

		logger.
			With(zap.Int("shares_len", len(sharesToPersist))).
			Debug("shares fetched. Persisting new shares")

		err = opt.Db.SetMany(opstorage.OperatorStoragePrefix, len(sharesToPersist), func(i int) (basedb.Obj, error) {
			return sharesToPersist[i], nil
		})
		if err != nil {
			return fmt.Errorf("error during 'SetMany' operation, err: '%w'", err)
		}

		logger.
			With(zap.String("old_prefix", string(oldSharesFullPrefix))).
			Debug("shares were successfully persisted. Dropping old prefix")

		if err = opt.Db.DropPrefix(oldSharesFullPrefix); err != nil {
			return fmt.Errorf("error during DropPrefix operation: %w", err)
		}

		logger.Debug("prefix dropped. Executing completion function")

		return completed(opt.Db)
	},
}

func mapShare(share *migration_6_OldStorageShare) *storage.Share {
	committee := make([]*spectypes.ShareMember, len(share.Committee))
	for i, c := range share.Committee {
		committee[i] = &spectypes.ShareMember{
			Signer:      c.OperatorID,
			SharePubKey: c.PubKey,
		}
	}

	var validatorPubKey spectypes.ValidatorPK
	copy(validatorPubKey[:], share.ValidatorPubKey)

	domainShare := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey:     validatorPubKey,
			SharePubKey:         share.SharePubKey,
			Committee:           committee,
			DomainType:          share.DomainType,
			FeeRecipientAddress: share.FeeRecipientAddress,
			Graffiti:            share.Graffiti,
			ValidatorIndex:      phase0.ValidatorIndex(share.ValidatorIndex),
		},
		OwnerAddress:    share.OwnerAddress,
		Liquidated:      share.Liquidated,
		Status:          v1.ValidatorState(share.Status), // nolint: gosec //G115: integer overflow conversion uint64 -> int
		ActivationEpoch: phase0.Epoch(share.ActivationEpoch),
	}

	/**
		We cannot directly map migration_6_OldStorageShare to storage.Share because storage.Share
		contains unexported types ('storageOperator'). Instead, we map to SSVShare first
		and use storage.FromSSVShare() which has access to set those unexported types.
	**/
	return storage.FromSSVShare(domainShare)
}
