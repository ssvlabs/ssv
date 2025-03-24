package validator

import (
	"context"
	"io"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func BenchmarkTracer(b *testing.B) {
	f, err := os.OpenFile("./benchdata/slot_3707881_3707882.ssz", os.O_RDONLY, 0644)
	if err != nil {
		b.Fatal(err)
	}

	traces, err := readByteSlices(f)
	if err != nil {
		b.Fatal(err)
	}

	_ = f.Close()

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		b.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(db, nil)

	b.ResetTimer()

	var until, i int
	for i = 1; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		collector := New(ctx, zap.NewNop(), vstore, mockDomainDataProvider{}, dutyStore, "BN")

		until = min(i*100, len(traces))

		var wg sync.WaitGroup
		for _, msg := range traces[:until] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				collector.Collect(context.Background(), msg, dummyVerify)
			}()
		}
		wg.Wait()
		cancel()
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

func (m mockDomainDataProvider) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, errors.New("not implemented")
}
