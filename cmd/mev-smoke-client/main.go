package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	builderapiv1 "github.com/attestantio/go-builder-client/api/v1"
	builderspec "github.com/attestantio/go-builder-client/spec"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	"github.com/ssvlabs/ssv/mev/smoke/sszutil"
)

func main() {
	var (
		builderURL      = flag.String("builder-url", "http://builder:18550", "base URL of the builder endpoint")
		expectedBestWei = flag.String("expected-best-bid-wei", "", "expected best bid value (wei)")
		timeout         = flag.Duration("timeout", 20*time.Second, "overall smoke timeout")
	)
	flag.Parse()

	if *expectedBestWei == "" {
		log.Fatal("-expected-best-bid-wei is required")
	}

	expected := uint256.NewInt(0)
	if err := expected.SetFromDecimal(*expectedBestWei); err != nil {
		log.Fatalf("invalid -expected-best-bid-wei: %q", *expectedBestWei)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	builderURLTrimmed := strings.TrimRight(*builderURL, "/")

	waitFor(ctx, builderURLTrimmed+"/eth/v1/builder/status")

	// Use a non-zero parent hash and pubkey (some clients reject all-zero).
	parentHash := mustHash32("0x" + strings.Repeat("11", 32))
	pubkey := mustPubkey("0x" + strings.Repeat("22", 48))

	bid := fetchHeader(ctx, builderURLTrimmed, 1, parentHash, pubkey)
	value, err := bid.Value()
	if err != nil {
		log.Fatalf("bid value: %v", err)
	}
	if value.Cmp(expected) != 0 {
		log.Fatalf("unexpected best bid: got %s want %s", value.ToBig().String(), expected.ToBig().String())
	}

	postBlindedBlocks(ctx, builderURLTrimmed, parentHash)
	postValidators(ctx, builderURLTrimmed, pubkey)

	log.Print("smoke OK")
}

func waitFor(ctx context.Context, url string) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			log.Fatalf("timeout waiting for %s", url)
		case <-ticker.C:
		}
	}
}

func fetchHeader(ctx context.Context, baseURL string, slot uint64, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) *builderspec.VersionedSignedBuilderBid {
	path := fmt.Sprintf("%s/eth/v1/builder/header/%d/%#x/%#x", baseURL, slot, parentHash[:], pubkey[:])
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("GET header: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("GET header status=%d body=%s", resp.StatusCode, string(body))
	}
	if got := strings.ToLower(resp.Header.Get(httpapi.EthConsensusVersion)); got != "deneb" {
		log.Fatalf("unexpected %s: got %q want %q", httpapi.EthConsensusVersion, got, "deneb")
	}

	var bid builderspec.VersionedSignedBuilderBid
	if err := json.NewDecoder(resp.Body).Decode(&bid); err != nil {
		log.Fatalf("decode header response: %v", err)
	}
	if bid.Version != consensusspec.DataVersionDeneb {
		log.Fatalf("unexpected bid version: got %v", bid.Version)
	}
	return &bid
}

func postBlindedBlocks(ctx context.Context, baseURL string, parentHash phase0.Hash32) {
	body := buildDenebSignedBlindedBeaconBlockJSON(slot(1), parentHash)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("POST blinded_blocks: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("POST blinded_blocks status=%d body=%s", resp.StatusCode, string(b))
	}
	if got := strings.ToLower(resp.Header.Get(httpapi.EthConsensusVersion)); got != "deneb" {
		log.Fatalf("unexpected %s: got %q want %q", httpapi.EthConsensusVersion, got, "deneb")
	}

	var decoded map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		log.Fatalf("decode blinded_blocks response: %v", err)
	}
	if _, ok := decoded["version"]; !ok {
		log.Fatalf("missing response version")
	}
	data, ok := decoded["data"]
	if !ok {
		log.Fatalf("missing response data")
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(data, &inner); err != nil {
		log.Fatalf("decode response data: %v", err)
	}
	if _, ok := inner["execution_payload"]; !ok {
		log.Fatalf("missing data.execution_payload")
	}
	if _, ok := inner["blobs_bundle"]; !ok {
		log.Fatalf("missing data.blobs_bundle")
	}
}

