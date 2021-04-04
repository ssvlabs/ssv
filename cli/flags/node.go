package flags

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/spf13/cobra"
	"time"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	nodeIDKeyFlag        = "node-id"
	validatorKeyFlag     = "validator-key"
	beaconAddrFlag       = "beacon-node-addr"
	networkFlag          = "network"
	consensusFlag        = "val"
	sigCollectionTimeout = "sig-collection-timeout"
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

// AddConsensusFlag adds the val flag to the command
func AddConsensusFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, consensusFlag, "validation", "The val type", false)
}

// GetConsensusFlagValue gets the val flag from the command
func GetConsensusFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(consensusFlag)
}

// AddNodeIDKeyFlag adds the node ID flag to the command
func AddSignatureCollectionTimeFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, sigCollectionTimeout, 5, "Timeout after consensus for signature collection", false)
}

// GetNodeIDKeyFlagValue gets the node ID flag from the command
func GetSignatureCollectionTimeValue(c *cobra.Command) (time.Duration, error) {
	v, err := c.Flags().GetUint64(sigCollectionTimeout)
	if err != nil {
		return 0, err
	}
	return time.Second * time.Duration(v), nil
}
