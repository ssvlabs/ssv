package api

import (
	"net/http"

	"github.com/go-chi/render"
)

type ErrorResponse struct {
	Err  error `json:"-"` // low-level runtime error
	Code int   `json:"-"` // http response status code

	Status  string `json:"status"`          // user-level status message
	Message string `json:"error,omitempty"` // application-level error message, for debugging
}

func (e *ErrorResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.Code)
	return nil
}

func (e *ErrorResponse) Error() string {
	return e.Err.Error()
}

func BadRequestError(err error) *ErrorResponse {
	return &ErrorResponse{
		Err:     err,
		Code:    400,
		Status:  http.StatusText(400),
		Message: err.Error(),
	}
}

func Error(err error) *ErrorResponse {
	return &ErrorResponse{
		Err:     err,
		Code:    500,
		Status:  http.StatusText(500),
		Message: err.Error(),
	}
}

var ErrNotFound = &ErrorResponse{Code: 404, Status: "Resource not found."}
