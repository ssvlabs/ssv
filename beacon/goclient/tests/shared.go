package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/slotticker"
)

type MockDataStore struct {
	operatorID types.OperatorID
}

func (m MockDataStore) AwaitOperatorID() types.OperatorID {
	return m.operatorID
}

type requestCallback = func(r *http.Request, resp json.RawMessage) (json.RawMessage, error)

func MockServer(t *testing.T, onRequestFn requestCallback) *httptest.Server {
	var mockResponses map[string]json.RawMessage
	f, err := os.Open("./tests/mock-beacon-responses.json")
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(f).Decode(&mockResponses))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("mock server handling request: %s", r.URL.Path)

		resp, ok := mockResponses[r.URL.Path]
		if !ok {
			require.FailNowf(t, "unexpected request", "unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var err error
		if onRequestFn != nil {
			resp, err = onRequestFn(r, resp)
			require.NoError(t, err)
		}

		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(resp); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
}

func MockSlotTickerProvider() slotticker.SlotTicker {
	return slotticker.New(zap.NewNop(), slotticker.Config{
		SlotDuration: 12 * time.Second,
		GenesisTime:  time.Now(),
	})
}
