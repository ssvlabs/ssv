package mocks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type requestCallback = func(r *http.Request, resp json.RawMessage) (json.RawMessage, error)
type requestHandler = func(r *http.Request, resp Response) (Response, error)

type Response struct {
	StatusCode int
	Body       json.RawMessage
	Headers    http.Header
	Delay      time.Duration
}

type ResponseOption func(*Response)

func NewResponse(body json.RawMessage, opts ...ResponseOption) Response {
	resp := Response{
		StatusCode: http.StatusOK,
		Body:       body,
		Headers:    make(http.Header),
	}
	for _, opt := range opts {
		opt(&resp)
	}

	return resp
}

func WithStatusCode(statusCode int) ResponseOption {
	return func(resp *Response) {
		resp.StatusCode = statusCode
	}
}

func WithHeader(key, value string) ResponseOption {
	return func(resp *Response) {
		resp.Headers.Set(key, value)
	}
}

func WithDelay(delay time.Duration) ResponseOption {
	return func(resp *Response) {
		resp.Delay = delay
	}
}

func NewServer(onRequestFn requestCallback) *httptest.Server {
	return NewServerWithHandler(func(r *http.Request, resp Response) (Response, error) {
		if onRequestFn == nil {
			return resp, nil
		}

		body, err := onRequestFn(r, resp.Body)
		if err != nil {
			return Response{}, err
		}
		resp.Body = body
		return resp, nil
	})
}

func NewServerWithHandler(onRequestFn requestHandler) *httptest.Server {
	mockResponses := ServerResponses()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := responseForPath(mockResponses, r.URL.Path)

		var err error
		if onRequestFn != nil {
			resp, err = onRequestFn(r, resp)
			if err != nil {
				http.Error(w, fmt.Sprintf("onRequestFn returned error: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if resp.Delay > 0 {
			select {
			case <-time.After(resp.Delay):
			case <-r.Context().Done():
			}
		}

		writeResponse(w, resp)
	}))
}

func responseForPath(mockResponses map[string]json.RawMessage, path string) Response {
	if resp, ok := mockResponses[path]; ok {
		return NewResponse(resp)
	}

	body, err := json.Marshal(map[string]string{
		"error": "unexpected request",
		"path":  path,
	})
	if err != nil {
		panic(fmt.Sprintf("json.Marshal returned error: %v", err))
	}

	return NewResponse(
		body,
		WithStatusCode(http.StatusNotFound),
	)
}

func writeResponse(w http.ResponseWriter, resp Response) {
	for key, values := range resp.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	statusCode := resp.StatusCode
	if statusCode == 0 {
		panic("response status code must be set")
	}

	w.WriteHeader(statusCode)
	if len(resp.Body) == 0 {
		return
	}

	_, _ = w.Write(resp.Body)
}

func ServerResponses() map[string]json.RawMessage {
	var responses map[string]json.RawMessage

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller returned no caller information")
	}
	f, err := os.Open(filepath.Join(filepath.Dir(filename), "mock-beacon-responses.json"))
	if err != nil {
		panic(fmt.Sprintf("os.Open returned error: %v", err))
	}
	defer func() {
		_ = f.Close()
	}()

	err = json.NewDecoder(f).Decode(&responses)
	if err != nil {
		panic(fmt.Sprintf("couldn't decode json file: %v", err))
	}
	return responses
}
