package types

import (
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/jamiealquiza/tachymeter"
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
		return SingleVerifyByOperators(s, data, domain, sigType, operators)
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

var Verifier = NewBatchVerifier(runtime.NumCPU(), 10, time.Millisecond*5)

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

	// timings keeps the start time of the request, multiple if it's aggregated.
	timings []time.Time
}

func (r *SignatureRequest) Finish(result bool) {
	for _, res := range r.Results {
		res <- result
	}
}

type requests map[[messageSize]byte]*SignatureRequest

type batch struct {
	start    time.Time
	requests []*SignatureRequest
}

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

	batches chan batch // Channel to receive batches of requests.

	busyWorkers atomic.Int32 // Count of currently busy workers.

	// debug struct to calculate the average batch size.
	debug struct {
		lens                    [8 * 1024]byte         // Lengths of the batches.
		batches                 int                    // Number of batches processed.
		reqs                    int                    // Number of requests processed.
		dups                    int                    // Number of duplicate message requests.
		fails                   int                    // Number of failed batches.
		failReqs                int                    // Number of requests in failed batches.
		requestTotalDurations   *tachymeter.Tachymeter // Lifetime duration of individual requests.
		requestPendingDurations *tachymeter.Tachymeter // Pending duration of individual requests.
		batchDurations          *tachymeter.Tachymeter // Duration of batches.
		batchPendingDurations   *tachymeter.Tachymeter // Pending duration of batches.
		sync.Mutex                                     // Mutex to guard access to the debug fields.
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
	v := &BatchVerifier{
		concurrency: concurrency,
		batchSize:   batchSize,
		timeout:     timeout,
		timer:       nopTimer,
		ticker:      time.NewTicker(timeout),
		pending:     make(requests),
		batches:     make(chan batch, concurrency*2*128),
	}
	n := concurrency * 1024
	v.debug.requestTotalDurations = tachymeter.New(&tachymeter.Config{Size: batchSize * n})
	v.debug.requestPendingDurations = tachymeter.New(&tachymeter.Config{Size: batchSize * n})
	v.debug.batchDurations = tachymeter.New(&tachymeter.Config{Size: n})
	v.debug.batchPendingDurations = tachymeter.New(&tachymeter.Config{Size: n})
	return v
}

// AggregateVerify adds a request to the current batch or verifies it immediately if a similar one exists.
// It returns the result of the signature verification.
func (b *BatchVerifier) AggregateVerify(signature *bls.Sign, pks []bls.PublicKey, message [messageSize]byte) bool {
	start := time.Now()
	defer func() {
		b.debug.Lock()
		b.debug.requestTotalDurations.AddTime(time.Since(start))
		b.debug.Unlock()
	}()
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
		dup.timings = append(dup.timings, start)
		valid := <-result
		if !valid {
			// If the aggregated verification failed, fall back to individual verification.
			return b.verifySingle(&SignatureRequest{
				Signature: signature,
				PubKeys:   pks,
				Message:   message,
			})
		}
		return valid
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
		timings:   []time.Time{start},
	}
	b.pending[message] = sr
	if len(b.pending) == b.batchSize {
		// Batch size reached: stop the timer and dispatch the batch.
		b.timer.Stop()
		previouslyStarted := b.started
		b.started = time.Time{}
		pending := b.pending
		b.pending = make(requests)
		b.mu.Unlock()

		b.debug.Lock()
		b.debug.batchPendingDurations.AddTime(time.Since(previouslyStarted))
		b.debug.Unlock()

		b.batches <- batch{
			start:    previouslyStarted,
			requests: maps.Values(pending),
		}
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
			formatGauge := func(g Gauge[time.Duration]) string {
				ms := func(d time.Duration) string {
					return fmt.Sprintf("%.1fms", d.Seconds()*1000)
				}
				return fmt.Sprintf("min: %s,\nmax: %s,\navg: %s,\np99: %s,\np95: %s,\np50: %s", ms(g.Min), ms(g.Max), ms(g.Avg), ms(g.P99), ms(g.P95), ms(g.P50))
			}
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
				zap.String("request_total_durations", formatGauge(stats.RequestTotalDurations)),
				zap.String("request_pending_durations", formatGauge(stats.RequestPendingDurations)),
				zap.String("batch_durations", formatGauge(stats.BatchDurations)),
				zap.String("batch_pending_durations", formatGauge(stats.BatchPendingDurations)),
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
		case newBatch := <-b.batches:
			b.verify(newBatch)
			// case <-b.timer.C:
		case <-b.ticker.C:
			// Dispatch the pending requests when the timer expires.
			b.mu.Lock()
			previouslyStarted := b.started
			pending := b.pending
			b.pending = make(requests)
			b.mu.Unlock()

			if len(pending) > 0 {
				b.debug.Lock()
				b.debug.batchPendingDurations.AddTime(time.Since(previouslyStarted))
				b.debug.Unlock()

				b.verify(batch{
					start:    previouslyStarted,
					requests: maps.Values(pending),
				})
			}
		}
	}
}

