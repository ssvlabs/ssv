package cli

import (
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"gopkg.in/yaml.v2"

	networkconfig "github.com/ssvlabs/ssv/network/config"
)

const (
	defaultOutputPath     = "./config/config.local.yaml"
	defaultLogLevel       = "info"
	defaultDBPath         = "./data/db"
	defaultDiscovery      = "mdns"
	sliceSeparator        = ","
	configFilePermissions = 0644
)

var (
	defaultNetwork = networkconfig.LocalTestnetSSV
)

var (
	outputPath                   string
	logLevel                     string
	dbPath                       string
	discovery                    string
	consensusClient              string
	executionClient              string
	operatorPrivateKey           string
	metricsAPIPort               int
	ssvNetworkName               string
	ssvGenesisDomain             string
	ssvAlanDomain                string
	ssvAlanForkEpoch             uint64
	ssvRegistrySyncOffset        uint64
	ssvRegistryContractAddr      string
	ssvBootnodes                 string
	ssvDiscoveryProtocolID       string
	ssvMaxValidatorsPerCommittee int
	ssvTotalEthereumValidators   int
)

type SSVConfig struct {
	Global struct {
		LogLevel string `yaml:"LogLevel,omitempty"`
	} `yaml:"global,omitempty"`
	DB struct {
		Path string `yaml:"Path,omitempty"`
	} `yaml:"db,omitempty"`
	ConsensusClient struct {
		Address string `yaml:"BeaconNodeAddr,omitempty"`
	} `yaml:"eth2,omitempty"`
	ExecutionClient struct {
		Address string `yaml:"ETH1Addr,omitempty"`
	} `yaml:"eth1,omitempty"`
	P2P struct {
		Discovery string `yaml:"Discovery,omitempty"`
	} `yaml:"p2p,omitempty"`
	SSV struct {
		NetworkName   string             `yaml:"Network,omitempty" env:"NETWORK" env-description:"Network is the network of this node,omitempty"`
		CustomNetwork *networkconfig.SSV `yaml:"CustomNetwork,omitempty" env:"CUSTOM_NETWORK" env-description:"Custom network parameters,omitempty"`
	} `yaml:"ssv,omitempty"`
	OperatorPrivateKey string `yaml:"OperatorPrivateKey,omitempty"`
	MetricsAPIPort     int    `yaml:"MetricsAPIPort,omitempty"`
}

// generateConfigCmd is the command to generate ssv operator config.
var generateConfigCmd = &cobra.Command{
	Use:   "generate-config",
	Short: "generates ssv operator config",
	Run: func(cmd *cobra.Command, args []string) {
		parsedGenesisDomain, err := hex.DecodeString(strings.TrimPrefix(ssvGenesisDomain, "0x"))
		if err != nil {
			log.Fatalf("Failed to decode genesis network domain: %v", err)
		}

		parsedAlanDomain, err := hex.DecodeString(strings.TrimPrefix(ssvAlanDomain, "0x"))
		if err != nil {
			log.Fatalf("Failed to decode alan network domain: %v", err)
		}

		parsedDiscoveryProtocolID, err := hex.DecodeString(strings.TrimPrefix(ssvDiscoveryProtocolID, "0x"))
		if err != nil {
			log.Fatalf("Failed to decode discovery protocol ID: %v", err)
		}

		var parsedDiscoveryProtocolIDArr [6]byte
		if len(parsedDiscoveryProtocolID) != 0 {
			parsedDiscoveryProtocolIDArr = [6]byte(parsedDiscoveryProtocolID)
		}

		var bootnodes []string
		if ssvBootnodes != "" {
			bootnodes = strings.Split(ssvBootnodes, sliceSeparator)
		}

		var config SSVConfig
		config.Global.LogLevel = logLevel
		config.DB.Path = dbPath
		config.ConsensusClient.Address = consensusClient
		config.ExecutionClient.Address = executionClient
		config.P2P.Discovery = discovery
		config.OperatorPrivateKey = operatorPrivateKey
		config.MetricsAPIPort = metricsAPIPort
		config.SSV.CustomNetwork = &networkconfig.SSV{
			Name:                      ssvNetworkName,
			GenesisDomainType:         spectypes.DomainType(parsedGenesisDomain),
			AlanDomainType:            spectypes.DomainType(parsedAlanDomain),
			RegistrySyncOffset:        new(big.Int).SetUint64(ssvRegistrySyncOffset),
			RegistryContractAddr:      ethcommon.HexToAddress(ssvRegistryContractAddr),
			Bootnodes:                 bootnodes,
			DiscoveryProtocolID:       parsedDiscoveryProtocolIDArr,
			AlanForkEpoch:             phase0.Epoch(ssvAlanForkEpoch),
			MaxValidatorsPerCommittee: ssvMaxValidatorsPerCommittee,
			TotalEthereumValidators:   ssvTotalEthereumValidators,
		}

		data, err := yaml.Marshal(&config)
		if err != nil {
			log.Fatalf("Failed to marshal YAML: %v", err)
		}

		err = os.WriteFile(outputPath, data, configFilePermissions)
		if err != nil {
			log.Fatalf("Failed to write file: %v", err)
		}

		log.Printf("Saved config into '%s':", outputPath)
		fmt.Println(string(data))
	},
}

