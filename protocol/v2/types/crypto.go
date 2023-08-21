package types

import (
	"encoding/hex"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

// VerifyByOperators verifies signature by the provided operators
// This is a copy of a function with the same name from the spec, except for it's use of
// DeserializeBLSPublicKey function and bounded.CGO
//
// TODO: rethink this function and consider moving/refactoring it.
func VerifyByOperators(s spectypes.Signature, data spectypes.MessageSignature, domain spectypes.DomainType, sigType spectypes.SignatureType, operators []*spectypes.Operator) error {
	// decode sig
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	// find operators
	pks := make([]bls.PublicKey, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pk, err := DeserializeBLSPublicKey(n.GetPublicKey())
				if err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}

				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// compute root
	computedRoot, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	// verify
	// if res := sign.FastAggregateVerify(pks, computedRoot[:]); !res {
	// 	return errors.New("failed to verify signature")
	// }
	if res := Verifier.AggregateVerify(sign, pks, computedRoot); !res {
		return errors.New("failed to verify signature")
	}
	return nil
}

func SingleVerifyByOperators(s spectypes.Signature, data spectypes.MessageSignature, domain spectypes.DomainType, sigType spectypes.SignatureType, operators []*spectypes.Operator) error {
	// decode sig
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	// find operators
	pks := make([]bls.PublicKey, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pk, err := DeserializeBLSPublicKey(n.GetPublicKey())
				if err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}

				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// compute root
	computedRoot, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	// verify
	if res := sign.FastAggregateVerify(pks, computedRoot[:]); !res {
		return errors.New("failed to verify signature")
	}
	return nil
}

func ReconstructSignature(ps *specssv.PartialSigContainer, root [32]byte, validatorPubKey []byte) ([]byte, error) {
	// Reconstruct signatures
	signature, err := spectypes.ReconstructSignatures(ps.Signatures[rootHex(root)])
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct signatures")
	}
	if err := VerifyReconstructedSignature(signature, validatorPubKey, root); err != nil {
		return nil, errors.Wrap(err, "failed to verify reconstruct signature")
	}
	return signature.Serialize(), nil
}

func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey []byte, root [32]byte) error {
	pk, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return errors.Wrap(err, "could not deserialize validator pk")
	}

	// verify reconstructed sig
	if res := Verifier.AggregateVerify(sig, []bls.PublicKey{pk}, root); !res {
		return errors.New("could not reconstruct a valid signature")
	}
	return nil
}

func rootHex(r [32]byte) string {
	return hex.EncodeToString(r[:])
}

var Verifier = NewBatchVerifier(runtime.NumCPU(), 14, time.Millisecond*50)

func init() {
	go Verifier.Start()
}

const messageSize = 32

// SignatureRequest represents a BLS signature request.
type SignatureRequest struct {
	Signature *bls.Sign
	PubKeys   []bls.PublicKey
	Message   [messageSize]byte
	Results   []chan bool // Results are sent on these channels.
}

func (r *SignatureRequest) Finish(result bool) {
	for _, res := range r.Results {
		res <- result
	}
}

type requests map[[messageSize]byte]*SignatureRequest

// BatchVerifier efficiently schedules and batches BLS signature verifications.
// It accumulates requests into batches, which are verified
// in aggregate (to reduce CPU cost) and concurrently
// (to maximize CPU utilization).
type BatchVerifier struct {
	concurrency int
	batchSize   int
	timeout     time.Duration

	timer   *time.Timer // Controls the timeout of the current batch.
	started time.Time   // Records when the current batch started.
	ticker  *time.Ticker
	pending requests
	mu      sync.Mutex

	batches chan []*SignatureRequest // Channel to receive batches of requests.

	busyWorkers atomic.Int32 // Count of currently busy workers.

	// debug struct to calculate the average batch size.
	debug struct {
		lens       [8 * 1024]byte // Lengths of the batches.
		n          int            // Number of batches processed.
		reqs       int            // Number of requests processed.
		dups       int            // Number of duplicate message requests.
		fails      int            // Number of failed batches.
		failReqs   int            // Number of requests in failed batches.
		sync.Mutex                // Mutex to guard access to the debug fields.
	}
}

