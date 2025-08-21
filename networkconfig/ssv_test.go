package networkconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"sort"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func TestSSVConfig_MarshalUnmarshalJSON(t *testing.T) {
	// Create a sample SSV config
	originalConfig := SSV{
		Name:                 "testnet",
		DomainType:           spectypes.DomainType{0x01, 0x02, 0x03, 0x04},
		RegistrySyncOffset:   big.NewInt(123456),
		RegistryContractAddr: ethcommon.HexToAddress("0x123456789abcdef0123456789abcdef012345678"),
		Bootnodes:            []string{"bootnode1", "bootnode2"},
		DiscoveryProtocolID:  [6]byte{0x05, 0x06, 0x07, 0x08, 0x09, 0x0a},
		Forks: SSVForks{
			Alan:            0,
			GasLimit36:      0,
			NetworkTopology: 0,
		},
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(&originalConfig)
	require.NoError(t, err)

	// Unmarshal from JSON
	var unmarshaledConfig SSV
	err = json.Unmarshal(jsonBytes, &unmarshaledConfig)
	require.NoError(t, err)

	// Marshal again after unmarshaling
	remarshaledBytes, err := json.Marshal(&unmarshaledConfig)
	require.NoError(t, err)

	// Compare the original and remarshaled JSON bytes
	assert.JSONEq(t, string(jsonBytes), string(remarshaledBytes))

	// Compare the original and unmarshaled structs
	assert.Equal(t, originalConfig.Name, unmarshaledConfig.Name)
	assert.Equal(t, originalConfig.DomainType, unmarshaledConfig.DomainType)
	assert.Equal(t, originalConfig.RegistrySyncOffset.Int64(), unmarshaledConfig.RegistrySyncOffset.Int64())
	assert.Equal(t, originalConfig.RegistryContractAddr, unmarshaledConfig.RegistryContractAddr)
	assert.Equal(t, originalConfig.Bootnodes, unmarshaledConfig.Bootnodes)
	assert.Equal(t, originalConfig.DiscoveryProtocolID, unmarshaledConfig.DiscoveryProtocolID)
	assert.Equal(t, originalConfig.TotalEthereumValidators, unmarshaledConfig.TotalEthereumValidators)
	assert.Equal(t, originalConfig.Forks, unmarshaledConfig.Forks)
}

func TestSSVConfig_MarshalUnmarshalYAML(t *testing.T) {
	// Create a sample SSV config
	originalConfig := SSV{
		Name:                 "testnet",
		DomainType:           spectypes.DomainType{0x01, 0x02, 0x03, 0x04},
		RegistrySyncOffset:   big.NewInt(123456),
		RegistryContractAddr: ethcommon.HexToAddress("0x123456789abcdef0123456789abcdef012345678"),
		Bootnodes:            []string{"bootnode1", "bootnode2"},
		DiscoveryProtocolID:  [6]byte{0x05, 0x06, 0x07, 0x08, 0x09, 0x0a},
		Forks: SSVForks{
			Alan:            0,
			GasLimit36:      0,
			NetworkTopology: 0,
		},
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(&originalConfig)
	require.NoError(t, err)

	// Unmarshal from YAML
	var unmarshaledConfig SSV
	err = yaml.Unmarshal(yamlBytes, &unmarshaledConfig)
	require.NoError(t, err)

	// Marshal again after unmarshaling
	remarshaledBytes, err := yaml.Marshal(&unmarshaledConfig)
	require.NoError(t, err)

	// Compare the original and unmarshaled structs
	assert.Equal(t, originalConfig.Name, unmarshaledConfig.Name)
	assert.Equal(t, originalConfig.DomainType, unmarshaledConfig.DomainType)
	assert.Equal(t, originalConfig.RegistrySyncOffset.Int64(), unmarshaledConfig.RegistrySyncOffset.Int64())
	assert.Equal(t, originalConfig.RegistryContractAddr, unmarshaledConfig.RegistryContractAddr)
	assert.Equal(t, originalConfig.Bootnodes, unmarshaledConfig.Bootnodes)
	assert.Equal(t, originalConfig.DiscoveryProtocolID, unmarshaledConfig.DiscoveryProtocolID)
	assert.Equal(t, originalConfig.TotalEthereumValidators, unmarshaledConfig.TotalEthereumValidators)
	assert.Equal(t, originalConfig.Forks, unmarshaledConfig.Forks)

	// Compare the original and remarshaled YAML bytes
	// YAML doesn't preserve order by default, so we need to compare the unmarshaled content
	var originalYAMLMap map[string]interface{}
	var remarshaledYAMLMap map[string]interface{}

	err = yaml.Unmarshal(yamlBytes, &originalYAMLMap)
	require.NoError(t, err)

	err = yaml.Unmarshal(remarshaledBytes, &remarshaledYAMLMap)
	require.NoError(t, err)

	assert.Equal(t, originalYAMLMap, remarshaledYAMLMap)
}

// hashStructJSON creates a deterministic hash of a struct by marshaling to sorted JSON
func hashStructJSON(v interface{}) (string, error) {
	// Create a JSON encoder that sorts map keys
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "")
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(v); err != nil {
		return "", err
	}

	// Compute SHA-256 hash
	hasher := sha256.New()
	hasher.Write(buffer.Bytes())
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// TestFieldPreservation ensures that all fields are properly preserved during
// marshal/unmarshal operations and that we can detect changes to the struct
func TestFieldPreservation(t *testing.T) {
	t.Run("test all fields are present after marshaling", func(t *testing.T) {
		// Get all field names from SSV config
		configType := reflect.TypeOf(SSV{})
		marshaledType := reflect.TypeOf(marshaledConfig{})

		var configFields, marshaledFields []string

		for i := 0; i < configType.NumField(); i++ {
			configFields = append(configFields, configType.Field(i).Name)
		}

		for i := 0; i < marshaledType.NumField(); i++ {
			marshaledFields = append(marshaledFields, marshaledType.Field(i).Name)
		}

		// Sort fields for deterministic comparison
		sort.Strings(configFields)
		sort.Strings(marshaledFields)

		// Ensure the same fields exist in both structs
		assert.Equal(t, configFields, marshaledFields, "SSV and marshaledConfig should have the same fields")
	})

	t.Run("hash comparison JSON", func(t *testing.T) {
		// Create a sample config
		config := SSV{
			Name:                 "testnet",
			DomainType:           spectypes.DomainType{0x01, 0x02, 0x03, 0x04},
			RegistrySyncOffset:   big.NewInt(123456),
			RegistryContractAddr: ethcommon.HexToAddress("0x123456789abcdef0123456789abcdef012345678"),
			Bootnodes:            []string{"bootnode1", "bootnode2"},
			DiscoveryProtocolID:  [6]byte{0x05, 0x06, 0x07, 0x08, 0x09, 0x0a},
			Forks: SSVForks{
				Alan:            0,
				GasLimit36:      0,
				NetworkTopology: 0,
			},
		}

		// Marshal and unmarshal to test preservation
		jsonBytes, err := json.Marshal(&config)
		require.NoError(t, err)

		var unmarshaled SSV
		err = json.Unmarshal(jsonBytes, &unmarshaled)
		require.NoError(t, err)

		// Hash the original and unmarshaled struct
		originalHash, err := hashStructJSON(&config)
		require.NoError(t, err)

		unmarshaledHash, err := hashStructJSON(&unmarshaled)
		require.NoError(t, err)

		// The hashes should match if all fields are preserved
		assert.Equal(t, originalHash, unmarshaledHash, "Hash mismatch indicates fields weren't properly preserved in JSON")

		// Store the expected hash - this will fail if a new field is added without updating the tests
		expectedJSONHash := "f54981560dec5aa10d052089dae6c5fec8e709b2629ad79fa8e08eabc1b55854"
		assert.Equal(t, expectedJSONHash, originalHash,
			"Hash has changed. If you've added a new field, please update the expected hash in this test.")
	})

	t.Run("hash comparison YAML", func(t *testing.T) {
		// Create a sample config
		config := SSV{
			Name:                 "testnet",
			DomainType:           spectypes.DomainType{0x01, 0x02, 0x03, 0x04},
			RegistrySyncOffset:   big.NewInt(123456),
			RegistryContractAddr: ethcommon.HexToAddress("0x123456789abcdef0123456789abcdef012345678"),
			Bootnodes:            []string{"bootnode1", "bootnode2"},
			DiscoveryProtocolID:  [6]byte{0x05, 0x06, 0x07, 0x08, 0x09, 0x0a},
			Forks: SSVForks{
				Alan:            0,
				GasLimit36:      0,
				NetworkTopology: 0,
			},
		}

		// Marshal and unmarshal to test preservation
		yamlBytes, err := yaml.Marshal(&config)
		require.NoError(t, err)

		var unmarshaled SSV
		err = yaml.Unmarshal(yamlBytes, &unmarshaled)
		require.NoError(t, err)

		// For YAML, convert to JSON for consistent hashing
		originalHash, err := hashStructJSON(&config)
		require.NoError(t, err)

		unmarshaledHash, err := hashStructJSON(&unmarshaled)
		require.NoError(t, err)

		// The hashes should match if all fields are preserved
		assert.Equal(t, originalHash, unmarshaledHash, "Hash mismatch indicates fields weren't properly preserved in YAML")
	})
}

// TestExistingNetworkConfigs validates that all predefined network configs
// can be marshaled and unmarshaled correctly
func TestExistingNetworkConfigs(t *testing.T) {
	for networkName, config := range supportedSSVConfigs {
		t.Run(networkName, func(t *testing.T) {
			// JSON test
			jsonBytes, err := json.Marshal(config)
			require.NoError(t, err)

			var jsonUnmarshaled SSV
			err = json.Unmarshal(jsonBytes, &jsonUnmarshaled)
			require.NoError(t, err)

			assert.Equal(t, config.DomainType, jsonUnmarshaled.DomainType)

			// YAML test
			yamlBytes, err := yaml.Marshal(config)
			require.NoError(t, err)

			var yamlUnmarshaled SSV
			err = yaml.Unmarshal(yamlBytes, &yamlUnmarshaled)
			require.NoError(t, err)

			assert.Equal(t, config.DomainType, yamlUnmarshaled.DomainType)
		})
	}
}
