package goclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGloasPublishSSZ_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad block"))
	}))
	defer srv.Close()

	err := gloasPublishSSZ(context.Background(), srv.URL, []byte{0x01}, nil)
	require.ErrorContains(t, err, "status 400")
}
