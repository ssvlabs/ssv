package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/api"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

type requestError struct {
	status int
	msg    string
}

func (e requestError) Error() string { return e.msg }

func decodeBlindedBlockRequest(r *http.Request) (*api.VersionedSignedBlindedBeaconBlock, error) {
	contentType := codec.NormalizeContentType(r.Header.Get("Content-Type"))
	consensusVersion := r.Header.Get(EthConsensusVersion)

	// Per builder-specs: Eth-Consensus-Version is required for SSZ encoded requests.
	if contentType == codec.MediaTypeSSZ && consensusVersion == "" {
		return nil, requestError{status: http.StatusBadRequest, msg: "no " + EthConsensusVersion + " header provided"}
	}

	// For JSON requests, the header is optional. If missing, try best-effort detection.
	var body io.Reader = r.Body
	if contentType == codec.MediaTypeJSON && consensusVersion == "" {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, requestError{status: http.StatusBadRequest, msg: "unable to read body"}
		}
		ver, err := codec.DetectConsensusVersionFromSignedBlindedBeaconBlockJSON(raw)
		if err != nil {
			return nil, requestError{status: http.StatusBadRequest, msg: "unable to determine consensus version"}
		}
		consensusVersion = ver
		body = bytes.NewReader(raw)
	}

	signedBlindedBeaconBlock, err := codec.UnmarshalBlindedBlock(contentType, consensusVersion, body)
	if err != nil {
		var unsupported codec.UnsupportedContentTypeError
		if errors.As(err, &unsupported) {
			return nil, requestError{status: http.StatusUnsupportedMediaType, msg: "unsupported content type"}
		}
		return nil, requestError{status: http.StatusBadRequest, msg: "unable to obtain blinded block"}
	}

	return signedBlindedBeaconBlock, nil
}

func handleBlindedBlocks(unblinder UnblinderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if unblinder == nil {
			writeError(w, http.StatusServiceUnavailable, "builder not configured")
			return
		}

		ctx := r.Context()

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

		signedProposal, err := unblinder(ctx, signedBlindedBeaconBlock)
		if err != nil {
			// MVP: treat all errors as internal failures.
			writeError(w, http.StatusInternalServerError, "failed to unblind block")
			return
		}

		if signedProposal == nil {
			writeError(w, http.StatusInternalServerError, "failed to unblind block")
			return
		}

		respCT, err := codec.PreferredResponseContentType(r.Header.Get("Accept"))
		if err != nil {
			writeError(w, http.StatusNotAcceptable, "not acceptable")
			return
		}

		w.Header().Set(EthConsensusVersion, strings.ToLower(signedProposal.Version.String()))

		switch respCT {
		case codec.MediaTypeJSON:
			resp, err := codec.BuildSubmitBlindedBlockResponseJSON(signedProposal)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to generate unblinded response")
				return
			}
			writeJSON(w, http.StatusOK, resp)
		case codec.MediaTypeSSZ:
			data, err := codec.MarshalSubmitBlindedBlockResponseSSZ(signedProposal)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to generate unblinded response")
				return
			}
			w.Header().Set("Content-Type", codec.MediaTypeSSZ)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		default:
			writeError(w, http.StatusNotAcceptable, "not acceptable")
			return
		}
	}
}
