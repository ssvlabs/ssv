package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/slotticker"
)

type MockDataStore struct {
	operatorID types.OperatorID
}

func (m MockDataStore) AwaitOperatorID() types.OperatorID {
	return m.operatorID
}

type requestCallback = func(r *http.Request, resp json.RawMessage) (json.RawMessage, error)

func MockServer(onRequestFn requestCallback) *httptest.Server {
	var mockResponses map[string]json.RawMessage
	f, err := os.Open("./tests/mock-beacon-responses.json")
	if err != nil {
		panic(fmt.Sprintf("os.Open returned error: %v", err))
	}
	err = json.NewDecoder(f).Decode(&mockResponses)
	if err != nil {
		panic(fmt.Sprintf("couldn't decode json file: %v", err))
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := mockResponses[r.URL.Path]
		if !ok {
			panic(fmt.Sprintf("unexpected request: %s", r.URL.Path))
		}

		var err error
		if onRequestFn != nil {
			resp, err = onRequestFn(r, resp)
			if err != nil {
				panic(fmt.Sprintf("onRequestFn returned error: %v", err))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(resp); err != nil {
			panic(fmt.Sprintf("got error writing response: %v", err))
		}
	}))
}

func MockSlotTickerProvider() slotticker.SlotTicker {
	return slotticker.New(zap.NewNop(), slotticker.Config{
		SlotDuration: 12 * time.Second,
		GenesisTime:  time.Now(),
	})
}
