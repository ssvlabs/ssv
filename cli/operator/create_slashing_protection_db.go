package operator

import (
	"fmt"
	"path/filepath"

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
		logger := zap.L().Named(logging.NameCreateSlashingProtectionDB)

		var cfg config
		configPath, err := GetConfigPathFlagValue(cmd, globalArgs.ConfigPath)
		if err != nil {
			logger.Panic("failed to get config path flag value", zap.Error(err))
		}
		if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
			logger.Panic("failed to read config", zap.Error(err))
		}

		networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.SSVOptions.NetworkName)
		if err != nil {
			logger.Panic("failed to get network config by name", zap.Error(err))
		}

		dbPath, err := GetDBPathFlagValue(cmd, &cfg)
		if err != nil {
			logger.Panic("failed to get db path flag value", zap.Error(err))
		}

		options := basedb.Options{
			Path:       dbPath,
			SyncWrites: true,
		}
		db, err := kv.New(cmd.Context(), nil, options)
		if err != nil {
			logger.Panic("failed to create slashing protection db", zap.Error(err))
		}

		isEmpty, err := db.IsEmpty()
		if err != nil {
			logger.Panic("failed to check database emptiness", zap.Error(err))
		}
		if !isEmpty {
			logger.Panic("database is not empty")
		}

		storage := ekm.NewSlashingProtectionStorage(db, logger, []byte(networkConfig.Beacon.GetBeaconNetwork()))
		if err = storage.SetVersion(ekm.GenesisVersion); err != nil {
			logger.Panic("failed to set genesis version: ", zap.Error(err))
		}

		if err := db.Close(); err != nil {
			logger.Panic("failed to close slashing protection db", zap.Error(err))
		}
	},
}

// AddConfigPathFlagValue adds the config path flag to the command
func AddConfigPathFlagValue(c *cobra.Command) {
	c.Flags().String(configPathFlag, "", "Path to the config file")
}

// GetConfigPathFlagValue gets the config path flag from the command or from the global args
func GetConfigPathFlagValue(c *cobra.Command, defaultPath string) (string, error) {
	configPath, err := c.Flags().GetString(configPathFlag)
	if err != nil {
		return "", err
	}

	if configPath != "" {
		return configPath, nil
	}

	return defaultPath, nil
}

// AddDBPathFlagValue adds the db path flag to the command
func AddDBPathFlagValue(c *cobra.Command) {
	c.Flags().String(dbPathFlag, "", "Path to create the slashing protection DB at")
}

// GetDBPathFlagValue gets the db path flag from the command or config file
func GetDBPathFlagValue(c *cobra.Command, cfg *config) (string, error) {
	dbPath, err := c.Flags().GetString(dbPathFlag)
	if err != nil {
		return "", err
	}

	if dbPath == "" {
		dbPath = cfg.SlashingProtectionOptions.DBPath
	}

	if dbPath == "" {
		return "", fmt.Errorf("no slashing protection database path provided")
	}

	// Validate that the slashing protection DB and node DB are not in the same directory
	if filepath.Dir(dbPath) == filepath.Dir(cfg.DBOptions.Path) {
		return "", fmt.Errorf("node DB (db.Path) and slashing protection DB (slashing_protection.DBPath) should not be in the same directory")
	}

	return dbPath, nil
}

func init() {
	AddDBPathFlagValue(CreateSlashingProtectionDBCmd)
	AddConfigPathFlagValue(CreateSlashingProtectionDBCmd)
}
