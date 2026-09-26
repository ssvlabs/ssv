package goclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
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

// The client remembers the dependent root of each epoch's last successful fetch, so a §5 runner whose own
// fetch fails can still build under the root the scheduler emitted for.
func TestProposerDutiesDependentRoot_RemembersLastRoot(t *testing.T) {
	const epoch = phase0.Epoch(7)
	root := phase0.Root{0xaa}
	var down atomic.Bool
	// The fake beacon node has no fixture for the v2 proposer-duties route, so the response — status
	// included — is built here.
	srv := mocks.NewServerWithHandler(func(r *http.Request, resp mocks.Response) (mocks.Response, error) {
		if r.URL.Path == "/eth/v2/validator/duties/proposer/7" {
			if down.Load() {
				return mocks.Response{}, errors.New("beacon node down")
			}
			return mocks.NewResponse(json.RawMessage(`{"dependent_root":"` + root.String() + `","execution_optimistic":false,"data":[]}`)), nil
		}
		return resp, nil
	})
	defer srv.Close()

	client, err := New(t.Context(), zap.NewNop(), Options{BeaconNodeAddr: srv.URL, CommonTimeout: 400 * time.Millisecond, LongTimeout: 500 * time.Millisecond})
	require.NoError(t, err)

	_, ok := client.LastProposerDutiesDependentRoot(epoch)
	require.False(t, ok, "nothing remembered before a fetch")

	got, err := client.ProposerDutiesDependentRoot(t.Context(), epoch)
	require.NoError(t, err)
	require.Equal(t, root, got)

	down.Store(true)
	_, err = client.ProposerDutiesDependentRoot(t.Context(), epoch)
	require.Error(t, err, "the fresh fetch fails while the beacon node is down")

	last, ok := client.LastProposerDutiesDependentRoot(epoch)
	require.True(t, ok)
	require.Equal(t, root, last, "the last successful fetch is remembered")
	_, ok = client.LastProposerDutiesDependentRoot(epoch + 1)
	require.False(t, ok, "only for the epochs actually fetched")
}
