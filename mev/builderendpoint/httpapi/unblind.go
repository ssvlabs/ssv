package httpapi

import (
	"net/http"
	"strings"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func handleBlindedBlocks(unblinder UnblinderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if unblinder == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		ctx := r.Context()

		consensusVersion := r.Header.Get(EthConsensusVersion)
		if consensusVersion == "" {
			writeError(w, http.StatusBadRequest, "no "+EthConsensusVersion+" header provided")
			return
		}

		contentType := codec.NormalizeContentType(r.Header.Get("Content-Type"))

		signedBlindedBeaconBlock, err := codec.UnmarshalBlindedBlock(contentType, consensusVersion, r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "unable to obtain blinded block")
			return
		}

		signedProposal, err := unblinder(ctx, signedBlindedBeaconBlock)
		if err != nil {
			// MVP: treat all errors as internal failures.
			writeError(w, http.StatusInternalServerError, "failed to unblind block")
			return
		}

		if signedProposal == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		resp, err := codec.MarshalUnblindBlockResponse(signedProposal)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate unblinded response")
			return
		}

		w.Header().Set(EthConsensusVersion, strings.ToLower(resp.Version.String()))
		writeJSON(w, http.StatusOK, resp)
	}
}
