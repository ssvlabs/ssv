package goclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	bitfield "github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the Gloas proposer beacon-node surface.
var _ beacon.GloasProposerCalls = (*GoClient)(nil)

func minimalGloasBlock() *gloas.BeaconBlock {
	return &gloas.BeaconBlock{
		Slot: 7,
		Body: &gloas.BeaconBlockBody{
			ETH1Data:                  &phase0.ETH1Data{BlockHash: make([]byte, 32)},
			SyncAggregate:             &altair.SyncAggregate{SyncCommitteeBits: bitfield.NewBitvector512()},
			SignedExecutionPayloadBid: &gloas.SignedExecutionPayloadBid{Message: &gloas.ExecutionPayloadBid{BuilderIndex: gloas.BuilderIndexSelfBuild}},
			ParentExecutionRequests:   &electra.ExecutionRequests{},
		},
	}
}

func TestRequestGloasBeaconBlock(t *testing.T) {
	blockSSZ, err := minimalGloasBlock().MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotPath, gotRandao, gotGraffiti, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotRandao = r.URL.Query().Get("randao_reveal")
		gotGraffiti = r.URL.Query().Get("graffiti")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01})
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v3/validator/blocks/7", gotPath)
	require.Equal(t, "0x01", gotRandao) // randao is the 5th arg, graffiti the 4th
	require.Equal(t, "0x02", gotGraffiti)
	require.Equal(t, "application/octet-stream", gotAccept)
	require.Equal(t, phase0.Slot(7), got.Slot)
}

func TestSubmitGloasBeaconBlock(t *testing.T) {
	var gotMethod, gotPath, gotVersion, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := submitGloasBeaconBlock(context.Background(), srv.URL, []byte{0x01, 0x02})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v2/beacon/blocks", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}

func TestGloasOctetStreamHTTP_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad block"))
	}))
	defer srv.Close()

	_, err := gloasOctetStreamHTTP(context.Background(), http.MethodGet, srv.URL, nil)
	require.ErrorContains(t, err, "status 400")
}
