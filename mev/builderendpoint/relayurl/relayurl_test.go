package relayurl

import "testing"

func TestStripUserInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "NoUserInfo", in: "https://relay.example.org", want: "https://relay.example.org"},
		{name: "UserInfoInURL", in: "https://0xabc@relay.example.org", want: "https://relay.example.org"},
		{name: "UserInfoWithPath", in: "https://0xabc@relay.example.org/path", want: "https://relay.example.org/path"},
		{name: "FallbackNoScheme", in: "0xabc@relay.example.org", want: "relay.example.org"},
		{name: "FallbackWithPort", in: "https://0xabc@relay.example.org:443", want: "https://relay.example.org:443"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := StripUserInfo(tt.in)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "URL", in: "https://relay.example.org", want: "relay.example.org"},
		{name: "URLWithPort", in: "https://relay.example.org:443", want: "relay.example.org:443"},
		{name: "URLWithUserInfo", in: "https://0xabc@relay.example.org", want: "relay.example.org"},
		{name: "URLWithUserInfoAndPort", in: "https://0xabc@relay.example.org:443", want: "relay.example.org:443"},
		{name: "NoScheme", in: "relay.example.org:443/path", want: "relay.example.org:443"},
		{name: "NoSchemeWithUserInfo", in: "0xabc@relay.example.org:443/path", want: "relay.example.org:443"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Host(tt.in)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
