package api

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestErrorResponse_Render(t *testing.T) {
	type fields struct {
		Err     error
		Code    int
		Status  string
		Message string
	}
	type args struct {
		w http.ResponseWriter
		r *http.Request
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:   "ok",
			fields: fields{Err: errors.New("err"), Code: 400, Status: "", Message: ""},
			args:   args{w: nil, r: &http.Request{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ErrorResponse{
				Err:     tt.fields.Err,
				Code:    tt.fields.Code,
				Status:  tt.fields.Status,
				Message: tt.fields.Message,
			}
			if err := e.Render(tt.args.w, tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("ErrorResponse.Render() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorResponse_Error(t *testing.T) {
	type fields struct {
		Err     error
		Code    int
		Status  string
		Message string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "ok",
			fields: fields{Err: errors.New("err"), Code: 400, Status: "", Message: ""},
			want:   "err",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ErrorResponse{
				Err:     tt.fields.Err,
				Code:    tt.fields.Code,
				Status:  tt.fields.Status,
				Message: tt.fields.Message,
			}
			if got := e.Error(); got != tt.want {
				t.Errorf("ErrorResponse.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInvalidRequestError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want *ErrorResponse
	}{
		{
			name: "ok",
			args: args{err: errors.New("err")},
			want: &ErrorResponse{
				Err:     errors.New("err"),
				Code:    400,
				Status:  http.StatusText(400),
				Message: "err",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InvalidRequestError(tt.args.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InvalidRequestError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want *ErrorResponse
	}{
		{
			name: "ok",
			args: args{err: errors.New("err")},
			want: &ErrorResponse{
				Err:     errors.New("err"),
				Code:    500,
				Status:  http.StatusText(500),
				Message: "err",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Error(tt.args.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}