// NewBatchVerifier returns a BatchVerifier.
//
// `concurrency`: number of worker goroutines.
//
// `batchSize`: target batch size.
//
// `timeout`: max batch wait time, adjusts based on load (see `adaptiveTimeout`).
func NewBatchVerifier(concurrency, batchSize int, timeout time.Duration) *BatchVerifier {
	nopTimer := time.NewTimer(0)
	return &BatchVerifier{
		concurrency: concurrency,
		batchSize:   batchSize,
		timeout:     timeout,
		timer:       nopTimer,
		ticker:      time.NewTicker(timeout),
		pending:     make(requests),
		batches:     make(chan []*SignatureRequest, concurrency*2),
	}
}

// AggregateVerify adds a request to the current batch or verifies it immediately if a similar one exists.
// It returns the result of the signature verification.
func (b *BatchVerifier) AggregateVerify(signature *bls.Sign, pks []bls.PublicKey, message [messageSize]byte) bool {
	result := make(chan bool)

	// If an identical message is already pending, aggregate it's request
	// with the current one and return the result.
	b.mu.Lock()
	if dup, exists := b.pending[message]; exists {
		b.mu.Unlock()

		b.debug.Lock()
		b.debug.reqs++
		b.debug.dups++
		b.debug.Unlock()

		dup.PubKeys = append(dup.PubKeys, pks...)
		sig := *dup.Signature
		sig.Add(signature)
		dup.Signature = &sig
		dup.Results = append(dup.Results, result)
		return <-result
	} else {
		b.debug.Lock()
		b.debug.reqs++
		b.debug.Unlock()
	}

	sr := &SignatureRequest{
		Signature: signature,
		PubKeys:   pks,
		Message:   message,
		Results:   []chan bool{result},
	}
	b.pending[message] = sr
	if len(b.pending) == b.batchSize {
		// Batch size reached: stop the timer and dispatch the batch.
		b.timer.Stop()
		b.started = time.Time{}
		batch := b.pending
		b.pending = make(requests)
		b.mu.Unlock()

		b.batches <- maps.Values(batch)
	} else {
		// Batch has grown: adjust the timer.
		b.timer.Stop()
		t := b.adaptiveTimeout(len(b.pending))
		b.timer.Reset(t)
		b.started = time.Now()
		b.mu.Unlock()
	}

	valid := <-result
	if !valid {
		// If the batch failed, fall back to individual verification.
		return b.verifySingle(sr)
	}
	return valid
}

// adaptiveTimeout calculates the timeout based on the proportion of pending requests.
func (b *BatchVerifier) adaptiveTimeout(pending int) time.Duration {
	if b.started.IsZero() {
		b.started = time.Now()
	}
	workload := int(b.busyWorkers.Load()) + len(b.batches) + int(float64(pending)/float64(b.batchSize))
	if workload > b.concurrency {
		workload = b.concurrency
	}
	// busyness := float64(workload) / float64(b.concurrency) / 2
	busyness := float64(workload+1) / float64(b.concurrency) * 2
	if busyness > 1 {
		busyness = 1
	}

	timeLeft := b.timeout - time.Since(b.started)
	if timeLeft <= 0 {
		return 0
	}
	// log.Printf("workload: %d, busyness: %f, timeLeft: %s, adjusted: %s", workload, busyness, timeLeft, time.Duration(busyness*float64(timeLeft)))
	return time.Duration(busyness * float64(timeLeft))
}

