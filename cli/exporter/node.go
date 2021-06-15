package exporter

import (
	"crypto/rsa"
	"fmt"
	global_config "github.com/bloxapp/ssv/cli/config"
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
	"time"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options   `yaml:"db"`
	P2pNetworkConfig           p2p.Config       `yaml:"network"`

	ETH1Addr   string `yaml:"ETH1Addr" env-required:"true"`
	PrivateKey string `yaml:"PrivateKey" env:"EXPORTER_NODE_PRIVATE_KEY" env-description:"exporter node private key (default will generate new)"`
	Network    string `yaml:"Network" env-default:"prater"`
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
		bootstrapNodeAddr := []string{
			// deployemnt
			// internal ip
			//"enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			//external ip
			"enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			// ssh
			//"enr:-LK4QAkFwcROm9CByx3aabpd9Muqxwj8oQeqnr7vm8PAA8l1ZbDWVZTF_bosINKhN4QVRu5eLPtyGCccRPb3yKG2xjcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQMCphx1UQ1PkBsdOb-4FRiSWM4JE7HoDarAzOp82SO4s4N0Y3CCE4iDdWRwgg-g",
		}
		if cfg.P2pNetworkConfig.Enr != "" {
			bootstrapNodeAddr = []string{
				cfg.P2pNetworkConfig.Enr,
			}
		}
		cfg.P2pNetworkConfig.BootstrapNodeAddr = bootstrapNodeAddr
		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		if cfg.ETH1Addr == "" {
			Logger.Fatal("eth1 address was not provided", zap.Error(err))
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
		exporterOptions := new(exporter.Options)
		exporterOptions.Eth1Client = eth1Client
		exporterOptions.Logger = Logger
		exporterOptions.Network = network
		exporterOptions.DB = db
		exporterOptions.Ctx = cmd.Context()

		exporterNode := exporter.New(*exporterOptions)

		eth1EventChan, err := eth1Client.EventsSubject().Register("Eth1ExporterObserver")
		if err != nil {
			Logger.Fatal("could not register for eth1 events subject", zap.Error(err))
		}
		go exporterNode.ListenToEth1Events(eth1EventChan)

		var syncProcess sync.WaitGroup
		syncProcess.Add(1)
		go func() {
			defer func() {
				// 100ms delay after sync
				time.Sleep(100 * time.Millisecond)
				syncProcess.Done()
			}()
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
