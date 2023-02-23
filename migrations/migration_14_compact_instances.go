package migrations

import (
	"bytes"
	"context"
	"fmt"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var migrationCompactInstances = Migration{
	Name: "migration_14_compact_instances",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		bdb, ok := opt.Db.(*kv.BadgerDb)
		if !ok {
			opt.Logger.Error("skipping migration: database is not Badger")
			return nil
		}
		lsmBefore, vlogBefore := bdb.Badger().Size()

		// Compact each role's instances.
		var roles = []spectypes.BeaconRole{
			spectypes.BNRoleAttester,
			spectypes.BNRoleAggregator,
			spectypes.BNRoleProposer,
			spectypes.BNRoleSyncCommitteeContribution,
			spectypes.BNRoleSyncCommittee,
		}

		for _, role := range roles {
			prefix := role.String()
			logger := opt.Logger.With(zap.String("role", role.String()))

			// Collect all stored MessageIDs.
			var messageIDs []spectypes.MessageID
			err := bdb.Badger().View(func(txn *badger.Txn) error {
				opt := badger.DefaultIteratorOptions
				opt.Prefix = []byte(role.String())
				it := txn.NewIterator(opt)
				defer it.Close()
			Loop:
				for it.Rewind(); it.Valid(); it.Next() {
					item := it.Item()
					key := item.Key()

					// Skip items that has a prefix of a different role.
					for _, r := range roles {
						if r.String() != role.String() && bytes.HasPrefix(key, []byte(r.String())) {
							break Loop
						}
					}

					// Extract MessageID from key.
					var messageID spectypes.MessageID
					messageID = spectypes.MessageIDFromBytes(key[len(prefix) : len(prefix)+len(messageID)])
					if messageID.GetRoleType() != role {
						return fmt.Errorf("unexpected role type %s in key %x", messageID.GetRoleType(), key)
					}
					messageIDs = append(messageIDs, messageID)
				}
				return nil
			})
			if err != nil {
				return errors.Wrap(err, "failed collecting message IDs")
			}

			// Get & save each instance (by MessageID) to trigger on-save compaction.
			storage := opt.ibftStorage(prefix, forksprotocol.GenesisForkVersion)
			for _, messageID := range messageIDs {
				inst, err := storage.GetHighestInstance(messageID[:])
				if err != nil {
					return errors.Wrap(err, "failed to get instance")
				}
				if inst == nil {
					return fmt.Errorf("instance not found for message ID %x", messageID)
				}
				if err := storage.SaveInstance(inst); err != nil {
					return errors.Wrap(err, "failed to save instance")
				}
			}
			logger.Debug("compacted instances", zap.Int("count", len(messageIDs)))
		}

		// Run GC to reclaim unused space.
		deadline := time.Now().Add(120 * time.Second)
		runs := 0
		for time.Now().Before(deadline) {
			err := bdb.Badger().RunValueLogGC(0.1)
			if err == badger.ErrNoRewrite {
				break
			}
			if err != nil {
				return errors.Wrap(err, "failed to collect garbage")
			}
			runs++
		}
		opt.Logger.Debug("collected garbage", zap.Int("gc_runs", runs))

		// Log storage savings.
		lsmAfter, vlogAfter := bdb.Badger().Size()
		opt.Logger.Debug("storage size after compaction",
			zap.String("lsm_savings", fmt.Sprintf("%.2fMB -> %.2fMB", float64(lsmBefore)/1024/1024, float64(lsmAfter)/1024/1024)),
			zap.String("vlog_savings", fmt.Sprintf("%.2fMB -> %.2fMB", float64(vlogBefore)/1024/1024, float64(vlogAfter)/1024/1024)))

		return nil
	},
}
