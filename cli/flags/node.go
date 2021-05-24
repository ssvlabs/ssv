package flags

import (
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	nodeIDKeyFlag        = "node-id"
	validatorKeyFlag     = "validator-key"
	beaconAddrFlag       = "beacon-node-addr"
	networkFlag          = "network"
	discoveryFlag        = "discovery-type"
	consensusFlag        = "val"
	sigCollectionTimeout = "sig-collection-timeout"
	dutySlotsLimit       = "duty-slots-limit"
	hostDNS              = "host-dns"
	hostAddress          = "host-address"
	tcpPort              = "tcp-port"
	udpPort              = "udp-port"
	genesisEpoch         = "genesis-epoch"
	loggerLevel          = "logger-level"
	eth1AddrFlag         = "eth1-addr"
	storagePath          = "storage-path"
	operatorPrivateFlag  = "operator-private-key"
	maxNetBatch          = "network-max-batch"
	netReqTimeout        = "network-req-timeout"
)

// AddNodeIDKeyFlag adds the node ID flag to the command
func AddNodeIDKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, nodeIDKeyFlag, 0, "SSV node ID", true)
}

// GetNodeIDKeyFlagValue gets the node ID flag from the command
func GetNodeIDKeyFlagValue(c *cobra.Command) (uint64, error) {
	return c.Flags().GetUint64(nodeIDKeyFlag)
}

// AddValidatorKeyFlag adds the validator key flag to the command
func AddValidatorKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, validatorKeyFlag, "", "Hex encoded public key of the validator", true)
}

// GetValidatorKeyFlagValue gets the validator key flag from the command
func GetValidatorKeyFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(validatorKeyFlag)
}

// AddBeaconAddrFlag adds the beacon address flag to the command
func AddBeaconAddrFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, beaconAddrFlag, "", "The address of the beacon node", true)
}

// GetBeaconAddrFlagValue gets the beacon address flag from the command
func GetBeaconAddrFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(beaconAddrFlag)
}

// AddNetworkFlag adds the network flag to the command
func AddNetworkFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, networkFlag, "", "The Ethereum network", true)
}

// GetNetworkFlagValue gets the network flag from the command
func GetNetworkFlagValue(c *cobra.Command) (core.Network, error) {
	network, err := c.Flags().GetString(networkFlag)
	if err != nil {
		return "", err
	}

	return core.NetworkFromString(network), nil
}

// AddDiscoveryFlag adds the discovery flag to the command
func AddDiscoveryFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, discoveryFlag, "discv5", "The discovery type [mdns, discv5]", false)
}

// GetDiscoveryFlagValue gets the discovery flag from the command
func GetDiscoveryFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(discoveryFlag)
}

// AddConsensusFlag adds the val flag to the command
func AddConsensusFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, consensusFlag, "validation", "The val type", false)
}

// GetConsensusFlagValue gets the val flag from the command
func GetConsensusFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(consensusFlag)
}

// AddSignatureCollectionTimeFlag adds the timeout for signatures flag to the command
func AddSignatureCollectionTimeFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, sigCollectionTimeout, 5, "Timeout after consensus for signature collection", false)
}

// GetSignatureCollectionTimeValue gets the max timeout to wait for signatures flag from the command
func GetSignatureCollectionTimeValue(c *cobra.Command) (time.Duration, error) {
	v, err := c.Flags().GetUint64(sigCollectionTimeout)
	if err != nil {
		return 0, err
	}
	return time.Second * time.Duration(v), nil
}

// AddDutySlotsLimit adds the max amount of slots to execute duty in delay flag to the command
func AddDutySlotsLimit(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, dutySlotsLimit, 32, "Duties max slots delay to attest", false)
}

// GetDutySlotsLimitValue gets the max amount of slots to execute duty in delay flag to  the command
func GetDutySlotsLimitValue(c *cobra.Command) (uint64, error) {
	return c.Flags().GetUint64(dutySlotsLimit)
}

// GetHostDNSFlagValue gets the host dns flag from the command
func GetHostDNSFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(hostDNS)
}

// AddHostDNSFlag adds the host dns flag to the command
func AddHostDNSFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, hostDNS, "", "host dns", false)
}

