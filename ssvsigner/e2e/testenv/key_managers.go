package testenv

import (
	"errors"
	"fmt"

	cockroachdb "github.com/cockroachdb/pebble"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	storagepebble "github.com/ssvlabs/ssv/storage/pebble"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// initializeKeyManagers initializes both local and remote key managers
func (env *TestEnvironment) initializeKeyManagers() error {
	logger := zaptest.NewLogger(nil)

	if err := env.createLocalKeyManager(logger); err != nil {
		return fmt.Errorf("failed to create local key manager: %w", err)
	}

	if err := env.createRemoteKeyManager(logger); err != nil {
		return fmt.Errorf("failed to create remote key manager: %w", err)
	}

	return nil
}

// createLocalKeyManager creates and configures the LocalKeyManager
func (env *TestEnvironment) createLocalKeyManager(logger *zap.Logger) error {
	localDB, err := storagepebble.New(logger, env.localKeyManagerPath, &cockroachdb.Options{})
	if err != nil {
		return fmt.Errorf("failed to create local database: %w", err)
	}

	localKeyManager, err := ekm.NewLocalKeyManager(
		logger,
		localDB,
		env.beaconConfig,
		env.operatorKey,
	)
	if err != nil {
		err = fmt.Errorf("failed to create local key manager: %w", err)
		if closeErr := localDB.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("close local database after key manager setup failure: %w", closeErr))
		}
		return err
	}
	env.localDB = localDB
	env.localKeyManager = localKeyManager

	return nil
}

// createRemoteKeyManager creates and configures the RemoteKeyManager
func (env *TestEnvironment) createRemoteKeyManager(logger *zap.Logger) error {
	remoteDB := env.remoteDB
	createdRemoteDB := false
	// Only create database on first initialization
	if remoteDB == nil {
		var err error
		remoteDB, err = storagepebble.New(logger, env.remoteKeyManagerPath, &cockroachdb.Options{})
		if err != nil {
			return fmt.Errorf("failed to create remote database: %w", err)
		}
		createdRemoteDB = true
	}

	remoteKeyManager, err := ekm.NewRemoteKeyManager(
		env.ctx,
		logger,
		env.beaconConfig,
		env, // TestEnvironment implements signerClient interface by delegating to ssvSignerClient
		remoteDB,
		func() spectypes.OperatorID { return 1 }, // operator ID getter
	)
	if err != nil {
		err = fmt.Errorf("failed to create remote key manager: %w", err)
		if createdRemoteDB {
			if closeErr := remoteDB.Close(); closeErr != nil {
				return errors.Join(err, fmt.Errorf("close remote database after key manager setup failure: %w", closeErr))
			}
		}
		return err
	}
	env.remoteDB = remoteDB
	env.remoteKeyManager = remoteKeyManager

	return nil
}
