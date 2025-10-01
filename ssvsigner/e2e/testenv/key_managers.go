package testenv

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"

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
	localDB, err := badger.New(logger, basedb.Options{
		Path: env.localKeyManagerPath,
	})
	if err != nil {
		return fmt.Errorf("failed to create local database: %w", err)
	}
	env.localDB = localDB

	localKeyManager, err := ekm.NewLocalKeyManager(
		logger,
		localDB,
		env.beaconConfig,
		env.operatorKey,
	)
	if err != nil {
		return fmt.Errorf("failed to create local key manager: %w", err)
	}
	env.localKeyManager = localKeyManager

	return nil
}

// createRemoteKeyManager creates and configures the RemoteKeyManager
func (env *TestEnvironment) createRemoteKeyManager(logger *zap.Logger) error {
	// Only create database on first initialization
	if env.remoteDB == nil {
		remoteDB, err := badger.New(logger, basedb.Options{
			Path: env.remoteKeyManagerPath,
		})
		if err != nil {
			return fmt.Errorf("failed to create remote database: %w", err)
		}
		env.remoteDB = remoteDB
	}

	remoteKeyManager, err := ekm.NewRemoteKeyManager(
		env.ctx,
		logger,
		env.beaconConfig,
		env, // TestEnvironment implements signerClient interface by delegating to ssvSignerClient
		env.remoteDB,
		1,
	)
	if err != nil {
		return fmt.Errorf("failed to create remote key manager: %w", err)
	}
	env.remoteKeyManager = remoteKeyManager

	return nil
}
