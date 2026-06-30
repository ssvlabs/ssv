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
