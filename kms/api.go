package kms

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	dkgkms "github.com/ssvlabs/ssv-dkg/pkgs/kms"

	"github.com/ssvlabs/ssv/kms/kmstypes"
)

const maxBodyBytes = 4 << 20

// Routes mounts the dKMS HTTP API:
//
//	GET  /v1/kms/config          policy chain and contract, chains this operator signs for
//	GET  /v1/kms/keys            keys this operator holds a share of
//	POST /v1/kms/keys            load a key from an ssv-dkg kms_key.json
//	POST /v1/kms/sign[?wait=1]   submit a signing request (optionally wait for the signature)
//	GET  /v1/kms/requests/{id}   a request's status and signed transaction
func (s *Service) Routes(r chi.Router) {
	r.Get("/v1/kms/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"operator_id":          s.operatorID(),
			"chain_id":             s.chain.ChainID(),
			"chains":               s.Chains(),
			"policy_contract":      s.policy.Hex(),
			"policy_confirmations": s.confs,
			"schemes":              []string{string(dkgkms.SchemeECDSASecp256k1)},
		})
	})
	r.Get("/v1/kms/keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Keys())
	})
	r.Post("/v1/kms/keys", func(w http.ResponseWriter, req *http.Request) {
		kf := &dkgkms.KeyFile{}
		if !readJSON(w, req, kf) {
			return
		}
		info, err := s.RegisterKey(kf)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	r.Post("/v1/kms/sign", func(w http.ResponseWriter, req *http.Request) {
		sr := &kmstypes.SignRequest{}
		if !readJSON(w, req, sr) {
			return
		}
		id, err := s.Submit(req.Context(), sr)
		if err != nil {
			var rejected *RejectedError
			if errors.As(err, &rejected) {
				writeError(w, http.StatusForbidden, err)
			} else {
				writeError(w, http.StatusBadGateway, err)
			}
			return
		}
		if req.URL.Query().Get("wait") == "" {
			res, _ := s.Result(id)
			writeJSON(w, http.StatusAccepted, res)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 60*time.Second)
		defer cancel()
		res, err := s.Wait(ctx, id)
		if err != nil {
			writeError(w, http.StatusGatewayTimeout, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	r.Get("/v1/kms/requests/{id}", func(w http.ResponseWriter, req *http.Request) {
		res, ok := s.Result(chi.URLParam(req, "id"))
		if !ok {
			writeError(w, http.StatusNotFound, errors.New("unknown request"))
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}

func readJSON(w http.ResponseWriter, req *http.Request, out any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	if err := json.Unmarshal(body, out); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		zap.L().Debug("could not write response", zap.Error(err))
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
