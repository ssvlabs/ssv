package handlers

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bloxapp/ssv/api"
)

const maxRequestBodySize = 82 // {"hashed_data":"hash 32 bytes"}

func (h *Node) Sign(w http.ResponseWriter, r *http.Request) error {
	limitedReader := http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	bodyContent := make([]byte, maxRequestBodySize)
	_, err := limitedReader.Read(bodyContent)
	if err != nil && err != io.EOF {
		if err == io.ErrUnexpectedEOF {
			return errors.New("request body too large")
		} else {
			return err
		}
	}
	var request struct {
		HashedData string `json:"hashed_data"`
	}
	if err := json.Unmarshal(bodyContent, &request); err != nil {
		return err
	}
	data, err := hex.DecodeString(request.HashedData)
	if err != nil {
		return err
	}
	signature, err := h.Signer(data[:])
	if err != nil {
		return err
	}
	var response struct {
		Signature string `json:"signature"`
	}
	response.Signature = hex.EncodeToString(signature)
	return api.Render(w, r, response)
}