type Gauge[T any] struct {
	Min T
	Max T
	Avg T
	P99 T
	P95 T
	P50 T
}

func gaugeFromTachymeter(t *tachymeter.Tachymeter) Gauge[time.Duration] {
	c := t.Calc()
	return Gauge[time.Duration]{
		Min: c.Time.Min,
		Max: c.Time.Max,
		Avg: c.Time.Avg,
		P99: c.Time.P99,
		P95: c.Time.P95,
		P50: c.Time.P50,
	}
}

type Stats struct {
	mu                      sync.Mutex
	TotalRequests           int
	DuplicateRequests       int
	TotalBatches            int
	AverageBatchSize        float64
	PendingRequests         int
	PendingBatches          int
	BusyWorkers             int
	FailedBatches           int
	FailedRequests          int
	RequestTotalDurations   Gauge[time.Duration]
	RequestPendingDurations Gauge[time.Duration]
	BatchDurations          Gauge[time.Duration]
	BatchPendingDurations   Gauge[time.Duration]
	RecentBatchSizes        [32]byte
}

func (b *BatchVerifier) Stats() (stats Stats) {
	b.debug.Lock()
	defer b.debug.Unlock()

	stats.TotalRequests = b.debug.reqs
	stats.DuplicateRequests = b.debug.dups
	stats.TotalBatches = b.debug.batches
	stats.FailedBatches = b.debug.fails
	stats.FailedRequests = b.debug.failReqs
	stats.RequestTotalDurations = gaugeFromTachymeter(b.debug.requestTotalDurations)
	stats.RequestPendingDurations = gaugeFromTachymeter(b.debug.requestPendingDurations)
	stats.BatchDurations = gaugeFromTachymeter(b.debug.batchDurations)
	stats.BatchPendingDurations = gaugeFromTachymeter(b.debug.batchPendingDurations)

	// Calculate the average batch size.
	lens := b.debug.lens[:]
	if b.debug.batches < len(b.debug.lens) {
		lens = lens[:b.debug.batches]
	}
	var sum float64
	for _, l := range lens {
		sum += float64(l)
	}
	stats.AverageBatchSize = sum / float64(len(lens))

	b.mu.Lock()
	pendingCount := len(b.pending)
	b.mu.Unlock()

	stats.PendingRequests = pendingCount
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
func (b *BatchVerifier) verify(batch batch) {
	b.busyWorkers.Add(1)
	defer b.busyWorkers.Add(-1)

	b.debug.Lock()
	for _, req := range batch.requests {
		for _, t := range req.timings {
			b.debug.requestPendingDurations.AddTime(time.Since(t))
		}
	}
	b.debug.Unlock()

	defer func() {
		b.debug.Lock()
		b.debug.batchDurations.AddTime(time.Since(batch.start))
		b.debug.Unlock()
	}()

	if len(batch.requests) == 1 {
		batch.requests[0].Finish(b.verifySingle(batch.requests[0]))
		return
	}

	// Prepare the signature, public keys, and messages for batch verification.
	sig := *batch.requests[0].Signature
	pks := make([]bls.PublicKey, len(batch.requests))
	msgs := make([]byte, len(batch.requests)*messageSize)
	for i, req := range batch.requests {
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
	for _, req := range batch.requests {
		req.Finish(valid)
	}

	// Update the debug fields under lock.
	b.debug.Lock()
	b.debug.batches++
	b.debug.lens[b.debug.batches%len(b.debug.lens)] = byte(len(batch.requests))
	if !valid {
		b.debug.fails++
		b.debug.failReqs += len(batch.requests)
	}
	b.debug.Unlock()
}

// verifySingle verifies a single request and sends the result back via the Result channel.
func (b *BatchVerifier) verifySingle(req *SignatureRequest) bool {
	cpy := req.Message
	return req.Signature.FastAggregateVerify(req.PubKeys, cpy[:])
}
