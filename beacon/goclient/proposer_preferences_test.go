package goclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the proposer-preferences beacon-node surface.
var _ beacon.ProposerPreferencesCalls = (*GoClient)(nil)

func TestSubmitProposerPreferences(t *testing.T) {
	prefs := []*gloas.SignedProposerPreferences{{
		Message: &gloas.ProposerPreferences{
			DependentRoot:  phase0.Root{0xaa},
			ProposalSlot:   9,
			ValidatorIndex: 7,
			FeeRecipient:   bellatrix.ExecutionAddress{0xcc},
			TargetGasLimit: 36_000_000,
		},
		Signature: phase0.BLSSignature{0xbb},
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

	require.NoError(t, submitProposerPreferences(context.Background(), srv.Client(), srv.URL, prefs))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/validator/proposer_preferences", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	want, err := json.Marshal(prefs)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(gotBody))
}

// A 404 — a BN build without the route (predating the merged beacon-APIs#608 endpoint) — is
// flagged as a missing endpoint rather than a transient failure.
func TestSubmitProposerPreferencesMissingRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":404,"message":"Route POST:/eth/v1/validator/proposer_preferences not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	err := submitProposerPreferences(context.Background(), srv.Client(), srv.URL, nil)
	require.ErrorContains(t, err, "beacon node lacks the gloas proposer_preferences endpoint")
	require.ErrorContains(t, err, "status 404")
}

// Non-404 failures surface unchanged — no missing-endpoint flag.
func TestSubmitProposerPreferencesOtherStatusUnflagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":500,"message":"internal"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := submitProposerPreferences(context.Background(), srv.Client(), srv.URL, nil)
	require.ErrorContains(t, err, "status 500")
	require.NotContains(t, err.Error(), "beacon node lacks")
}

func TestRequestProposerDutiesDependentRoot(t *testing.T) {
	want := phase0.Root{0xde, 0xad, 0xbe, 0xef}

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		// phase0.Root marshals to the "0x…" JSON string the endpoint returns.
		_ = json.NewEncoder(w).Encode(map[string]any{"dependent_root": want, "data": []any{}})
	}))
	defer srv.Close()

	got, err := requestProposerDutiesDependentRoot(context.Background(), srv.Client(), srv.URL, 3)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v2/validator/duties/proposer/3", gotPath)
	require.Equal(t, want, got)
}

// A dependent_root that is not a valid 32-byte "0x…" root is rejected (phase0.Root.UnmarshalJSON).
func TestRequestProposerDutiesDependentRootRejectsMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"dependent_root":"0x00","data":[]}`)
	}))
	defer srv.Close()

	_, err := requestProposerDutiesDependentRoot(context.Background(), srv.Client(), srv.URL, 3)
	require.Error(t, err)
}
