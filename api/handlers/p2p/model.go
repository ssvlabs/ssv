package p2p

import (
	"net/http"

	"github.com/go-chi/render"
)

type pinnedPeerJSON struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Connected bool     `json:"connected"`
}

type listPinnedResponse struct {
	Pinned []pinnedPeerJSON `json:"pinned"`
}

type pinPeersRequest struct {
	Peers []string `json:"peers"`
}

type pinPeersResponse struct {
	Added   []string     `json:"added"`
	Failed  []failedItem `json:"failed,omitempty"`
	Partial bool         `json:"partial_success,omitempty"`
}

type unpinPeersRequest struct {
	Peers []string `json:"peers"`
}

type unpinPeersResponse struct {
	Removed  []string     `json:"removed"`
	NotFound []string     `json:"not_found"`
	Failed   []failedItem `json:"failed,omitempty"`
	Partial  bool         `json:"partial_success,omitempty"`
}

type invalidItem struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

type failedItem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// badRequestInvalid is a renderer+error that returns 400 with a structured invalid list.
type badRequestInvalid struct {
	Message string        `json:"error"`
	Invalid []invalidItem `json:"invalid"`
}

func (e *badRequestInvalid) Error() string { return e.Message }

func (e *badRequestInvalid) Render(w http.ResponseWriter, r *http.Request) error {
	// Only set the status; the outer render.Render will serialize the body.
	render.Status(r, http.StatusBadRequest)
	return nil
}
