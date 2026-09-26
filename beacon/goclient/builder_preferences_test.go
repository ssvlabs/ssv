package goclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestSubmitBuilderPreferences(t *testing.T) {
	prefs := []*gloas.BuilderPreferencesEntry{{
		ProposerPubKey:      phase0.BLSPubKey{0xab},
		URL:                 "https://builder.example.com",
		Auth:                &gloas.SignedBuilderRequestAuth{Message: &gloas.BuilderRequestAuth{Data: []byte{0x01}, Slot: 9}},
		MaxExecutionPayment: 250,
	}}

	var gotMethod, gotPath, gotVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, submitBuilderPreferences(context.Background(), srv.Client(), srv.URL, prefs))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/validator/builder_preferences", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	require.Contains(t, string(gotBody), `"max_execution_payment":"250"`) // uint64 as a decimal string
	require.Contains(t, string(gotBody), `"proposer_pubkey":"0xab00`)     // pubkey as 0x-hex
	want, err := json.Marshal(prefs)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(gotBody))
}

// A 404 — a beacon node predating the merged beacon-APIs#630 endpoint — is flagged as a missing endpoint
// rather than a transient failure.
func TestSubmitBuilderPreferencesMissingRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":404,"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	err := submitBuilderPreferences(context.Background(), srv.Client(), srv.URL, nil)
	require.ErrorContains(t, err, "beacon node lacks the gloas builder_preferences endpoint")
	require.ErrorContains(t, err, "status 404")
}
