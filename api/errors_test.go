package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorResponse_Render verifies the Render method sets the correct status code.
func TestErrorResponse_Render(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		err      *ErrorResponse
		expected int
	}{
		{"BadRequest", BadRequestError(errors.New("bad input")), 400},
		{"ServerError", Error(errors.New("server error")), 500},
		{"NotFound", ErrNotFound, 404},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)

			err := tc.err.Render(w, r)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, tc.err.Code)
		})
	}
}

// TestErrorResponse_Error verifies the Error method returns the underlying error message.
func TestErrorResponse_Error(t *testing.T) {
	t.Parallel()

	errMsg := "test error message"
	baseErr := errors.New(errMsg)
	resp := ErrorResponse{
		Err:     baseErr,
		Code:    500,
		Status:  "Error",
		Message: "Something went wrong",
	}

	assert.Equal(t, errMsg, resp.Error())
}

// TestBadRequestError verifies BadRequestError creates the correct ErrorResponse.
func TestBadRequestError(t *testing.T) {
	t.Parallel()

	errMsg := "invalid input"
	baseErr := errors.New(errMsg)
	resp := BadRequestError(baseErr)

	assert.Equal(t, baseErr, resp.Err)
	assert.Equal(t, 400, resp.Code)
	assert.Equal(t, http.StatusText(400), resp.Status)
	assert.Equal(t, errMsg, resp.Message)
}

// TestError verifies Error creates the correct ErrorResponse.
func TestError(t *testing.T) {
	t.Parallel()

	errMsg := "server error"
	baseErr := errors.New(errMsg)
	resp := Error(baseErr)

	assert.Equal(t, baseErr, resp.Err)
	assert.Equal(t, 500, resp.Code)
	assert.Equal(t, http.StatusText(500), resp.Status)
	assert.Equal(t, errMsg, resp.Message)
}

// TestErrNotFound verifies ErrNotFound is properly initialized.
func TestErrNotFound(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 404, ErrNotFound.Code)
	assert.Equal(t, "Resource not found.", ErrNotFound.Status)
	assert.Empty(t, ErrNotFound.Message)
	assert.Nil(t, ErrNotFound.Err)
}
