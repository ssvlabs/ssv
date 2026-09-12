package gloas

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestBuildProduceConfig(t *testing.T) {
	authA := &SignedBuilderRequestAuth{Message: &BuilderRequestAuth{Data: []byte("https://a.example"), Slot: 7}}
	ten := uint64(10)
	cfg := BuilderConfig{
		MinBid:             5,
		BuilderBoostFactor: nil, // -> neutral 100
		Entries: []BuilderEntry{
			{URL: "https://a.example"},               // auth data defaults to the URL bytes; has an auth
			{URL: "https://b.example", MinBid: &ten}, // no auth this slot -> omitted
		},
	}
	auths := map[string]*SignedBuilderRequestAuth{
		BuilderIdentity("https://a.example", []byte("https://a.example")): authA,
	}

	resolved, err := ResolveBuilderConfig(cfg)
	require.NoError(t, err)

	body, unavailable := BuildProduceConfig(resolved, auths)
	require.Equal(t, 1, unavailable, "builder B has no reconstructed auth for the slot")
	require.Equal(t, uint64(5), body.MinBid)
	require.Equal(t, uint64(100), body.BuilderBoostFactor, "nil config boost -> neutral 100")
	require.Len(t, body.Builders, 1, "only the authed builder is included (auth is required per entry)")
	require.Equal(t, "https://a.example", body.Builders[0].URL)
	require.Same(t, authA, body.Builders[0].Auth)
	require.Equal(t, uint64(5), body.Builders[0].MinBid, "entry omits MinBid -> inherits config default 5")
	require.Equal(t, uint64(100), body.Builders[0].BuilderBoostFactor)

	// No reconstructed auths at all -> empty builders list, every configured builder counted unavailable.
	empty, un := BuildProduceConfig(resolved, nil)
	require.Empty(t, empty.Builders)
	require.Equal(t, 2, un)
}

func TestProduceBuilderConfig_MarshalJSON(t *testing.T) {
	body := ProduceBuilderConfig{
		MinBid:             10,
		BuilderBoostFactor: 100,
		Builders: []ProduceBuilderEntry{{
			URL:                 "https://a.example",
			Auth:                &SignedBuilderRequestAuth{Message: &BuilderRequestAuth{Data: []byte{0x01}, Slot: 7}},
			BuilderPubKeys:      []phase0.BLSPubKey{{0xab}},
			MaxExecutionPayment: 250,
			MinBid:              10,
			BuilderBoostFactor:  100,
		}},
	}
	b, err := json.Marshal(&body)
	require.NoError(t, err)
	s := string(b)
	// beacon-API field names, uint64 as decimal strings.
	require.Contains(t, s, `"min_bid":"10"`)
	require.Contains(t, s, `"builder_boost_factor":"100"`)
	require.Contains(t, s, `"max_execution_payment":"250"`)
	require.Contains(t, s, `"url":"https://a.example"`)
	require.Contains(t, s, `"builder_pubkeys":["0xab00`) // pubkey as 0x-hex
	require.Contains(t, s, `"auth":{"message":`)         // nested SignedBuilderRequestAuth object
}

func TestNeutralProduceBuilderConfig(t *testing.T) {
	// The no-builders local-build body: neutral boost (100), no min-bid floor, and an empty (not null)
	// builders list — the beacon-APIs#630 shape a cluster with no builders configured POSTs.
	c := NeutralProduceBuilderConfig()
	require.Equal(t, uint64(defaultBuilderBoostFactor), c.BuilderBoostFactor)
	require.Zero(t, c.MinBid)
	require.Empty(t, c.Builders)

	b, err := json.Marshal(c)
	require.NoError(t, err)
	require.JSONEq(t, `{"min_bid":"0","builder_boost_factor":"100","builders":[]}`, string(b))
}
