package operator

import (
	"fmt"
	"path/filepath"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

// Flag names.
const (
	networkConfigNameFlag = "network-config-name"
	dbPathFlag            = "db-path"
	configPathFlag        = "config-path"
)

type SlashingProtectionOptions struct {
	DBPath string `yaml:"DBPath" env:"SP_DB_PATH" env-description:"Path for slashing protection db"`
}

var CreateSlashingProtectionDBCmd = &cobra.Command{
	Use:   "create-slashing-protection-db",
	Short: "Create the slashing protection database",
	Run: func(cmd *cobra.Command, args []string) {
		logger := zap.L().Named(logging.NameExportKeys)

		configPath, err := GetConfigPathFlagValue(cmd)
		if err != nil {
			logger.Panic("failed to get config path flag value: ", zap.Error(err))
		}

		if configPath != "" {
			if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
				logger.Panic("failed to read config: ", zap.Error(err))
			}
		}

		network, err := GetNetworkFlagValue(cmd)
		if err != nil {
			logger.Panic("failed to get network flag value: ", zap.Error(err))
		}

		dbPath, err := GetDBPathFlagValue(cmd)
		if err != nil {
			logger.Panic("failed to get db path flag value", zap.Error(err))
		}

		options := basedb.Options{
			Path:       dbPath,
			SyncWrites: true,
		}
		db, err := kv.New(cmd.Context(), nil, options)
		if err != nil {
			logger.Panic("failed to create slashing protection db: ", zap.Error(err))
		}

		storage := ekm.NewSlashingProtectionStorage(db, logger, []byte(network))
		if err = storage.SetVersion(ekm.GenesisVersion); err != nil {
			logger.Panic("failed to set genesis version: ", zap.Error(err))
		}

		if err := db.Close(); err != nil {
			logger.Panic("failed to close slashing protection db: ", zap.Error(err))
		}
	},
}

// AddConfigPathFlagValue adds the config path flag to the command
func AddConfigPathFlagValue(c *cobra.Command) {
	c.Flags().String(configPathFlag, "", "Path to the config file")
}

// GetConfigPathFlagValue gets the config path flag from the command or from the global args
func GetConfigPathFlagValue(c *cobra.Command) (string, error) {
	configPath, err := c.Flags().GetString(configPathFlag)
	if err != nil {
		return "", err
	}

	if configPath != "" {
		return configPath, nil
	}

	return globalArgs.ConfigPath, nil
}

// AddNetworkFlagValue adds the network flag to the command
func AddNetworkFlagValue(c *cobra.Command) {
	c.Flags().String(networkConfigNameFlag, "", "Network config name: one of the supported network config names")
}

// GetNetworkFlagValue gets the network flag from the command or config file
func GetNetworkFlagValue(c *cobra.Command) (spectypes.BeaconNetwork, error) {
	networkName, err := c.Flags().GetString(networkConfigNameFlag)
	if err != nil {
		return "", err
	}

	if networkName == "" {
		networkName = cfg.SSVOptions.NetworkName
	}

	networkConfig, err := networkconfig.GetNetworkConfigByName(networkName)
	if err != nil {
		return "", fmt.Errorf("failed to get network config by name: %w", err)
	}

	return networkConfig.Beacon.GetBeaconNetwork(), nil
}

// AddDBPathFlagValue adds the db path flag to the command
func AddDBPathFlagValue(c *cobra.Command) {
	c.Flags().String(dbPathFlag, "", "Path to create the slashing protection DB at")
}

// GetDBPathFlagValue gets the db path flag from the command or config file
func GetDBPathFlagValue(c *cobra.Command) (string, error) {
	dbPath, err := c.Flags().GetString(dbPathFlag)
	if err != nil {
		return "", err
	}

	if dbPath != "" {
		if cfg.DBOptions.Path != "" {
			// Validate that the slashing protection DB and node DB are not in the same directory
			if filepath.Dir(dbPath) == filepath.Dir(cfg.DBOptions.Path) {
				return "", fmt.Errorf("node DB and slashing protection DB should not be in the same directory")
			}
		}

		return dbPath, nil
	}

	if cfg.SlashingProtectionOptions.DBPath != "" {
		return cfg.SlashingProtectionOptions.DBPath, nil
	}

	return "", fmt.Errorf("no slashing protection database path provided")
}

func init() {
	AddNetworkFlagValue(CreateSlashingProtectionDBCmd)
	AddDBPathFlagValue(CreateSlashingProtectionDBCmd)
	AddConfigPathFlagValue(CreateSlashingProtectionDBCmd)
}
