package pubsub

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPubSub(t *testing.T) {
	t.Run("Successfully Register", func(t *testing.T) {
		s := struct {
			BaseSubject
			Log  types.Log
			Data interface{}
		}{}

		o1 := BaseObserver{
			ID: "BaseObserver1",
		}

		s.Register(o1)
		require.Equal(t, 1, len(s.ObserverList))

		o2 := BaseObserver{
			ID: "BaseObserver2",
		}

		s.Register(o2)
		require.Equal(t, 2, len(s.ObserverList))

		s.Deregister(o1)
		require.NotEqual(t, 2, len(s.ObserverList))
	})
}
