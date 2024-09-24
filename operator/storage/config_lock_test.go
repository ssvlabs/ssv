package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigLock(t *testing.T) {
	t.Run("same configs", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		require.NoError(t, c1.ValidateCompatibility(c2))
	})

	t.Run("all fields are different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test2",
			UsingLocalEvents: false,
			Version:          "v1.0.0-test",
		}

		require.Error(t, c1.ValidateCompatibility(c2))
	})

	t.Run("only network name is different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test2",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		require.Error(t, c1.ValidateCompatibility(c2))
	})

	t.Run("only local events usage is different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
			Version:          "v0.0.0-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.0.0-test",
		}

		require.Error(t, c1.ValidateCompatibility(c2))
	})

	t.Run("only version is different (possible upgrade)", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.0.2-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.0.3-test",
		}

		require.NoError(t, c1.ValidateCompatibility(c2))
	})

	t.Run("only version is different (possible downgrade)", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.0.2-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.0.1-test",
		}

		require.NoError(t, c1.ValidateCompatibility(c2))
	})

	t.Run("only version is different (impossible downgrade)", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v1.0.1-test",
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
			Version:          "v0.9.0-test",
		}

		require.Error(t, c1.ValidateCompatibility(c2))
	})
}