// GetHostAddressFlagValue gets the host address flag from the command
func GetHostAddressFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(hostAddress)
}

// AddHostAddressFlag adds the host address flag to the command
func AddHostAddressFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, hostAddress, "", "host address", false)
}

// GetTCPPortFlagValue gets the tcp port flag from the command
func GetTCPPortFlagValue(c *cobra.Command) (int, error) {
	val, err := c.Flags().GetUint64(tcpPort)
	return int(val), err
}

// AddTCPPortFlag adds the tcp port flag to the command
func AddTCPPortFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, tcpPort, 13000, "tcp port number", false)
}

// GetUDPPortFlagValue gets the udp port flag from the command
func GetUDPPortFlagValue(c *cobra.Command) (int, error) {
	val, err := c.Flags().GetUint64(udpPort)
	return int(val), err
}

// AddUDPPortFlag adds the udp port flag to the command
func AddUDPPortFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, udpPort, 12000, "udp port number", false)
}

// GetGenesisEpochValue gets the genesis epoch flag from the command
func GetGenesisEpochValue(c *cobra.Command) (uint64, error) {
	val, err := c.Flags().GetUint64(genesisEpoch)
	return val, err
}

// AddGenesisEpochFlag adds the udp port flag to the command
func AddGenesisEpochFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, genesisEpoch, 34500, "genesis epoch number", false)
}

// GetLoggerLevelValue gets the logger level flag from the command
func GetLoggerLevelValue(c *cobra.Command) (zapcore.Level, error) {
	val, err := c.Flags().GetString(loggerLevel)
	switch val := val; val {
	case "debug":
		return zapcore.DebugLevel, err
	case "info":
		return zapcore.InfoLevel, err
	case "warn":
		return zapcore.WarnLevel, err
	case "error":
		return zapcore.ErrorLevel, err
	case "dpanic":
		return zapcore.DPanicLevel, err
	case "panic":
		return zapcore.PanicLevel, err
	case "fatal":
		return zapcore.FatalLevel, err
	}
	return zapcore.InfoLevel, err
}

// AddLoggerLevelFlag adds the logger level flag to the command
func AddLoggerLevelFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, loggerLevel, "info", "logger level", false)
}

// AddEth1AddrFlag adds the eth1 address flag to the command
func AddEth1AddrFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, eth1AddrFlag, "", "eth1 node address", false)
}

// GetEth1AddrValue gets the eth1 address flag from the command
func GetEth1AddrValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(eth1AddrFlag)
}

// AddOperatorPrivateKeyFlag adds the operator private key flag to the command
func AddOperatorPrivateKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, operatorPrivateFlag, "", "operator private key", false)
}

// GetOperatorPrivateKeyFlag gets the operator private key flag from the command
func GetOperatorPrivateKeyFlag(c *cobra.Command) (string, error) {
	return c.Flags().GetString(operatorPrivateFlag)
}

// GetStoragePathValue gets the storage path flag from the command
func GetStoragePathValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(storagePath)
}

// AddStoragePathFlag adds the storage path flag to the command
func AddStoragePathFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, storagePath, "./data/db", "storage path", false)
}

// GetMaxNetworkResponseBatchValue gets the max network response batch flag from the command
func GetMaxNetworkResponseBatchValue(c *cobra.Command) (uint64, error) {
	val, err := c.Flags().GetUint64(maxNetBatch)
	return val, err
}

// AddMaxNetworkResponseBatchFlag adds the max network response batch flag to the command
func AddMaxNetworkResponseBatchFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, maxNetBatch, 25, "maximum number of batched return objects in a network response", false)
}

// GetNetworkRequestTimeoutValue gets the network request timeout (in seconds) flag from the command
func GetNetworkRequestTimeoutValue(c *cobra.Command) (uint64, error) {
	val, err := c.Flags().GetUint64(netReqTimeout)
	return val, err
}

// AddNetworkRequestTimeoutFlag adds the network request timeout (in seconds) flag to the command
func AddNetworkRequestTimeoutFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, netReqTimeout, 5, "P2P network request timeout (in seconds)", false)
}
