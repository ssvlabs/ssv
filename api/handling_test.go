package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dummyString implements fmt.Stringer.
type dummyString string

func (d dummyString) String() string {
	return string(d)
}

// failingRenderer simulates a renderer whose Render always fails.
type failingRenderer struct{}

func (f *failingRenderer) Render(http.ResponseWriter, *http.Request) error {
	return errors.New("render failure")
}

func (f *failingRenderer) Error() string {
	return "render failure"
}

// recordingResponseWriter is a custom ResponseWriter that captures output
// and always returns an error on Write.
type recordingResponseWriter struct {
	http.ResponseWriter
	written string
}

func (r *recordingResponseWriter) Write(b []byte) (int, error) {
	r.written = string(b)

	return 0, errors.New("write failure")
}

// TestRender tests the Render function.
func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		acceptHeader      string
		response          any
		wantContentType   string
		wantBodySubstring string
	}{
		{
			name:              "fallthrough json when no fmt.stringer with text/plain",
			acceptHeader:      "text/plain",
			response:          map[string]interface{}{"key": "value"},
			wantContentType:   "application/json",
			wantBodySubstring: `"key":"value"`,
		},
		{
			name:              "plain text rendering",
			acceptHeader:      "text/plain",
			response:          dummyString("dummy text"),
			wantContentType:   "text/plain",
			wantBodySubstring: "dummy text",
		},
		{
			name:              "explicit json rendering",
			acceptHeader:      "application/json",
			response:          map[string]interface{}{"foo": "bar", "baz": 123},
			wantContentType:   "application/json",
			wantBodySubstring: `"foo":"bar"`,
		},
		{
			name:              "default json when no accept header",
			acceptHeader:      "",
			response:          map[string]interface{}{"key": "value"},
			wantContentType:   "application/json",
			wantBodySubstring: `"key":"value"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			if tc.acceptHeader != "" {
				req.Header.Set("Accept", tc.acceptHeader)
			}
			err := Render(rr, req, tc.response)

			require.NoError(t, err)

			assert.Contains(t, rr.Header().Get("Content-Type"), tc.wantContentType)
			assert.Contains(t, rr.Body.String(), tc.wantBodySubstring)
		})
	}
}

// TestHandler_NoError verifies that a handler without errors writes the expected response.
func TestHandler_NoError(t *testing.T) {
	t.Parallel()

	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("OK"))
		return err
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	h(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "OK", rr.Body.String())
}

// TestHandler_RendererError verifies that if the handler returns an error implementing render.Renderer,
// the renderer is used to write the error response.
func TestHandler_RendererError(t *testing.T) {
	t.Parallel()

	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return BadRequestError(errors.New("my error"))
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	req.Header.Set("Accept", "text/plain")
	h(rr, req)

	assert.Equal(t, `{"status":"Bad Request","error":"my error"}`, strings.TrimSpace(rr.Body.String()))
}

// TestHandler_RendererError_Failure verifies that when the renderer returns an error,
// the Handler calls http.Error with the renderer's error message.
func TestHandler_RendererError_Failure(t *testing.T) {
	t.Parallel()

	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return &failingRenderer{}
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	req.Header.Set("Accept", "text/plain")
	h(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "render failure", strings.TrimSpace(rr.Body.String()))
}

// TestHandler_DefaultError verifies that if the handler returns a plain error,
// the default error branch is executed and the response contains the error message.
func TestHandler_DefaultError(t *testing.T) {
	t.Parallel()

	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("plain error")
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h(rr, req)

	assert.Contains(t, rr.Body.String(), "plain error")
}

// TestHandler_DefaultError_Failure verifies that when the default error branch's renderer fails,
// the Handler calls http.Error with the appropriate error message.
func TestHandler_DefaultError_Failure(t *testing.T) {
	t.Parallel()

	h := Handler(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("plain error")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	customRW := &recordingResponseWriter{ResponseWriter: rr}
	h(customRW, req)

	assert.Contains(t, customRW.written, "plain error")
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
