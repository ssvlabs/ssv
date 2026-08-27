package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the PTC beacon-node surface.
var _ beacon.PTCCalls = (*GoClient)(nil)

func TestRequestPTCDuties(t *testing.T) {
	duty := &gloas.PTCDuty{PubKey: phase0.BLSPubKey{0x11, 0x22}, ValidatorIndex: 7, Slot: 9}
	dutyJSON, err := json.Marshal(duty)
	require.NoError(t, err)

	var gotMethod, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(w, `{"dependent_root":"0x00","execution_optimistic":false,"data":[%s]}`, dutyJSON)
	}))
	defer srv.Close()

	duties, err := requestPTCDuties(context.Background(), srv.Client(), srv.URL, 3, []phase0.ValidatorIndex{7, 8})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/validator/duties/ptc/3", gotPath)
	require.JSONEq(t, `["7","8"]`, string(gotBody))
	require.Equal(t, []*gloas.PTCDuty{duty}, duties)
}

func TestRequestPayloadAttestationData(t *testing.T) {
	data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)

	var gotMethod, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = fmt.Fprintf(w, `{"version":"gloas","data":%s}`, dataJSON)
	}))
	defer srv.Close()

	got, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 9)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v1/validator/payload_attestation_data", gotPath)
	require.Equal(t, "slot=9", gotQuery)
	require.Equal(t, data, got)
}

// A 204 No Content is the beacon-APIs "no block seen" signal: requestPayloadAttestationData surfaces
// it as (nil, nil), not an error, so the PTC member abstains.
func TestRequestPayloadAttestationData_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	got, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 9)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestSubmitPayloadAttestationMessages(t *testing.T) {
	msgs := []*gloas.PayloadAttestationMessage{{
		ValidatorIndex: 7,
		Data:           &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true},
		Signature:      phase0.BLSSignature{0xbb},
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

	require.NoError(t, submitPayloadAttestationMessages(context.Background(), srv.Client(), srv.URL, msgs))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/beacon/pool/payload_attestations", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	want, err := json.Marshal(msgs)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(gotBody))
}

func TestPTCDo_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":503,"message":"beacon node is syncing"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "503")
}
