package operator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils"
)

func TestCreateSlashingProtectionDBCmd(t *testing.T) {
	logger := zap.L()
	cmd := CreateSlashingProtectionDBCmd
	cmd.SetContext(context.Background())

	// Set the config path flag to the temporary directory
	f, err := utils.CreateMockConfigFile(t.TempDir(), "")
	require.NoError(t, err, "Failed to create mock config file")

	// close and remove the temporary file at the end of the test
	defer f.Close()
	defer os.Remove(f.Name())

	filePath, err := filepath.Abs(f.Name())
	require.NoError(t, err, "Failed to get absolute path of temporary config file")
	require.NoError(t, cmd.Flags().Set(configPathFlag, filePath), "Failed to set config path flag")

	// Set the DB path flag to the temporary directory
	tmpDir := t.TempDir()
	require.NoError(t, cmd.Flags().Set(dbPathFlag, tmpDir))

	// Run the command and expect it not to fail
	require.NotPanics(t, func() {
		cmd.Run(cmd, []string{})
	}, "The command should not panic if a valid DB path is provided")

	// Check that the DB file was created
	_, err = os.Stat(tmpDir)
	require.NoError(t, err, "DB file should have been created")

	db, err := kv.New(cmd.Context(), logger, basedb.Options{
		Path: tmpDir,
	})
	require.NoError(t, err, "Failed to create DB instance")

	var cfg config
	require.NoError(t, cleanenv.ReadConfig(filePath, &cfg), "Failed to read config file")

	networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.SSVOptions.NetworkName)
	if err != nil {
		logger.Panic("failed to get network config by name", zap.Error(err))
	}

	storage := ekm.NewSlashingProtectionStorage(db, logger, []byte(networkConfig.Beacon.GetBeaconNetwork()))
	version, found, err := storage.GetVersion()

	require.NoError(t, err, "Failed to get genesis version from DB")
	require.True(t, found, "Genesis version should have been found in DB")
	require.NotNil(t, version, "Genesis version should not be nil")
	require.Equal(t, ekm.GenesisVersion, version, "Genesis version should be correct")
}

func TestCreateSlashingProtectionDBCmd_NoDBPath(t *testing.T) {
	cmd := CreateSlashingProtectionDBCmd
	cmd.SetContext(context.Background())

	// Run the command and expect it to fail
	require.Panics(t, func() {
		cmd.Run(cmd, []string{})
	}, "The command should panic if no DB path is provided")
}

func TestCreateSlashingProtectionDBCmd_InvalidDBPath(t *testing.T) {
	cmd := CreateSlashingProtectionDBCmd
	cmd.SetContext(context.Background())

	// Set the DB path flag to an invalid path
	require.NoError(t, cmd.Flags().Set(dbPathFlag, "/invalid/path"))

	// Run the command and expect it to fail
	require.Panics(t, func() {
		CreateSlashingProtectionDBCmd.Run(cmd, []string{})
	}, "The command should panic if an invalid DB path is provided")

	// Check that the DB file was not created
	_, err := os.Stat("/invalid/path")
	require.Error(t, err, "DB file should not have been created")
}

func TestCreateSlashingProtectionDBCmd_DBPath_Same_Directory(t *testing.T) {
	cmd := CreateSlashingProtectionDBCmd
	cmd.SetContext(context.Background())

	// Set the config path flag to the temporary directory
	tmpDBDir := t.TempDir()
	f, err := utils.CreateMockConfigFile(tmpDBDir, tmpDBDir)
	require.NoError(t, err, "Failed to create mock config file")

	// close and remove the temporary file at the end of the test
	defer f.Close()
	defer os.Remove(f.Name())

	filePath, err := filepath.Abs(f.Name())
	require.NoError(t, err, "Failed to get absolute path of temporary config file")
	require.NoError(t, cmd.Flags().Set(configPathFlag, filePath), "Failed to set config path flag")

	// Set the DB path flag to the temporary directory
	tmpDir := t.TempDir()
	require.NoError(t, cmd.Flags().Set(dbPathFlag, tmpDir))

	// Run the command and expect it to fail
	require.Panics(t, func() {
		cmd.Run(cmd, []string{})
	}, "The command should panic if the same DB path is provided as the config db path")
}
