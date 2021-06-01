package exporter

import (
	"crypto/rsa"
	"fmt"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"sync"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	ExporterOptions            exporter.Options `yaml:"exporter"`
	DBOptions                  basedb.Options   `yaml:"db"`
	P2pNetworkConfig           p2p.Config       `yaml:"network"`

	ETH1Addr   string `yaml:"ETH1Addr" env-required:"true"`
	PrivateKey string `yaml:"PrivateKey" env:"EXPORTER_NODE_PRIVATE_KEY" env-description:"exporter node private key (default will generate new)"`
	Network    string `yaml:"Network" env-default:"pyrmont"`
}

var cfg config

var globalArgs global_config.Args

// StartExporterNodeCmd is the command to start SSV boot node
var StartExporterNodeCmd = &cobra.Command{
	Use:   "start-exporter",
	Short: "Starts exporter node",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}

		loggerLevel, err := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(cmd.Parent().Short, loggerLevel)
		if err != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(err))
		}
		cfg.DBOptions.Logger = Logger
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		var eth1Client eth1.Client
		if cfg.ETH1Addr != "" {
			//operatorStorage := collections.NewOperatorStorage(db, Logger)
			//if err := operatorStorage.SetupPrivateKey(cfg.OperatorKey); err != nil {
			//	Logger.Fatal("failed to setup operator private key", zap.Error(err))
			//}
			eth1Client, err = goeth.NewEth1Client(goeth.ClientOptions{
				Ctx: cmd.Context(), Logger: Logger, NodeAddr: cfg.ETH1Addr,
				PrivKeyProvider: func() (*rsa.PrivateKey, error) {
					return nil, nil
				},
			})
			if err != nil {
				Logger.Fatal("failed to create eth1 client", zap.Error(err))
			}
		} else {
			Logger.Fatal("eth1 address was not provided", zap.Error(err))
		}

		cfg.ExporterOptions.Eth1Client = eth1Client
		cfg.ExporterOptions.Logger = Logger
		cfg.ExporterOptions.Network = network
		cfg.ExporterOptions.DB = db

		exporterNode := exporter.New(cfg.ExporterOptions)
		var syncProcess sync.WaitGroup
		syncProcess.Add(1)
		go func() {
			Logger.Debug("about to sync exporter")
			defer syncProcess.Done()
			err := exporterNode.Sync()
			if err != nil {
				Logger.Error("failed to sync exporter node", zap.Error(err))
			} else {
				Logger.Debug("sync is done")
			}
		}()
		syncProcess.Wait()
		if err := exporterNode.Start(); err != nil {
			Logger.Fatal("failed to start exporter node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartExporterNodeCmd)
}
