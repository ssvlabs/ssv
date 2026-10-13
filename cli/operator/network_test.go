package operator

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

func newTestSSVSignerClient(t *testing.T, register func(mux *http.ServeMux)) *ssvsigner.Client {
	mux := http.NewServeMux()
	if register != nil {
		register(mux)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return ssvsigner.NewClient(server.URL, ssvsigner.WithLogger(zap.NewNop()))
}

func Test_decideNetworkKeyProtectors(t *testing.T) {
	t.Run("exporter ignores operator key", func(t *testing.T) {
		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		protectFn, unprotectFn, err := decideNetworkKeyProtectors(context.Background(), zap.NewNop(), nil, true, operatorPrivKey, nil)
		require.NoError(t, err)
		require.Nil(t, protectFn)
		require.Nil(t, unprotectFn)
	})

	t.Run("exporter ignores ssv-signer config", func(t *testing.T) {
		client := newTestSSVSignerClient(t, nil)

		protectFn, unprotectFn, err := decideNetworkKeyProtectors(context.Background(), zap.NewNop(), nil, true, nil, client)
		require.NoError(t, err)
		require.Nil(t, protectFn)
		require.Nil(t, unprotectFn)
	})

	t.Run("non-exporter keeps local operator key", func(t *testing.T) {
		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		protectFn, unprotectFn, err := decideNetworkKeyProtectors(context.Background(), zap.NewNop(), nil, false, operatorPrivKey, nil)
		require.NoError(t, err)
		require.NotNil(t, protectFn)
		require.NotNil(t, unprotectFn)

		plaintext := []byte("local-protected-payload")
		protectedValue, err := protectFn(context.Background(), plaintext)
		require.NoError(t, err)
		require.NotEqual(t, plaintext, protectedValue)

		decryptedValue, err := unprotectFn(context.Background(), protectedValue)
		require.NoError(t, err)
		require.Equal(t, plaintext, decryptedValue)
	})

	t.Run("non-exporter keeps ssv-signer client", func(t *testing.T) {
		probeClient := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(append([]byte("encrypted:"), payload...))
				require.NoError(t, err)
			})
			mux.HandleFunc(ssvsigner.PathOperatorDecrypt, func(w http.ResponseWriter, r *http.Request) {
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(bytes.TrimPrefix(payload, []byte("encrypted:")))
				require.NoError(t, err)
			})
		})

		protectFn, unprotectFn, err := decideNetworkKeyProtectors(context.Background(), zap.NewNop(), nil, false, nil, probeClient)
		require.NoError(t, err)
		require.NotNil(t, protectFn)
		require.NotNil(t, unprotectFn)

		plaintext := []byte("remote-protected-payload")
		protectedValue, err := protectFn(context.Background(), plaintext)
		require.NoError(t, err)
		require.Equal(t, []byte("encrypted:"+string(plaintext)), protectedValue)

		decryptedValue, err := unprotectFn(context.Background(), protectedValue)
		require.NoError(t, err)
		require.Equal(t, plaintext, decryptedValue)
	})
}

func Test_probeRemoteNetworkKeyProtector(t *testing.T) {
	t.Run("uses remote signer encrypt and decrypt endpoints when supported", func(t *testing.T) {
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(append([]byte("encrypted:"), payload...))
				require.NoError(t, err)
			})
			mux.HandleFunc(ssvsigner.PathOperatorDecrypt, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(bytes.TrimPrefix(payload, []byte("encrypted:")))
				require.NoError(t, err)
			})
		})

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.NoError(t, err)
	})

	t.Run("returns unsupported when remote signer does not support remote data protection", func(t *testing.T) {
		client := newTestSSVSignerClient(t, nil)

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.ErrorIs(t, err, ssvsigner.ErrOperatorDataProtectionUnsupported)
	})

	t.Run("fails instead of downgrading on transient remote signer fetch error", func(t *testing.T) {
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "temporary upstream failure", http.StatusInternalServerError)
			})
		})

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.ErrorContains(t, err, "probe remote data protector encrypt")
		require.ErrorContains(t, err, "unexpected status: 500")
	})
}
