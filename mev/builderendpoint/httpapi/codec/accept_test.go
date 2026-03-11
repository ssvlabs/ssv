package codec_test

import (
	"testing"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestPreferredResponseContentType_DefaultsToJSON(t *testing.T) {
	t.Parallel()

	ct, err := codec.PreferredResponseContentType("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ct != codec.MediaTypeJSON {
		t.Fatalf("got %q want %q", ct, codec.MediaTypeJSON)
	}
}

func TestPreferredResponseContentType_SelectsSSZ(t *testing.T) {
	t.Parallel()

	ct, err := codec.PreferredResponseContentType("application/octet-stream")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ct != codec.MediaTypeSSZ {
		t.Fatalf("got %q want %q", ct, codec.MediaTypeSSZ)
	}
}

func TestPreferredResponseContentType_SelectsByQValue(t *testing.T) {
	t.Parallel()

	ct, err := codec.PreferredResponseContentType("application/json;q=0.1, application/octet-stream;q=0.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ct != codec.MediaTypeSSZ {
		t.Fatalf("got %q want %q", ct, codec.MediaTypeSSZ)
	}
}

func TestPreferredResponseContentType_NotAcceptable(t *testing.T) {
	t.Parallel()

	_, err := codec.PreferredResponseContentType("text/plain")
	if err == nil {
		t.Fatalf("expected error")
	}
}
