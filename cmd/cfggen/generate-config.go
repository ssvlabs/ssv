package main

import (
	"encoding/hex"
	"flag"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"gopkg.in/yaml.v2"

	"github.com/ssvlabs/ssv/networkconfig"
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
	defaultNetwork = networkconfig.LocalTestnet
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
	networkGenesisEpoch := flag.Uint64("network-genesis-epoch", uint64(defaultNetwork.GenesisEpoch), "Network genesis epoch")
	networkRegistrySyncOffset := flag.Uint64("network-registry-sync-offset", defaultNetwork.RegistrySyncOffset.Uint64(), "Network registry sync offset")
	networkRegistryContractAddr := flag.String("network-registry-contract-addr", defaultNetwork.RegistryContractAddr.String(), "Network registry contract addr")
	networkBootnodes := flag.String("network-bootnodes", strings.Join(defaultNetwork.Bootnodes, sliceSeparator), "Network bootnodes (comma-separated)")
	networkDiscoveryProtocolID := flag.String("network-discovery-protocol-id", "0x"+hex.EncodeToString(defaultNetwork.DiscoveryProtocolID[:]), "Network discovery protocol ID")
	networkAlanForkEpoch := flag.Uint64("network-alan-fork-epoch", uint64(defaultNetwork.AlanForkEpoch), "Network Alan fork epoch")

	beaconNetworkGenesisForkVersion := flag.String("beacon-network-genesis-fork-version", "0x"+hex.EncodeToString(defaultNetwork.BeaconConfig.GenesisForkVersionVal[:]), "Beacon network genesis fork version")
	beaconNetworkCapellaForkVersion := flag.String("beacon-network-capella-fork-version", "0x"+hex.EncodeToString(defaultNetwork.BeaconConfig.CapellaForkVersionVal[:]), "Beacon network capella fork version")
	beaconNetworkMinGenesisTime := flag.Int64("beacon-network-min-genesis-time", defaultNetwork.BeaconConfig.MinGenesisTimeVal.Unix(), "Beacon network min genesis time")
	beaconNetworkSlotDuration := flag.Int64("beacon-network-slot-duration", int64(defaultNetwork.BeaconConfig.SlotDurationVal), "Beacon network slot duration")
	beaconNetworkSlotsPerEpoch := flag.Uint64("beacon-network-slots-per-epoch", uint64(defaultNetwork.BeaconConfig.SlotsPerEpochVal), "Beacon network slots per epoch")
	beaconNetworkEpochsPerSyncCommitteePeriod := flag.Uint64("beacon-network-epochs-per-sync-committee-period", uint64(defaultNetwork.BeaconConfig.EpochsPerSyncCommitteePeriodVal), "Beacon network epochs per sync committee period")

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

	parsedBeaconNetworkGenesisForkVersion, err := hex.DecodeString(strings.TrimPrefix(*beaconNetworkGenesisForkVersion, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode beacon network genesis fork version: %v", err)
	}

	var parsedBeaconNetworkGenesisForkVersionArr [4]byte
	if len(parsedBeaconNetworkGenesisForkVersion) != 0 {
		parsedBeaconNetworkGenesisForkVersionArr = [4]byte(parsedBeaconNetworkGenesisForkVersion)
	}

	parsedBeaconNetworkCapellaForkVersion, err := hex.DecodeString(strings.TrimPrefix(*beaconNetworkCapellaForkVersion, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode beacon network capella fork version: %v", err)
	}

	var parsedBeaconNetworkCapellaForkVersionArr [4]byte
	if len(parsedBeaconNetworkCapellaForkVersion) != 0 {
		parsedBeaconNetworkCapellaForkVersionArr = [4]byte(parsedBeaconNetworkCapellaForkVersion)
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
	config.SSV.CustomNetwork = &networkconfig.NetworkConfig{
		Name: *networkName,
		BeaconConfig: networkconfig.BeaconConfig{
			GenesisForkVersionVal:           parsedBeaconNetworkGenesisForkVersionArr,
			CapellaForkVersionVal:           parsedBeaconNetworkCapellaForkVersionArr,
			MinGenesisTimeVal:               time.Unix(*beaconNetworkMinGenesisTime, 0),
			SlotDurationVal:                 time.Duration(*beaconNetworkSlotDuration),
			SlotsPerEpochVal:                phase0.Slot(*beaconNetworkSlotsPerEpoch),
			EpochsPerSyncCommitteePeriodVal: phase0.Epoch(*beaconNetworkEpochsPerSyncCommitteePeriod),
		},
		GenesisDomainType:    spectypes.DomainType(parsedGenesisDomain),
		AlanDomainType:       spectypes.DomainType(parsedAlanDomain),
		GenesisEpoch:         phase0.Epoch(*networkGenesisEpoch),
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