func init() {
	generateConfigCmd.Flags().StringVarP(&outputPath, "output-path", "o", defaultOutputPath, "Output path for generated config")
	generateConfigCmd.Flags().StringVar(&logLevel, "log-level", defaultLogLevel, "Log level")
	generateConfigCmd.Flags().StringVar(&dbPath, "db-path", defaultDBPath, "DB path")
	generateConfigCmd.Flags().StringVar(&discovery, "discovery", defaultDiscovery, "Discovery")
	generateConfigCmd.Flags().StringVar(&consensusClient, "consensus-client", "", "Consensus client (required)")
	_ = generateConfigCmd.MarkFlagRequired("consensus-client")
	generateConfigCmd.Flags().StringVar(&executionClient, "execution-client", "", "Execution client (required)")
	_ = generateConfigCmd.MarkFlagRequired("execution-client")
	generateConfigCmd.Flags().StringVar(&operatorPrivateKey, "operator-private-key", "", "Secret key")
	generateConfigCmd.Flags().IntVar(&metricsAPIPort, "metrics-api-port", 0, "Metrics API port")

	generateConfigCmd.Flags().StringVar(&ssvNetworkName, "ssv-network-name", defaultNetwork.Name, "SSV network name")
	ssvGenesisDomainDefault := "0x" + hex.EncodeToString(defaultNetwork.GenesisDomainType[:])
	generateConfigCmd.Flags().StringVar(&ssvGenesisDomain, "ssv-genesis-domain", ssvGenesisDomainDefault, "SSV genesis domain")
	ssvAlanDomainDefault := "0x" + hex.EncodeToString(defaultNetwork.AlanDomainType[:])
	generateConfigCmd.Flags().StringVar(&ssvAlanDomain, "ssv-alan-domain", ssvAlanDomainDefault, "SSV Alan fork domain type")
	generateConfigCmd.Flags().Uint64Var(&ssvAlanForkEpoch, "ssv-alan-fork-epoch", uint64(defaultNetwork.AlanForkEpoch), "SSV Alan fork epoch")
	generateConfigCmd.Flags().Uint64Var(&ssvRegistrySyncOffset, "ssv-registry-sync-offset", defaultNetwork.RegistrySyncOffset.Uint64(), "SSV registry sync offset")
	generateConfigCmd.Flags().StringVar(&ssvRegistryContractAddr, "ssv-registry-contract-addr", defaultNetwork.RegistryContractAddr.String(), "SSV registry contract addr")
	generateConfigCmd.Flags().StringVar(&ssvBootnodes, "ssv-bootnodes", strings.Join(defaultNetwork.Bootnodes, sliceSeparator), "SSV bootnodes (comma-separated)")
	ssvDiscoveryProtocolIDDefault := "0x" + hex.EncodeToString(defaultNetwork.DiscoveryProtocolID[:])
	generateConfigCmd.Flags().StringVar(&ssvDiscoveryProtocolID, "ssv-discovery-protocol-id", ssvDiscoveryProtocolIDDefault, "SSV discovery protocol ID")
	generateConfigCmd.Flags().IntVar(&ssvMaxValidatorsPerCommittee, "ssv-max-validators-per-committee", defaultNetwork.MaxValidatorsPerCommittee, "SSV max validators per committee")
	generateConfigCmd.Flags().IntVar(&ssvTotalEthereumValidators, "ssv-total-ethereum-validators", defaultNetwork.TotalEthereumValidators, "Total ethereum network validators")

	RootCmd.AddCommand(generateConfigCmd)
}
