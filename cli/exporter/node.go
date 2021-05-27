package exporter

import (
	"fmt"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	ExporterOptions            exporter.Options `yaml:"exporter"`
	DBOptions                  basedb.Options   `yaml:"db"`
	PrivateKey                 string           `yaml:"PrivateKey" env:"EXPORTER_NODE_PRIVATE_KEY" env-description:"exporter node private key (default will generate new)"`

	Network                    string           `yaml:"Network" env-default:"pyrmont"`
	DiscoveryType              string           `yaml:"DiscoveryType" env-default:"mdns"`
	TCPPort                    int              `yaml:"TcpPort" env-default:"13000"`
	UDPPort                    int              `yaml:"UdpPort" env-default:"12000"`
	HostAddress                string           `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS                    string           `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`
}

var cfg config

var globalArgs global_config.Args

// StartExporterNodeCmd is the command to start SSV boot node
var StartExporterNodeCmd = &cobra.Command{
	Use:   "start-exporter-node",
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
		p2pCfg := p2p.Config{
			DiscoveryType: cfg.DiscoveryType,
			BootstrapNodeAddr: []string{
				// deployemnt
				// internal ip
				//"enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
				//external ip
				"enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
				// ssh
				//"enr:-LK4QAkFwcROm9CByx3aabpd9Muqxwj8oQeqnr7vm8PAA8l1ZbDWVZTF_bosINKhN4QVRu5eLPtyGCccRPb3yKG2xjcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQMCphx1UQ1PkBsdOb-4FRiSWM4JE7HoDarAzOp82SO4s4N0Y3CCE4iDdWRwgg-g",
			},
			UDPPort:     cfg.UDPPort,
			TCPPort:     cfg.TCPPort,
			HostDNS:     cfg.HostDNS,
			HostAddress: cfg.HostAddress,
		}
		network, err := p2p.New(cmd.Context(), Logger, &p2pCfg)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		cfg.ExporterOptions.Logger = Logger
		cfg.ExporterOptions.Network = network

		exporterNode := exporter.New(cfg.ExporterOptions)
		if err := exporterNode.Start(cmd.Context()); err != nil {
			Logger.Fatal("failed to start exporter node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartExporterNodeCmd)
}