// Start launches the worker goroutines.
func (b *BatchVerifier) Start() {
	go func() {
		for {
			time.Sleep(12 * time.Second)
			stats := b.Stats()
			zap.L().Debug("BatchVerifier stats",
				zap.Int("total_requests", stats.TotalRequests),
				zap.Int("duplicate_requests", stats.DuplicateRequests),
				zap.Int("total_batches", stats.TotalBatches),
				zap.Float64("average_batch_size", stats.AverageBatchSize),
				zap.Int("pending_requests", stats.PendingRequests),
				zap.Int("pending_batches", stats.PendingBatches),
				zap.Int("busy_workers", stats.BusyWorkers),
				zap.Any("recent_batch_sizes", stats.RecentBatchSizes),
				zap.Int("failed_batches", stats.FailedBatches),
				zap.Int("failed_requests", stats.FailedRequests),
			)
		}
	}()

	for i := 0; i < b.concurrency; i++ {
		go b.worker()
	}
}

// worker is a goroutine that processes batches of requests.
func (b *BatchVerifier) worker() {
	for {
		select {
		case batch := <-b.batches:
			b.verify(batch)
			// case <-b.timer.C:
		case <-b.ticker.C:
			// Dispatch the pending requests when the timer expires.
			b.mu.Lock()
			batch := b.pending
			b.pending = make(requests)
			b.mu.Unlock()

			if len(batch) > 0 {
				b.verify(maps.Values(batch))
			}
		}
	}
}

type Stats struct {
	TotalRequests     int
	DuplicateRequests int
	TotalBatches      int
	AverageBatchSize  float64
	PendingRequests   int
	PendingBatches    int
	BusyWorkers       int
	FailedBatches     int
	FailedRequests    int
	RecentBatchSizes  [32]byte
}

func (b *BatchVerifier) Stats() (stats Stats) {
	b.debug.Lock()
	defer b.debug.Unlock()

	stats.TotalRequests = b.debug.reqs
	stats.DuplicateRequests = b.debug.dups
	stats.TotalBatches = b.debug.n
	stats.FailedBatches = b.debug.fails
	stats.FailedRequests = b.debug.failReqs

	// Calculate the average batch size.
	lens := b.debug.lens[:]
	if b.debug.n < len(b.debug.lens) {
		lens = lens[:b.debug.n]
	}
	var sum float64
	for _, l := range lens {
		sum += float64(l)
	}
	stats.AverageBatchSize = sum / float64(len(lens))

	stats.PendingRequests = len(b.pending)
	stats.PendingBatches = len(b.batches)
	stats.BusyWorkers = int(b.busyWorkers.Load())

	startIndex := len(lens) - len(stats.RecentBatchSizes)
	if startIndex < 0 {
		startIndex = 0
	}
	copy(stats.RecentBatchSizes[:], lens[startIndex:])

	return
}

// verify verifies a batch of requests and sends the results back via the Result channels.
func (b *BatchVerifier) verify(batch []*SignatureRequest) {
	b.busyWorkers.Add(1)
	defer b.busyWorkers.Add(-1)

	if len(batch) == 1 {
		batch[0].Finish(b.verifySingle(batch[0]))
		return
	}

	// Prepare the signature, public keys, and messages for batch verification.
	sig := *batch[0].Signature
	pks := make([]bls.PublicKey, len(batch))
	msgs := make([]byte, len(batch)*messageSize)
	for i, req := range batch {
		if i > 0 {
			sig.Add(req.Signature)
		}
		pk := req.PubKeys[0]
		for j := 1; j < len(req.PubKeys); j++ {
			pk.Add(&req.PubKeys[j])
		}
		pks[i] = pk
		copy(msgs[messageSize*i:], req.Message[:])
	}

	// Batch verify the signatures.
	valid := sig.AggregateVerifyNoCheck(pks, msgs)
	for _, req := range batch {
		req.Finish(valid)
	}

	// Update the debug fields under lock.
	b.debug.Lock()
	b.debug.n++
	b.debug.lens[b.debug.n%len(b.debug.lens)] = byte(len(batch))
	if !valid {
		b.debug.fails++
		b.debug.failReqs += len(batch)
	}
	b.debug.Unlock()
}

// verifySingle verifies a single request and sends the result back via the Result channel.
func (b *BatchVerifier) verifySingle(req *SignatureRequest) bool {
	cpy := req.Message
	return req.Signature.FastAggregateVerify(req.PubKeys, cpy[:])
}
