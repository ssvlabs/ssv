package httpapi

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func handleHeader(bidProvider BidProviderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bidProvider == nil {
			// Not configured yet; behave as "no bid" rather than hanging or failing.
			w.WriteHeader(http.StatusNoContent)
			return
		}

		slot, parentHash, pubkey, err := parseHeaderParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		bid, err := bidProvider(r.Context(), slot, parentHash, pubkey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to obtain bid")
			return
		}

		if bid == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		respCT, err := codec.PreferredResponseContentType(r.Header.Get("Accept"))
		if err != nil {
			writeError(w, http.StatusNotAcceptable, "not acceptable")
			return
		}

		w.Header().Set(EthConsensusVersion, bid.Version.String())

		switch respCT {
		case codec.MediaTypeJSON:
			writeJSON(w, http.StatusOK, bid)
		case codec.MediaTypeSSZ:
			data, err := marshalSignedBuilderBidSSZ(bid)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to marshal bid")
				return
			}
			w.Header().Set("Content-Type", codec.MediaTypeSSZ)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		default:
			writeError(w, http.StatusNotAcceptable, "not acceptable")
		}
	}
}

func marshalSignedBuilderBidSSZ(bid *builderspec.VersionedSignedBuilderBid) ([]byte, error) {
	if bid == nil {
		return nil, fmt.Errorf("nil bid")
	}

	switch bid.Version {
	case consensusspec.DataVersionBellatrix:
		if bid.Bellatrix == nil {
			return nil, fmt.Errorf("missing bellatrix bid")
		}
		return bid.Bellatrix.MarshalSSZ()
	case consensusspec.DataVersionCapella:
		if bid.Capella == nil {
			return nil, fmt.Errorf("missing capella bid")
		}
		return bid.Capella.MarshalSSZ()
	case consensusspec.DataVersionDeneb:
		if bid.Deneb == nil {
			return nil, fmt.Errorf("missing deneb bid")
		}
		return bid.Deneb.MarshalSSZ()
	case consensusspec.DataVersionElectra:
		if bid.Electra == nil {
			return nil, fmt.Errorf("missing electra bid")
		}
		return bid.Electra.MarshalSSZ()
	case consensusspec.DataVersionFulu:
		if bid.Fulu == nil {
			return nil, fmt.Errorf("missing fulu bid")
		}
		return bid.Fulu.MarshalSSZ()
	default:
		return nil, fmt.Errorf("unsupported bid version %s", bid.Version.String())
	}
}

func parseHeaderParams(r *http.Request) (phase0.Slot, phase0.Hash32, phase0.BLSPubKey, error) {
	slotStr := chi.URLParam(r, "slot")
	parentHashStr := chi.URLParam(r, "parent_hash")
	pubkeyStr := chi.URLParam(r, "pubkey")

	slotU64, err := strconv.ParseUint(slotStr, 10, 64)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid slot")
	}
	slot := phase0.Slot(slotU64)

	parentHash, err := parseHexBytes32(parentHashStr)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid parent_hash")
	}

	pubkey, err := parseHexBytes48(pubkeyStr)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid pubkey")
	}

	return slot, parentHash, pubkey, nil
}

func parseHexBytes32(input string) (phase0.Hash32, error) {
	raw, err := parseFixedHex(input, 32)
	if err != nil {
		return phase0.Hash32{}, err
	}
	var out phase0.Hash32
	copy(out[:], raw)
	return out, nil
}

func parseHexBytes48(input string) (phase0.BLSPubKey, error) {
	raw, err := parseFixedHex(input, 48)
	if err != nil {
		return phase0.BLSPubKey{}, err
	}
	var out phase0.BLSPubKey
	copy(out[:], raw)
	return out, nil
}

func parseFixedHex(input string, size int) ([]byte, error) {
	trimmed := strings.TrimPrefix(input, "0x")
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	if len(b) != size {
		return nil, fmt.Errorf("expected %d bytes, got %d", size, len(b))
	}
	return b, nil
}
