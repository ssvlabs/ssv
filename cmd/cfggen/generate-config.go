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

	ssvNetworkName := flag.String("ssv-network-name", defaultNetwork.Name, "SSV network name")
	ssvGenesisDomain := flag.String("ssv-genesis-domain", "0x"+hex.EncodeToString(defaultNetwork.GenesisDomainType[:]), "SSV genesis domain")
	ssvAlanDomain := flag.String("ssv-alan-domain", "0x"+hex.EncodeToString(defaultNetwork.AlanDomainType[:]), "SSV Alan fork domain type")
	ssvAlanForkEpoch := flag.Uint64("ssv-alan-fork-epoch", uint64(defaultNetwork.AlanForkEpoch), "SSV Alan fork epoch")
	ssvRegistrySyncOffset := flag.Uint64("ssv-registry-sync-offset", defaultNetwork.RegistrySyncOffset.Uint64(), "SSV registry sync offset")
	ssvRegistryContractAddr := flag.String("ssv-registry-contract-addr", defaultNetwork.RegistryContractAddr.String(), "SSV registry contract addr")
	ssvBootnodes := flag.String("ssv-bootnodes", strings.Join(defaultNetwork.Bootnodes, sliceSeparator), "SSV bootnodes (comma-separated)")
	ssvDiscoveryProtocolID := flag.String("ssv-discovery-protocol-id", "0x"+hex.EncodeToString(defaultNetwork.DiscoveryProtocolID[:]), "SSV discovery protocol ID")
	ssvMaxValidatorsPerCommittee := flag.Int("ssv-max-validators-per-committee", defaultNetwork.MaxValidatorsPerCommittee, "SSV max validators per committee")
	ssvTotalEthereumValidators := flag.Int("ssv-total-ethereum-validators", defaultNetwork.TotalEthereumValidators, "Total ethereum network validators")

	flag.Parse()

	if *consensusClient == "" {
		log.Fatalf("The --consensus-client flag is mandatory")
	}

	if *executionClient == "" {
		log.Fatalf("The --execution-client flag is mandatory")
	}

	parsedGenesisDomain, err := hex.DecodeString(strings.TrimPrefix(*ssvGenesisDomain, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode genesis network domain: %v", err)
	}

	parsedAlanDomain, err := hex.DecodeString(strings.TrimPrefix(*ssvAlanDomain, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode alan network domain: %v", err)
	}

	parsedDiscoveryProtocolID, err := hex.DecodeString(strings.TrimPrefix(*ssvDiscoveryProtocolID, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode discovery protocol ID: %v", err)
	}

	var parsedDiscoveryProtocolIDArr [6]byte
	if len(parsedDiscoveryProtocolID) != 0 {
		parsedDiscoveryProtocolIDArr = [6]byte(parsedDiscoveryProtocolID)
	}

	var bootnodes []string
	if *ssvBootnodes != "" {
		bootnodes = strings.Split(*ssvBootnodes, sliceSeparator)
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
		Name:                      *ssvNetworkName,
		GenesisDomainType:         spectypes.DomainType(parsedGenesisDomain),
		AlanDomainType:            spectypes.DomainType(parsedAlanDomain),
		RegistrySyncOffset:        new(big.Int).SetUint64(*ssvRegistrySyncOffset),
		RegistryContractAddr:      ethcommon.HexToAddress(*ssvRegistryContractAddr),
		Bootnodes:                 bootnodes,
		DiscoveryProtocolID:       parsedDiscoveryProtocolIDArr,
		AlanForkEpoch:             phase0.Epoch(*ssvAlanForkEpoch),
		MaxValidatorsPerCommittee: *ssvMaxValidatorsPerCommittee,
		TotalEthereumValidators:   *ssvTotalEthereumValidators,
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
