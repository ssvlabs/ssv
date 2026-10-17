package kms

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailover(t *testing.T) {
	var limited, backup atomic.Int32
	hosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limited.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer hosted.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backup.Add(1)
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}))
	defer public.Close()

	c := &http.Client{Transport: &failover{urls: []string{hosted.URL, public.URL}, next: http.DefaultTransport}}
	resp, err := c.Post(hosted.URL, "application/json", strings.NewReader(`{"id":1}`))
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `{"id":1}`, string(b))
	require.EqualValues(t, 2, limited.Load()) // tried twice before moving on
	require.EqualValues(t, 1, backup.Load())

	require.True(t, hostedRPC("https://mainnet.infura.io/v3/x"))
	require.False(t, hostedRPC("http://anvil:8545"))
}
