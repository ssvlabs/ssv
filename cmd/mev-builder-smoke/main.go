package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint"
	"github.com/ssvlabs/ssv/mev/builderendpoint/config"
)

func main() {
	var (
		listen              = flag.String("listen", ":18550", "listen address")
		relaysCSV           = flag.String("relays", "", "comma-separated relay URLs (e.g. http://relay_a:18551,http://relay_b:18552)")
		relayTimeout        = flag.Duration("relay-timeout", 750*time.Millisecond, "per-relay request timeout")
		bidDeadline         = flag.Duration("bid-deadline", 750*time.Millisecond, "overall bidding deadline")
		bidGap              = flag.Duration("bid-gap", 50*time.Millisecond, "polling gap between bid attempts")
		cacheTTL            = flag.Duration("cache-ttl", 2*time.Second, "bid cache TTL")
		prefetchMaxInflight = flag.Int("prefetch-max-in-flight", 4, "max in-flight prefetch requests")
		unblindRetries      = flag.Int("unblind-retries", 0, "retries per relay for unblinding")
		unblindRetryInt     = flag.Duration("unblind-retry-interval", 0, "interval between unblind retries")

		prewarmEnabled = flag.Bool("prewarm", false, "prewarm a single bid key before starting the HTTP server (smoke harness)")
		prewarmSlot    = flag.Uint64("prewarm-slot", 1, "slot to prewarm (must match client test slot)")
		prewarmParent  = flag.String("prewarm-parent-hash", "0x"+strings.Repeat("11", 32), "parent hash to prewarm (hex, 32 bytes)")
		prewarmPubkey  = flag.String("prewarm-pubkey", "0x"+strings.Repeat("22", 48), "pubkey to prewarm (hex, 48 bytes)")
		prewarmTimeout = flag.Duration("prewarm-timeout", 5*time.Second, "timeout for prewarming bids before serving")
	)
	flag.Parse()

	if *relaysCSV == "" {
		log.Fatal("-relays is required")
	}
	relays := splitCSV(*relaysCSV)
	if len(relays) == 0 {
		log.Fatal("no relays supplied")
	}

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}

	cfg := config.Config{
		Enabled:                   true,
		ListenAddress:             *listen,
		Relays:                    relays,
		RelayRequestTimeout:       *relayTimeout,
		BidDeadline:               *bidDeadline,
		BidGap:                    *bidGap,
		CacheTTL:                  *cacheTTL,
		PrefetchEnabled:           true,
		PrefetchParentHashTimeout: 150 * time.Millisecond,
		PrefetchMaxInFlight:       *prefetchMaxInflight,
		UnblindRetries:            *unblindRetries,
		UnblindRetryInterval:      *unblindRetryInt,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := builderendpoint.New(ctx, logger, cfg, builderendpoint.Dependencies{
		// For this smoke harness we don't have a real chain time; make deadlines relative to "now".
		SlotStartTime: func(_ phase0.Slot) time.Time { return time.Now() },
	})
	if err != nil {
		log.Fatalf("builder endpoint init: %v", err)
	}

	if *prewarmEnabled {
		slot := phase0.Slot(*prewarmSlot)
		parentHash, err := parseHash32(*prewarmParent)
		if err != nil {
			log.Fatalf("prewarm parent hash: %v", err)
		}
		pubkey, err := parsePubkey(*prewarmPubkey)
		if err != nil {
			log.Fatalf("prewarm pubkey: %v", err)
		}

		pctx, cancel := context.WithTimeout(ctx, *prewarmTimeout)
		defer cancel()

		start := time.Now()
		if err := srv.PrefetchBidSync(pctx, slot, parentHash, pubkey); err != nil {
			log.Fatalf("prewarm failed: %v", err)
		}
		logger.Info("prewarm complete", zap.Duration("took", time.Since(start)))
	}

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("builder endpoint error: %v", err)
	}
}

func splitCSV(in string) []string {
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parseHash32(input string) (phase0.Hash32, error) {
	b, err := decodeFixedHex(input, 32)
	if err != nil {
		return phase0.Hash32{}, err
	}
	var out phase0.Hash32
	copy(out[:], b)
	return out, nil
}

func parsePubkey(input string) (phase0.BLSPubKey, error) {
	b, err := decodeFixedHex(input, 48)
	if err != nil {
		return phase0.BLSPubKey{}, err
	}
	var out phase0.BLSPubKey
	copy(out[:], b)
	return out, nil
}

func decodeFixedHex(input string, size int) ([]byte, error) {
	trimmed := strings.TrimPrefix(input, "0x")
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	if len(b) != size {
		return nil, fmt.Errorf("expected %d bytes got %d", size, len(b))
	}
	return b, nil
}
