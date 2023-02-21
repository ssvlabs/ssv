package metrics

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// handleCountByCollection responds with the number of key in the database by collection.
// Prefix can be a string or a 0x-prefixed hex string.
// Empty prefix returns the total number of keys in the database.
func (mh *metricsHandler) handleCountByCollection(w http.ResponseWriter, r *http.Request) {
	var response struct {
		Count int64 `json:"count"`
	}

	// Parse prefix from query. Supports both hex and string.
	var prefix []byte
	prefixStr := r.URL.Query().Get("prefix")
	if prefixStr != "" {
		if strings.HasPrefix(prefixStr, "0x") {
			var err error
			prefix, err = hex.DecodeString(prefixStr[2:])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			prefix = []byte(prefixStr)
		}
	}

	n, err := mh.db.CountByCollection(prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Count = n

	if err := json.NewEncoder(w).Encode(&response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (mh *metricsHandler) handleHighestInstance(w http.ResponseWriter, r *http.Request) {
	publicKeyStr := strings.TrimPrefix(r.URL.Path, "/highest-instance/")
	if publicKeyStr == "" {
		http.Error(w, "public key is required", http.StatusBadRequest)
		return
	}
	publicKey, err := hex.DecodeString(publicKeyStr)
	if err != nil {
		http.Error(w, "invalid public key", http.StatusBadRequest)
		return
	}
	roleStr := r.URL.Query().Get("role")
	if roleStr == "" {
		http.Error(w, "role is required", http.StatusBadRequest)
		return
	}
	role, err := message.BeaconRoleFromString(roleStr)
	if err != nil {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	st := storage.New(mh.db, mh.logger, role.String(), forksprotocol.GenesisForkVersion)
	msgID := spectypes.NewMsgID(publicKey, role)
	highest, err := st.GetHighestInstance(msgID[:])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	encoded, err := json.Marshal(highest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	instance.Compact(highest.State, highest.DecidedMessage)
	encodedCompact, err := json.Marshal(highest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var response = struct {
		PublicKey       string          `json:"publicKey"`
		Role            string          `json:"role"`
		Size            int             `json:"size"`
		CompactSize     int             `json:"compactSize"`
		Instance        json.RawMessage `json:"instance"`
		CompactInstance json.RawMessage `json:"compactInstance"`
	}{
		PublicKey:       hex.EncodeToString(publicKey),
		Role:            role.String(),
		Size:            len(encoded),
		CompactSize:     len(encodedCompact),
		Instance:        json.RawMessage(encoded),
		CompactInstance: json.RawMessage(encodedCompact),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
