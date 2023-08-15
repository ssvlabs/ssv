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
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
		}

		require.NoError(t, c1.EnsureSameWith(c2))
	})

	t.Run("all fields are different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
		}

		c2 := &ConfigLock{
			NetworkName:      "test2",
			UsingLocalEvents: false,
		}

		require.Error(t, c1.EnsureSameWith(c2))
	})

	t.Run("only network name is different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
		}

		c2 := &ConfigLock{
			NetworkName:      "test2",
			UsingLocalEvents: true,
		}

		require.Error(t, c1.EnsureSameWith(c2))
	})

	t.Run("only local events usage is different", func(t *testing.T) {
		c1 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: true,
		}

		c2 := &ConfigLock{
			NetworkName:      "test",
			UsingLocalEvents: false,
		}

		require.Error(t, c1.EnsureSameWith(c2))
	})
}
