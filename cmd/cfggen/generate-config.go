package main

import (
	"encoding/hex"
	"flag"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
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

func main() {
	outputPath := flag.String("output-path", defaultOutputPath, "Output path for generated config")

	logLevel := flag.String("log-level", defaultLogLevel, "Log level")
	dbPath := flag.String("db-path", defaultDBPath, "DB path")
	discovery := flag.String("discovery", defaultDiscovery, "Discovery")
	consensusClient := flag.String("consensus-client", "", "Consensus client")
	executionClient := flag.String("execution-client", "", "Execution client")
	operatorPrivateKey := flag.String("operator-private-key", "", "Secret key")
	metricsAPIPort := flag.Int("metrics-api-port", 0, "Metrics API port")

	networkName := flag.String("network-name", defaultNetwork.Name, "Network name")
	networkGenesisDomain := flag.String("network-genesis-domain", "0x"+hex.EncodeToString(defaultNetwork.GenesisDomainType[:]), "Network genesis domain")
	networkAlanDomain := flag.String("network-alan-domain", "0x"+hex.EncodeToString(defaultNetwork.AlanDomainType[:]), "Network alan domain")
	networkRegistrySyncOffset := flag.Uint64("network-registry-sync-offset", defaultNetwork.RegistrySyncOffset.Uint64(), "Network registry sync offset")
	networkRegistryContractAddr := flag.String("network-registry-contract-addr", defaultNetwork.RegistryContractAddr.String(), "Network registry contract addr")
	networkBootnodes := flag.String("network-bootnodes", strings.Join(defaultNetwork.Bootnodes, sliceSeparator), "Network bootnodes (comma-separated)")
	networkDiscoveryProtocolID := flag.String("network-discovery-protocol-id", "0x"+hex.EncodeToString(defaultNetwork.DiscoveryProtocolID[:]), "Network discovery protocol ID")
	networkAlanForkEpoch := flag.Uint64("network-alan-fork-epoch", uint64(defaultNetwork.AlanForkEpoch), "Network Alan fork epoch")

	flag.Parse()

	if *consensusClient == "" {
		log.Fatalf("The --consensus-client flag is mandatory")
	}

	if *executionClient == "" {
		log.Fatalf("The --execution-client flag is mandatory")
	}

	parsedGenesisDomain, err := hex.DecodeString(strings.TrimPrefix(*networkGenesisDomain, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode genesis network domain: %v", err)
	}

	parsedAlanDomain, err := hex.DecodeString(strings.TrimPrefix(*networkAlanDomain, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode alan network domain: %v", err)
	}

	parsedDiscoveryProtocolID, err := hex.DecodeString(strings.TrimPrefix(*networkDiscoveryProtocolID, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode discovery protocol ID: %v", err)
	}

	var parsedDiscoveryProtocolIDArr [6]byte
	if len(parsedDiscoveryProtocolID) != 0 {
		parsedDiscoveryProtocolIDArr = [6]byte(parsedDiscoveryProtocolID)
	}

	var bootnodes []string
	if *networkBootnodes != "" {
		bootnodes = strings.Split(*networkBootnodes, sliceSeparator)
	}

	var config Config
	config.Global.LogLevel = *logLevel
	config.DB.Path = *dbPath
	config.ConsensusClient.Address = *consensusClient
	config.ExecutionClient.Address = *executionClient
	config.P2P.Discovery = *discovery
	config.OperatorPrivateKey = *operatorPrivateKey
	config.MetricsAPIPort = *metricsAPIPort
	config.SSV.CustomNetwork = &networkconfig.SSV{
		Name:                 *networkName,
		GenesisDomainType:    spectypes.DomainType(parsedGenesisDomain),
		AlanDomainType:       spectypes.DomainType(parsedAlanDomain),
		RegistrySyncOffset:   new(big.Int).SetUint64(*networkRegistrySyncOffset),
		RegistryContractAddr: ethcommon.HexToAddress(*networkRegistryContractAddr),
		Bootnodes:            bootnodes,
		DiscoveryProtocolID:  parsedDiscoveryProtocolIDArr,
		AlanForkEpoch:        phase0.Epoch(*networkAlanForkEpoch),
	}

	data, err := yaml.Marshal(&config)
	if err != nil {
		log.Fatalf("Failed to marshal YAML: %v", err)
	}

	err = os.WriteFile(*outputPath, data, configFilePermissions)
	if err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	log.Printf("Saved config into '%s'", *outputPath)
}
