package codec_test

import (
	"testing"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestNormalizeContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "default",
			in:   "",
			want: "application/json",
		},
		{
			name: "strip_parameters",
			in:   "application/json; charset=utf-8",
			want: "application/json",
		},
		{
			name: "trim",
			in:   " application/octet-stream ",
			want: "application/octet-stream",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := codec.NormalizeContentType(tt.in); got != tt.want {
				t.Fatalf("unexpected content type: got %q want %q", got, tt.want)
			}
		})
	}
}
