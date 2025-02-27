package validator

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
)

// createTestValidatorPK creates a test ValidatorPK with the specified input byte
func createTestValidatorPK(input byte) spectypes.ValidatorPK {
	var byteArray [48]byte
	for i := range byteArray {
		byteArray[i] = input
	}
	return byteArray
}

func TestTypedSyncMap_Store(t *testing.T) {
	tests := []struct {
		name           string
		setupMap       func(*TypedSyncMap[spectypes.ValidatorPK, string])
		key            spectypes.ValidatorPK
		value          string
		wantAfterStore string
	}{
		{
			name:           "store new key-value pair",
			setupMap:       func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {},
			key:            createTestValidatorPK(1),
			value:          "validator-1",
			wantAfterStore: "validator-1",
		},
		{
			name: "override existing key-value pair",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(2)
				m.Map.Store(key, "old-validator")
			},
			key:            createTestValidatorPK(2),
			value:          "updated-validator",
			wantAfterStore: "updated-validator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
			tt.setupMap(m)

			// Store the key-value pair
			m.Store(tt.key, tt.value)

			// Verify the state after Store
			loadedValue, exists := m.Load(tt.key)
			assert.True(t, exists)
			assert.Equal(t, tt.wantAfterStore, loadedValue)
		})
	}
}

func TestTypedSyncMap_Load(t *testing.T) {
	tests := []struct {
		name       string
		setupMap   func(*TypedSyncMap[spectypes.ValidatorPK, string])
		key        spectypes.ValidatorPK
		wantValue  string
		wantExists bool
	}{
		{
			name: "existing key",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(1)
				m.Map.Store(key, "validator-1")
			},
			key:        createTestValidatorPK(1),
			wantValue:  "validator-1",
			wantExists: true,
		},
		{
			name:       "non-existing key",
			setupMap:   func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {},
			key:        createTestValidatorPK(2),
			wantValue:  "", // zero value for string
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
			tt.setupMap(m)

			gotValue, gotExists := m.Load(tt.key)
			assert.Equal(t, tt.wantExists, gotExists)
			if gotExists {
				assert.Equal(t, tt.wantValue, gotValue)
			}
		})
	}
}

func TestTypedSyncMap_LoadOrStore(t *testing.T) {
	tests := []struct {
		name             string
		setupMap         func(*TypedSyncMap[spectypes.ValidatorPK, string])
		key              spectypes.ValidatorPK
		valueToStore     string
		wantReturnValue  string
		wantLoaded       bool
		wantAfterMapLoad string
	}{
		{
			name: "existing key",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(1)
				m.Map.Store(key, "existing-validator")
			},
			key:              createTestValidatorPK(1),
			valueToStore:     "new-validator",
			wantReturnValue:  "existing-validator",
			wantLoaded:       true,
			wantAfterMapLoad: "existing-validator", // value should not change
		},
		{
			name:             "non-existing key",
			setupMap:         func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {},
			key:              createTestValidatorPK(2),
			valueToStore:     "new-validator",
			wantReturnValue:  "new-validator",
			wantLoaded:       false,
			wantAfterMapLoad: "new-validator", // new value should be stored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
			tt.setupMap(m)

			gotValue, gotLoaded := m.LoadOrStore(tt.key, tt.valueToStore)
			assert.Equal(t, tt.wantLoaded, gotLoaded)
			assert.Equal(t, tt.wantReturnValue, gotValue)

			// Verify the state after LoadOrStore
			loadedValue, exists := m.Load(tt.key)
			assert.True(t, exists)
			assert.Equal(t, tt.wantAfterMapLoad, loadedValue)
		})
	}
}

func TestTypedSyncMap_Delete(t *testing.T) {
	tests := []struct {
		name         string
		setupMap     func(*TypedSyncMap[spectypes.ValidatorPK, string])
		keyToDelete  spectypes.ValidatorPK
		expectExists bool
	}{
		{
			name: "delete existing key",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(1)
				m.Store(key, "validator-1")
			},
			keyToDelete:  createTestValidatorPK(1),
			expectExists: false, // should be deleted
		},
		{
			name: "delete non-existing key",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(1)
				m.Store(key, "validator-1")
			},
			keyToDelete:  createTestValidatorPK(2), // different key
			expectExists: false,                    // doesn't exist already
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
			tt.setupMap(m)

			// Delete the key
			m.Delete(tt.keyToDelete)

			// Check if the key still exists
			_, exists := m.Load(tt.keyToDelete)
			assert.Equal(t, tt.expectExists, exists)
		})
	}
}

func TestTypedSyncMap_Range(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
		called := false

		m.Range(func(key spectypes.ValidatorPK, value string) bool {
			called = true
			return true
		})

		assert.False(t, called, "Range should not call the function on an empty map")
	})

	t.Run("iterate through all items", func(t *testing.T) {
		m := NewTypedSyncMap[spectypes.ValidatorPK, string]()

		// Setup test data
		key1 := createTestValidatorPK(1)
		key2 := createTestValidatorPK(2)
		key3 := createTestValidatorPK(3)

		m.Store(key1, "validator-1")
		m.Store(key2, "validator-2")
		m.Store(key3, "validator-3")

		// Create a map to track visited keys
		visitedKeys := make(map[spectypes.ValidatorPK]bool)
		visitedValues := make(map[string]bool)

		m.Range(func(key spectypes.ValidatorPK, value string) bool {
			visitedKeys[key] = true
			visitedValues[value] = true
			return true
		})

		// We should have visited all keys and values
		assert.Len(t, visitedKeys, 3)
		assert.Len(t, visitedValues, 3)

		assert.True(t, visitedKeys[key1])
		assert.True(t, visitedKeys[key2])
		assert.True(t, visitedKeys[key3])

		assert.True(t, visitedValues["validator-1"])
		assert.True(t, visitedValues["validator-2"])
		assert.True(t, visitedValues["validator-3"])
	})

	t.Run("early termination", func(t *testing.T) {
		m := NewTypedSyncMap[spectypes.ValidatorPK, string]()

		// Setup test data
		key1 := createTestValidatorPK(1)
		key2 := createTestValidatorPK(2)
		key3 := createTestValidatorPK(3)

		m.Store(key1, "validator-1")
		m.Store(key2, "validator-2")
		m.Store(key3, "validator-3")

		count := 0

		m.Range(func(key spectypes.ValidatorPK, value string) bool {
			count++
			return count < 2 // stop after visiting 2 items
		})

		assert.Equal(t, 2, count, "Range should have stopped after 2 items")
	})
}

func TestTypedSyncMap_Has(t *testing.T) {
	tests := []struct {
		name      string
		setupMap  func(*TypedSyncMap[spectypes.ValidatorPK, string])
		key       spectypes.ValidatorPK
		wantExist bool
	}{
		{
			name: "has existing key",
			setupMap: func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {
				key := createTestValidatorPK(1)
				m.Store(key, "validator-1")
			},
			key:       createTestValidatorPK(1),
			wantExist: true,
		},
		{
			name:      "doesn't have non-existing key",
			setupMap:  func(m *TypedSyncMap[spectypes.ValidatorPK, string]) {},
			key:       createTestValidatorPK(2),
			wantExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTypedSyncMap[spectypes.ValidatorPK, string]()
			tt.setupMap(m)

			exists := m.Has(tt.key)
			assert.Equal(t, tt.wantExist, exists)
		})
	}
}