func postValidators(ctx context.Context, baseURL string, pubkey phase0.BLSPubKey) {
	reg := &builderapiv1.SignedValidatorRegistration{
		Message: &builderapiv1.ValidatorRegistration{
			FeeRecipient: bellatrix.ExecutionAddress{1},
			GasLimit:     1,
			Timestamp:    time.Unix(0, 0),
			Pubkey:       pubkey,
		},
		Signature: phase0.BLSSignature{},
	}
	body, err := json.Marshal([]*builderapiv1.SignedValidatorRegistration{reg})
	if err != nil {
		log.Fatalf("marshal validator registrations: %v", err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/eth/v1/builder/validators", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("POST validators: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("POST validators status=%d body=%s", resp.StatusCode, string(b))
	}
}

func buildDenebSignedBlindedBeaconBlockJSON(slot phase0.Slot, parentHash phase0.Hash32) []byte {
	// Build a Deneb payload/header pair that satisfies go-builder-client's check:
	// hash_tree_root(execution_payload) == hash_tree_root(execution_payload_header).
	emptyTransactionsRoot := toRoot(sszutil.EmptyListRoot(1 << 20)) // 1048576
	emptyWithdrawalsRoot := toRoot(sszutil.EmptyListRoot(1 << 4))   // 16

	payload := &deneb.ExecutionPayload{
		ParentHash:    parentHash,
		BaseFeePerGas: uint256.NewInt(0),
		Transactions:  []bellatrix.Transaction{},
		Withdrawals:   []*capella.Withdrawal{},
		BlobGasUsed:   0,
		ExcessBlobGas: 0,
		FeeRecipient:  bellatrix.ExecutionAddress{},
		StateRoot:     phase0.Root{},
		ReceiptsRoot:  phase0.Root{},
		LogsBloom:     [256]byte{},
		PrevRandao:    [32]byte{},
		BlockNumber:   0,
		GasLimit:      0,
		GasUsed:       0,
		Timestamp:     0,
		ExtraData:     []byte{},
		BlockHash:     phase0.Hash32{},
	}

	header := &deneb.ExecutionPayloadHeader{
		ParentHash:       payload.ParentHash,
		FeeRecipient:     payload.FeeRecipient,
		StateRoot:        payload.StateRoot,
		ReceiptsRoot:     payload.ReceiptsRoot,
		LogsBloom:        payload.LogsBloom,
		PrevRandao:       payload.PrevRandao,
		BlockNumber:      payload.BlockNumber,
		GasLimit:         payload.GasLimit,
		GasUsed:          payload.GasUsed,
		Timestamp:        payload.Timestamp,
		ExtraData:        payload.ExtraData,
		BaseFeePerGas:    payload.BaseFeePerGas,
		BlockHash:        payload.BlockHash,
		TransactionsRoot: emptyTransactionsRoot,
		WithdrawalsRoot:  emptyWithdrawalsRoot,
		BlobGasUsed:      payload.BlobGasUsed,
		ExcessBlobGas:    payload.ExcessBlobGas,
	}

	payloadRoot, err := payload.HashTreeRoot()
	if err != nil {
		log.Fatalf("payload HashTreeRoot: %v", err)
	}
	headerRoot, err := header.HashTreeRoot()
	if err != nil {
		log.Fatalf("header HashTreeRoot: %v", err)
	}
	if payloadRoot != headerRoot {
		log.Fatalf("payload/header root mismatch (smoke harness): %#x != %#x", payloadRoot, headerRoot)
	}

	block := &apiv1deneb.SignedBlindedBeaconBlock{
		Message: &apiv1deneb.BlindedBeaconBlock{
			Slot:          slot,
			ProposerIndex: phase0.ValidatorIndex(1),
			ParentRoot:    phase0.Root{},
			StateRoot:     phase0.Root{},
			Body: &apiv1deneb.BlindedBeaconBlockBody{
				RANDAOReveal: phase0.BLSSignature{},
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{},
					DepositCount: 0,
					BlockHash:    make([]byte, 32),
				},
				Graffiti:          [32]byte{},
				ProposerSlashings: make([]*phase0.ProposerSlashing, 0),
				AttesterSlashings: make([]*phase0.AttesterSlashing, 0),
				Attestations:      make([]*phase0.Attestation, 0),
				Deposits:          make([]*phase0.Deposit, 0),
				VoluntaryExits:    make([]*phase0.SignedVoluntaryExit, 0),
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{},
				},
				ExecutionPayloadHeader: header,
				BLSToExecutionChanges:  make([]*capella.SignedBLSToExecutionChange, 0),
				BlobKZGCommitments:     []deneb.KZGCommitment{},
			},
		},
		Signature: phase0.BLSSignature{},
	}

	b, err := json.Marshal(block)
	if err != nil {
		log.Fatalf("marshal blinded block: %v", err)
	}
	return b
}

func mustHash32(input string) phase0.Hash32 {
	b, err := decodeFixedHex(input, 32)
	if err != nil {
		log.Fatalf("bad hash32: %v", err)
	}
	var out phase0.Hash32
	copy(out[:], b)
	return out
}

func mustPubkey(input string) phase0.BLSPubKey {
	b, err := decodeFixedHex(input, 48)
	if err != nil {
		log.Fatalf("bad pubkey: %v", err)
	}
	var out phase0.BLSPubKey
	copy(out[:], b)
	return out
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

func toRoot(b [32]byte) phase0.Root {
	var out phase0.Root
	copy(out[:], b[:])
	return out
}

func slot(v uint64) phase0.Slot { return phase0.Slot(v) }
