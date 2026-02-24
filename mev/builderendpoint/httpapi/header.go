package httpapi

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"
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

		w.Header().Set(EthConsensusVersion, bid.Version.String())
		writeJSON(w, http.StatusOK, bid)
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
