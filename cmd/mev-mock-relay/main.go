package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	builderapideneb "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	consensusdeneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	"github.com/ssvlabs/ssv/mev/smoke/sszutil"
)

func main() {
	var (
		listen           = flag.String("listen", ":18551", "listen address")
		relayID          = flag.String("id", "relay", "relay identifier (logging only)")
		bidEnabled       = flag.Bool("bid-enabled", true, "whether this relay returns bids")
		bidValueWei      = flag.String("bid-value-wei", "0", "bid value in wei (uint256)")
		bidValueWeiAfter = flag.String("bid-value-wei-after", "", "optional bid value after -bid-value-after duration (wei, uint256)")
		bidValueAfter    = flag.Duration("bid-value-after", 0, "duration after process start at which bid value switches to -bid-value-wei-after")
		bidDelay         = flag.Duration("bid-delay", 0, "optional delay before responding to getHeader")
		bidHang          = flag.Bool("bid-hang", false, "if set, hang getHeader until request context is done")

		unblindEnabled = flag.Bool("unblind-enabled", true, "whether this relay supports unblinding")
		unblindDelay   = flag.Duration("unblind-delay", 0, "optional delay before responding to blinded_blocks")
		unblindStatus  = flag.Int("unblind-status", http.StatusOK, "HTTP status code for blinded_blocks")

		validatorsStatus = flag.Int("validators-status", http.StatusOK, "HTTP status code for validators")
	)
	flag.Parse()

	startedAt := time.Now()

	value := uint256.NewInt(0)
	if bidValueWei != nil && *bidValueWei != "" {
		if err := value.SetFromDecimal(*bidValueWei); err != nil {
			log.Fatalf("invalid -bid-value-wei: %q", *bidValueWei)
		}
	}
	valueAfter := (*uint256.Int)(nil)
	if *bidValueWeiAfter != "" {
		tmp := uint256.NewInt(0)
		if err := tmp.SetFromDecimal(*bidValueWeiAfter); err != nil {
			log.Fatalf("invalid -bid-value-wei-after: %q", *bidValueWeiAfter)
		}
		valueAfter = tmp
	}

	emptyTransactionsRoot := toRoot(sszutil.EmptyListRoot(1 << 20)) // 1048576
	emptyWithdrawalsRoot := toRoot(sszutil.EmptyListRoot(1 << 4))   // 16

	mux := http.NewServeMux()

	mux.HandleFunc("/eth/v1/builder/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/eth/v1/builder/header/", func(w http.ResponseWriter, r *http.Request) {
		if *bidHang {
			<-r.Context().Done()
			return
		}

		if *bidDelay > 0 {
			time.Sleep(*bidDelay)
		}

		// Path: /eth/v1/builder/header/{slot}/{parent_hash}/{pubkey}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/eth/v1/builder/header/"), "/")
		if len(parts) != 3 {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}

		_, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid slot", http.StatusBadRequest)
			return
		}
		parentHash, err := parseHex32(parts[1])
		if err != nil {
			http.Error(w, "invalid parent_hash", http.StatusBadRequest)
			return
		}
		_, err = parseHex48(parts[2])
		if err != nil {
			http.Error(w, "invalid pubkey", http.StatusBadRequest)
			return
		}

		if !*bidEnabled {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		bidValue := value
		if valueAfter != nil && *bidValueAfter > 0 && time.Since(startedAt) >= *bidValueAfter {
			bidValue = valueAfter
		}

		// Minimal bid: only needs to be parseable and have correct parent hash.
		bid := &builderspec.VersionedSignedBuilderBid{
			Version: consensusspec.DataVersionDeneb,
			Deneb: &builderapideneb.SignedBuilderBid{
				Message: &builderapideneb.BuilderBid{
					Header: &consensusdeneb.ExecutionPayloadHeader{
						ParentHash:       parentHash,
						BaseFeePerGas:    uint256.NewInt(0),
						TransactionsRoot: emptyTransactionsRoot,
						WithdrawalsRoot:  emptyWithdrawalsRoot,
					},
					BlobKZGCommitments: []consensusdeneb.KZGCommitment{},
					Value:              bidValue,
					Pubkey:             phase0.BLSPubKey{1},
				},
				Signature: phase0.BLSSignature{},
			},
		}

		b, err := json.Marshal(bid)
		if err != nil {
			http.Error(w, "marshal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(httpapi.EthConsensusVersion, "deneb")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	mux.HandleFunc("/eth/v1/builder/blinded_blocks", func(w http.ResponseWriter, r *http.Request) {
		if !*unblindEnabled || *unblindStatus != http.StatusOK {
			http.Error(w, "unblind failed", *unblindStatus)
			return
		}
		if *unblindDelay > 0 {
			time.Sleep(*unblindDelay)
		}

		consensusVersion := strings.ToLower(r.Header.Get(httpapi.EthConsensusVersion))
		if consensusVersion == "" {
			http.Error(w, "missing Eth-Consensus-Version", http.StatusBadRequest)
			return
		}
		if consensusVersion != "deneb" {
			http.Error(w, "unsupported version", http.StatusBadRequest)
			return
		}

		var blinded deneb.SignedBlindedBeaconBlock
		if err := json.NewDecoder(r.Body).Decode(&blinded); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if blinded.Message == nil || blinded.Message.Body == nil || blinded.Message.Body.ExecutionPayloadHeader == nil {
			http.Error(w, "missing payload header", http.StatusBadRequest)
			return
		}

		h := blinded.Message.Body.ExecutionPayloadHeader
		// This mock relay only supports returning a payload with empty txs/withdrawals; validate header roots.
		if h.TransactionsRoot != emptyTransactionsRoot {
			http.Error(w, "unexpected transactions_root (smoke harness requires empty tx list root)", http.StatusBadRequest)
			return
		}
		if h.WithdrawalsRoot != emptyWithdrawalsRoot {
			http.Error(w, "unexpected withdrawals_root (smoke harness requires empty withdrawals list root)", http.StatusBadRequest)
			return
		}

		payload := &consensusdeneb.ExecutionPayload{
			ParentHash:    h.ParentHash,
			FeeRecipient:  h.FeeRecipient,
			StateRoot:     h.StateRoot,
			ReceiptsRoot:  h.ReceiptsRoot,
			LogsBloom:     h.LogsBloom,
			PrevRandao:    h.PrevRandao,
			BlockNumber:   h.BlockNumber,
			GasLimit:      h.GasLimit,
			GasUsed:       h.GasUsed,
			Timestamp:     h.Timestamp,
			ExtraData:     h.ExtraData,
			BaseFeePerGas: h.BaseFeePerGas,
			BlockHash:     h.BlockHash,
			Transactions:  []bellatrix.Transaction{},
			Withdrawals:   []*capella.Withdrawal{},
			BlobGasUsed:   h.BlobGasUsed,
			ExcessBlobGas: h.ExcessBlobGas,
		}

		// Return a v1-style response wrapper with `data`, compatible with go-builder-client's decodeJSONResponse().
		resp := map[string]any{
			"version": "deneb",
			"data": map[string]any{
				"execution_payload": payload,
				"blobs_bundle": map[string]any{
					"commitments": []any{},
					"proofs":      []any{},
					"blobs":       []any{},
				},
			},
		}

		b, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "marshal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(httpapi.EthConsensusVersion, "deneb")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	mux.HandleFunc("/eth/v1/builder/validators", func(w http.ResponseWriter, r *http.Request) {
		// Accept and ignore the registrations; return 200 for success.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(*validatorsStatus)
	})

	log.Printf("[%s] listening on %s", *relayID, *listen)
	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Fatalf("[%s] server error: %v", *relayID, err)
	}
}

func parseHex32(input string) (phase0.Hash32, error) {
	b, err := parseFixedHex(input, 32)
	if err != nil {
		return phase0.Hash32{}, err
	}
	var out phase0.Hash32
	copy(out[:], b)
	return out, nil
}

func parseHex48(input string) (phase0.BLSPubKey, error) {
	b, err := parseFixedHex(input, 48)
	if err != nil {
		return phase0.BLSPubKey{}, err
	}
	var out phase0.BLSPubKey
	copy(out[:], b)
	return out, nil
}

func parseFixedHex(input string, size int) ([]byte, error) {
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

func toRoot(b [32]byte) phase0.Root {
	var out phase0.Root
	copy(out[:], b[:])
	return out
}
