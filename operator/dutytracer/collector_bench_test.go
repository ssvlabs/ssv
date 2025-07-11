package validator

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func BenchmarkTracer(b *testing.B) {
	f, err := os.OpenFile("./benchdata/slot_3707881_3707882.ssz", os.O_RDONLY, 0644)
	if err != nil {
		b.Fatal(err)
	}

	traces, err := readByteSlices(f) // len(traces) = 8992
	if err != nil {
		b.Fatal(err)
	}

	_ = f.Close()

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		b.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, nil)

	// Define different message counts to test
	messageCounts := []int{10, 20, 50, 100, 200, 500, 1000, 2000, 4000, 8000}

	for _, count := range messageCounts {
		b.Run(fmt.Sprintf("Messages_%d", count), func(b *testing.B) {
			// Ensure we don't exceed available traces
			actualCount := min(count, len(traces))

			b.ResetTimer()
			for b.Loop() {
				ctx, cancel := context.WithCancel(b.Context())
				collector := New(ctx, zap.NewNop(), vstore, mockDomainDataProvider{}, dutyStore, networkconfig.TestNetwork.Beacon)

				var wg sync.WaitGroup
				for _, msg := range traces[:actualCount] {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_ = collector.Collect(b.Context(), msg, dummyVerify)
					}()
				}
				wg.Wait()
				cancel()
			}
		})
	}
}

func dummyVerify(*spectypes.PartialSignatureMessages) error {
	return nil
}

func readByteSlices(file *os.File) (result []*queue.SSVMessage, err error) {
	var header [2]byte
	r := io.NewSectionReader(file, 0, math.MaxInt64)

	for {
		_, err := r.Read(header[:])
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}

		length := int(header[0])<<8 | int(header[1])

		diskMsgBytes := make([]byte, length)
		_, err = r.Read(diskMsgBytes)
		if err != nil {
			return nil, err
		}

		dataMsg := new(model.DiskMsg)
		if err := dataMsg.UnmarshalSSZ(diskMsgBytes); err != nil {
			return nil, err
		}

		msg := &queue.SSVMessage{
			SignedSSVMessage: &dataMsg.Signed,
			SSVMessage:       &dataMsg.Spec,
		}

		if dataMsg.Kind == 0 {
			msg.Body = &dataMsg.Qbft
		} else {
			msg.Body = &dataMsg.Sig
		}

		result = append(result, msg)
	}
}

type mockDomainDataProvider struct{}

func (m mockDomainDataProvider) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, errors.New("not implemented")
}
