package httpapi

import (
	"errors"
	"net/http"
)

func handleBlindedBlocksV2(unblinder UnblinderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if unblinder == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		signedBlindedBeaconBlock, err := decodeBlindedBlockRequest(r)
		if err != nil {
			var re requestError
			if errors.As(err, &re) {
				writeError(w, re.status, re.msg)
				return
			}
			writeError(w, http.StatusBadRequest, "unable to obtain blinded block")
			return
		}

		signedProposal, err := unblinder(r.Context(), signedBlindedBeaconBlock)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unblind block")
			return
		}
		if signedProposal == nil {
			// This endpoint is intended for "submit without response"; returning 204 makes it
			// ambiguous if unblinding/publishing happened. Treat as an internal failure.
			writeError(w, http.StatusInternalServerError, "failed to unblind block")
			return
		}

		// Builder API v2 blinded blocks endpoint does not return the unblinded payload.
		w.WriteHeader(http.StatusAccepted)
	}
}
