package main

import (
	"context"
	"fmt"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/rs/zerolog"
	"time"
)

func main() {
	ctx := context.Background()
	client, err := eth2clienthttp.New(ctx,
		// WithAddress supplies the address of the beacon node, in host:port format.
		eth2clienthttp.WithAddress("http://bn-h-3.stage.bloxinfra.com:5052"),
		// LogLevel supplies the level of logging to carry out.
		eth2clienthttp.WithLogLevel(zerolog.DebugLevel),
	)
	if err != nil {
		panic(err)
	}

	timestamp := time.Now()
	fmt.Println("Started at", formatDate(timestamp))
	if err := client.(*eth2clienthttp.Service).Events(ctx, []string{"finalized_checkpoint"}, func(event *eth2apiv1.Event) {
		if event.Data == nil {
			return
		}

		ch := event.Data.(*eth2apiv1.FinalizedCheckpointEvent)
		now := time.Now()
		fmt.Println(formatDate(now), "-> ", ch, "formed in", now.Sub(timestamp))
		timestamp = now
	}); err != nil {
		panic(fmt.Errorf("failed to subscribe to finalized checkpoint events: %w", err))
	}

	select {
	case <-ctx.Done():
		return
	}

}

func formatDate(t time.Time) string {
	return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}
