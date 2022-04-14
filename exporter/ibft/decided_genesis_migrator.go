package ibft

//
//// decidedGenesisMigrator
//type decidedGenesisMigrator struct {
//	logger *zap.Logger
//}
//
//// Migrate take care of decided messages migration
//func (m *decidedGenesisMigrator) Migrate(r Reader) error {
//	dr, ok := r.(*decidedReader)
//	if !ok {
//		return nil
//	}
//	n, err := m.migrate(dr)
//	if err != nil {
//		return errors.Wrap(err, "could not migrate decided 0")
//	}
//	m.logger.Debug("managed to migrate decided 0",
//		zap.String("identifier", string(dr.identifier)), zap.Int("items", n))
//	return nil
//}
//
//// migrate performing migration for decided messages
//func (m *decidedGenesisMigrator) migrate(dr *decidedReader) (int, error) {
//	if migrateDecided0, err := m.check(dr); err != nil {
//		return 0, err
//	} else if migrateDecided0 {
//		return dr.newHistorySync().StartRange(uint64(0), uint64(1))
//	}
//	return 0, nil
//}
//
//// check determines if the given reader should migrate
//func (m *decidedGenesisMigrator) check(dr *decidedReader) (bool, error) {
//	_, found, err := dr.storage.GetDecided(dr.identifier, uint64(0))
//	if err != nil {
//		return false, err
//	}
//	if found {
//		return false, nil
//	}
//	return true, nil
//}
