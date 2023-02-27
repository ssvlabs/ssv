package migrations

import (
	"context"
)

var migrationCompactInstances = Migration{
	Name: "migration_14_compact_instances",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		// Migration is disabled for now.
		return nil

		// bdb, ok := opt.Db.(*kv.BadgerDb)
		// if !ok {
		// 	opt.Logger.Error("skipping migration: database is not Badger")
		// 	return nil
		// }

		// // Compact each role's instances.
		// var roles = []spectypes.BeaconRole{
		// 	spectypes.BNRoleAttester,
		// 	spectypes.BNRoleAggregator,
		// 	spectypes.BNRoleProposer,
		// 	spectypes.BNRoleSyncCommitteeContribution,
		// 	spectypes.BNRoleSyncCommittee,
		// }

		// for _, role := range roles {
		// 	prefix := role.String()
		// 	logger := opt.Logger.With(zap.String("role", role.String()))
		// 	logger.Info("collecting instances")

		// 	// Collect all stored highest instances for this role.
		// 	var messageIDs []spectypes.MessageID
		// 	err := bdb.Badger().View(func(txn *badger.Txn) error {
		// 		opt := badger.DefaultIteratorOptions
		// 		opt.Prefix = []byte(role.String())
		// 		it := txn.NewIterator(opt)
		// 		defer it.Close()
		// 	Loop:
		// 		for it.Rewind(); it.Valid(); it.Next() {
		// 			item := it.Item()
		// 			key := item.Key()
		// 			if !bytes.HasSuffix(key, []byte("highest_instance")) {
		// 				continue
		// 			}

		// 			// Skip items that has a prefix of a different role.
		// 			for _, r := range roles {
		// 				if r.String() != role.String() && bytes.HasPrefix(key, []byte(r.String())) {
		// 					continue Loop
		// 				}
		// 			}

		// 			// Extract MessageID from key.
		// 			var messageID spectypes.MessageID
		// 			messageID = spectypes.MessageIDFromBytes(key[len(prefix) : len(prefix)+len(messageID)])
		// 			if messageID.GetRoleType() != role {
		// 				return fmt.Errorf("unexpected role type %s in key %x", messageID.GetRoleType(), key)
		// 			}
		// 			messageIDs = append(messageIDs, messageID)

		// 			if len(messageIDs)%100 == 0 {
		// 				logger.Debug("collecting instances", zap.Int("count_so_far", len(messageIDs)))
		// 			}
		// 		}
		// 		return nil
		// 	})
		// 	if err != nil {
		// 		return errors.Wrap(err, "failed collecting message IDs")
		// 	}
		// 	logger.Debug("done collecting instances", zap.Int("count", len(messageIDs)))

		// 	// Get & save each instance (by MessageID) to trigger on-save compaction.
		// 	storage := opt.ibftStorage(prefix, forksprotocol.GenesisForkVersion)
		// 	for _, messageID := range messageIDs {
		// 		inst, err := storage.GetHighestInstance(messageID[:])
		// 		if err != nil {
		// 			return errors.Wrap(err, "failed to get instance")
		// 		}
		// 		if inst == nil {
		// 			return fmt.Errorf("instance not found for message ID %x", messageID)
		// 		}
		// 		if err := storage.SaveHighestInstance(inst); err != nil {
		// 			return errors.Wrap(err, "failed to save instance")
		// 		}
		// 	}
		// 	logger.Debug("compacted instances", zap.Int("count", len(messageIDs)))
		// }

		// return nil
	},
}
