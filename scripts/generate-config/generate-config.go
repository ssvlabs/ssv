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
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v2"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
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
	networkDomain := flag.String("network-domain", "0x"+hex.EncodeToString(defaultNetwork.Domain[:]), "Network domain")
	networkGenesisEpoch := flag.Uint64("network-genesis-epoch", uint64(defaultNetwork.GenesisEpoch), "Network genesis epoch")
	networkRegistrySyncOffset := flag.Uint64("network-registry-sync-offset", defaultNetwork.RegistrySyncOffset.Uint64(), "Network registry sync offset")
	networkRegistryContractAddr := flag.String("network-registry-contract-addr", defaultNetwork.RegistryContractAddr.String(), "Network registry contract addr")
	networkBootnodes := flag.String("network-bootnodes", strings.Join(defaultNetwork.Bootnodes, sliceSeparator), "Network bootnodes (comma-separated)")
	networkWhitelistedOperatorKeys := flag.String("network-whitelisted-operator-keys", strings.Join(defaultNetwork.WhitelistedOperatorKeys, sliceSeparator), "Network whitelisted operator keys (comma-separated)")
	networkPermissionlessActivationEpoch := flag.Uint64("network-permissionless-activation-epoch", uint64(defaultNetwork.PermissionlessActivationEpoch), "Network permissionless activation epoch")

	beaconNetworkParent := flag.String("beacon-network-parent", string(defaultNetwork.Beacon.(*beacon.Network).Parent), "Beacon network parent")
	beaconNetworkName := flag.String("beacon-network-name", defaultNetwork.Beacon.(*beacon.Network).Name, "Beacon network name")

	beaconNetworkForkVersion := flag.String("beacon-network-fork-version", "0x"+hex.EncodeToString(defaultNetwork.Beacon.(*beacon.Network).ForkVersionVal[:]), "Beacon network fork version")
	beaconNetworkMinGenesisTime := flag.Uint64("beacon-network-min-genesis-time", defaultNetwork.Beacon.(*beacon.Network).MinGenesisTimeVal, "Beacon network min genesis time")
	beaconNetworkSlotDuration := flag.Int64("beacon-network-slot-duration", int64(defaultNetwork.Beacon.(*beacon.Network).SlotDurationVal), "Beacon network slot duration")
	beaconNetworkSlotsPerEpoch := flag.Uint64("beacon-network-slots-per-epoch", defaultNetwork.Beacon.(*beacon.Network).SlotsPerEpochVal, "Beacon network slots per epoch")
	beaconNetworkEpochsPerSyncCommitteePeriod := flag.Uint64("beacon-network-epochs-per-sync-committee-period", defaultNetwork.Beacon.(*beacon.Network).EpochsPerSyncCommitteePeriodVal, "Beacon network epochs per sync committee period")

	flag.Parse()

	if *consensusClient == "" {
		log.Fatalf("The --consensus-client flag is mandatory")
	}

	if *executionClient == "" {
		log.Fatalf("The --execution-client flag is mandatory")
	}

	parsedDomain, err := hex.DecodeString(strings.TrimPrefix(*networkDomain, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode network domain: %v", err)
	}

	parsedBeaconNetworkForkVersion, err := hex.DecodeString(strings.TrimPrefix(*beaconNetworkForkVersion, "0x"))
	if err != nil {
		log.Fatalf("Failed to decode beacon network fork version: %v", err)
	}

	var parsedBeaconNetworkForkVersionArr [4]byte
	if len(parsedBeaconNetworkForkVersion) != 0 {
		parsedBeaconNetworkForkVersionArr = [4]byte(parsedBeaconNetworkForkVersion)
	}

	var bootnodes []string
	if *networkBootnodes != "" {
		bootnodes = strings.Split(*networkBootnodes, sliceSeparator)
	}

	var whitelistedOperators []string
	if *networkWhitelistedOperatorKeys != "" {
		whitelistedOperators = strings.Split(*networkWhitelistedOperatorKeys, sliceSeparator)
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
		Beacon: &beacon.Network{
			Parent:                          spectypes.BeaconNetwork(*beaconNetworkParent),
			Name:                            *beaconNetworkName,
			ForkVersionVal:                  parsedBeaconNetworkForkVersionArr,
			MinGenesisTimeVal:               *beaconNetworkMinGenesisTime,
			SlotDurationVal:                 time.Duration(*beaconNetworkSlotDuration),
			SlotsPerEpochVal:                *beaconNetworkSlotsPerEpoch,
			EpochsPerSyncCommitteePeriodVal: *beaconNetworkEpochsPerSyncCommitteePeriod,
		},
		Domain:                        spectypes.DomainType(parsedDomain),
		GenesisEpoch:                  phase0.Epoch(*networkGenesisEpoch),
		RegistrySyncOffset:            new(big.Int).SetUint64(*networkRegistrySyncOffset),
		RegistryContractAddr:          ethcommon.HexToAddress(*networkRegistryContractAddr),
		Bootnodes:                     bootnodes,
		WhitelistedOperatorKeys:       whitelistedOperators,
		PermissionlessActivationEpoch: phase0.Epoch(*networkPermissionlessActivationEpoch),
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
