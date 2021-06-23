package exporter

import (
	"crypto/rsa"
	"fmt"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/adapters/gorilla"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"net/http"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options `yaml:"db"`
	P2pNetworkConfig           p2p.Config     `yaml:"p2p"`

	ExporterConfig             exporter.Options `yaml:"exporter"`

	ETH1Addr       string `yaml:"ETH1Addr" env-required:"true"`
	ETH1SyncOffset string `yaml:"ETH1SyncOffset" env:"ETH_1_SYNC_OFFSET"`
	Network        string `yaml:"Network" env-default:"prater"`
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
		// configure logger and db
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
		eth1Client, err := goeth.NewEth1Client(goeth.ClientOptions{
			Ctx: cmd.Context(), Logger: Logger, NodeAddr: cfg.ETH1Addr,
			// using an empty private key provider
			// because the exporter doesn't run in the context of an operator
			PrivKeyProvider: func() (*rsa.PrivateKey, error) {
				return nil, nil
			},
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		cfg.ExporterConfig.Eth1Client = eth1Client
		cfg.ExporterConfig.Logger = Logger
		cfg.ExporterConfig.Network = network
		cfg.ExporterConfig.DB = db
		cfg.ExporterConfig.Ctx = cmd.Context()
		cfg.ExporterConfig.WS = api.NewWsServer(Logger, gorilla.NewGorillaAdapter(Logger), http.NewServeMux())

		exporterNode := exporter.New(cfg.ExporterConfig)

		if err := exporterNode.StartEth1(eth1.NewSyncOffset(cfg.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if err := exporterNode.Start(); err != nil {
			Logger.Fatal("failed to start exporter", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartExporterNodeCmd)
}
