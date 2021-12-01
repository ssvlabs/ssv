package collections

import (
	"encoding/binary"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
	"strings"
)

// Iibft is an interface for persisting chain data
type Iibft interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier []byte, state *proto.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier []byte) (*proto.State, bool, error)
	// SaveDecided saves a signed message for an ibft instance with decided justification
	SaveDecided(signedMsg *proto.SignedMessage) error
	// GetDecided returns a signed message for an ibft instance which decided by identifier
	GetDecided(identifier []byte, seqNumber uint64) (*proto.SignedMessage, bool, error)
	// GetDecidedInRange returns decided message in the given range
	GetDecidedInRange(identifier []byte, from uint64, to uint64) ([]*proto.SignedMessage, error)
	// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
	SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error
	// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
	GetHighestDecidedInstance(identifier []byte) (*proto.SignedMessage, bool, error)
}

var (
	metricsHighestDecided = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_highest_decided",
		Help: "The highest decided sequence number",
	}, []string{"lambda", "pubKey"})
)

func init() {
	if err := prometheus.Register(metricsHighestDecided); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// IbftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type IbftStorage struct {
	prefix []byte
	db     basedb.IDb
	logger *zap.Logger
}

// NewIbft create new ibft storage
func NewIbft(db basedb.IDb, logger *zap.Logger, instanceType string) IbftStorage {
	ibft := IbftStorage{
		prefix: []byte(instanceType),
		db:     db,
		logger: logger,
	}
	return ibft
}

// SaveCurrentInstance func implementation
func (i *IbftStorage) SaveCurrentInstance(identifier []byte, state *proto.State) error {
	value, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, "current", identifier)
}

// GetCurrentInstance func implementation
func (i *IbftStorage) GetCurrentInstance(identifier []byte) (*proto.State, bool, error) {
	val, found, err := i.get("current", identifier)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, false, err
	}
	ret := &proto.State{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, false, nil
}

// SaveDecided func implementation
func (i *IbftStorage) SaveDecided(signedMsg *proto.SignedMessage) error {
	value, err := json.Marshal(signedMsg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, "decided", signedMsg.Message.Lambda, uInt64ToByteSlice(signedMsg.Message.SeqNumber))
}

// GetDecided returns a signed message for an ibft instance which decided by identifier
func (i *IbftStorage) GetDecided(identifier []byte, seqNumber uint64) (*proto.SignedMessage, bool, error) {
	val, found, err := i.get("decided", identifier, uInt64ToByteSlice(seqNumber))
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, found, nil
}

// GetDecidedInRange returns decided message in the given range
func (i *IbftStorage) GetDecidedInRange(identifier []byte, from uint64, to uint64) ([]*proto.SignedMessage, error) {
	prefix := append(i.prefix[:], identifier...)

	var sequences [][]byte
	for seq := from; seq <= to; seq++ {
		sequences = append(sequences, i.key("decided", uInt64ToByteSlice(seq)))
	}
	msgs := make([]*proto.SignedMessage, 0)
	results, err := i.db.GetMany(prefix, sequences...)
	if err != nil {
		return msgs, err
	}
	for _, res := range results {
		msg := &proto.SignedMessage{}
		if err := json.Unmarshal(res.Value, msg); err != nil {
			return nil, errors.Wrap(err, "un-marshaling error")
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
func (i *IbftStorage) SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error {
	value, err := json.Marshal(signedMsg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}

	if err = i.save(value, "highest", signedMsg.Message.Lambda); err != nil {
		return err
	}
	reportHighestDecided(signedMsg)

	return nil
}

func reportHighestDecided(signedMsg *proto.SignedMessage) {
	l := string(signedMsg.Message.Lambda)
	// in order to extract the public key, the role (e.g. '_ATTESTER') is removed
	if idx := strings.Index(l, "_"); idx > 0 {
		pubKey := l[:idx]
		metricsHighestDecided.WithLabelValues(string(signedMsg.Message.Lambda), pubKey).
			Set(float64(signedMsg.Message.SeqNumber))
	}
}

// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
func (i *IbftStorage) GetHighestDecidedInstance(identifier []byte) (*proto.SignedMessage, bool, error) {
	val, found, err := i.get("highest", identifier)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, found, nil
}

func (i *IbftStorage) save(value []byte, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Set(prefix, key, value)
}

func (i *IbftStorage) get(id string, pk []byte, keyParams ...[]byte) ([]byte, bool, error) {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	obj, found, err := i.db.Get(prefix, key)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	return obj.Value, found, nil
}

func (i *IbftStorage) key(id string, params ...[]byte) []byte {
	ret := make([]byte, 0)
	ret = append(ret, []byte(id)...)
	for _, p := range params {
		ret = append(ret, p...)
	}
	return ret
}

func uInt64ToByteSlice(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}
