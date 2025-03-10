package validator

import (
	"context"
	"io"
	"math"
	"os"
	"sync"
	"testing"

	"go.uber.org/zap"

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
		tracer := NewTracer(ctx, zap.NewNop(), vstore, nil, dutyStore, "BN", true)

		until = min(i*100, len(traces))

		var wg sync.WaitGroup
		for _, msg := range traces[:until] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tracer.Trace(context.Background(), msg)
			}()
		}
		wg.Wait()
		cancel()
	}
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

// f, err := os.OpenFile("../../data/ssvtraces25m.bin", os.O_RDONLY, 0644)

func TestBin(t *testing.T) {
	file, err := os.OpenFile("../../data/ssvtraces25m.bin", os.O_RDONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	out, err := os.OpenFile("../../data/slot_3707881_3707882.bin", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	var header [2]byte
	r := io.NewSectionReader(file, 0, math.MaxInt64)

	for {
		_, err := r.Read(header[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}

		length := int(header[0])<<8 | int(header[1])

		data := make([]byte, length)
		_, err = r.Read(data)
		if err != nil {
			panic(err)
		}

		dataMsg := new(model.DiskMsg)
		if err := dataMsg.UnmarshalSSZ(data); err != nil {
			panic(err)
		}

		var slot uint64

		if dataMsg.Qbft.Height > 0 {
			slot = uint64(dataMsg.Qbft.Height)
		} else if dataMsg.Sig.Slot > 0 {
			slot = uint64(dataMsg.Sig.Slot)
		} else {
			panic("no slot found")
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

		if slot == 3707881 || slot == 3707882 {
			if _, err := out.Write(header[:]); err != nil {
				panic("write header to file")
			}

			// Write the byte slice itself
			if _, err := out.Write(data); err != nil {
				panic("write data to file")
			}
		}

		// t.Log(msg)
	}
}
