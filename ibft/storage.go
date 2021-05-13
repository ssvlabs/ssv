package ibft
//
//import (
//	"encoding/binary"
//	"encoding/json"
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/storage/basedb"
//	"go.uber.org/zap"
//)
//
//// ICollection interface for IBFT storage
//type ICollection interface {
//	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
//	SaveCurrentInstance(state *proto.State) error
//	// GetCurrentInstance returns the state for the current running (not yet decided) instance
//	GetCurrentInstance(pk []byte) (*proto.State, error)
//	// SaveDecided saves a signed message for an ibft instance with decided justification
//	SaveDecided(signedMsg *proto.SignedMessage) error
//	// GetDecided returns a signed message for an ibft instance which decided by identifier
//	GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error)
//	// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
//	SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error
//	// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
//	GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error)
//}
//
//type collectionOptions struct {
//	DB     *basedb.IDb
//	Logger *zap.Logger
//}
//
//// Collection struct
//// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
//type Collection struct {
//	db     basedb.IDb
//	logger *zap.Logger
//	prefix []byte
//	instanceType string
//}
//
//// NewCollection creates new share storage
//func NewCollection(options collectionOptions) ICollection {
//	collection := Collection{
//		db: *options.DB,
//		logger: options.Logger,
//		prefix: []byte(GetCollectionPrefix()),
//	}
//	return &collection
//}
//func GetCollectionPrefix() string {
//	return "ibft"
//}
//
//// SaveCurrentInstance func implementation
//func (i *Collection) SaveCurrentInstance(state *proto.State) error {
//	value, err := json.Marshal(state)
//	if err != nil {
//		i.logger.Error("failed serializing state", zap.Error(err))
//	}
//	return i.save(value, "current", state.ValidatorPk)
//}
//
//// GetCurrentInstance func implementation
//func (i *Collection) GetCurrentInstance(pk []byte) (*proto.State, error) {
//	val, err := i.get("current", pk)
//	if err != nil {
//		return nil, err
//	}
//	ret := &proto.State{}
//	if err := json.Unmarshal(val, ret); err != nil {
//		return nil, err
//	}
//	return ret, nil
//}
//
//// SaveDecided func implementation
//func (i *Collection) SaveDecided(signedMsg *proto.SignedMessage) error {
//	value, err := json.Marshal(signedMsg)
//	if err != nil {
//		i.logger.Error("failed serializing decided msg", zap.Error(err))
//	}
//	return i.save(value, "decided", signedMsg.Message.ValidatorPk, uInt64ToByteSlice(signedMsg.Message.SeqNumber))
//}
//
//// GetDecided returns a signed message for an ibft instance which decided by identifier
//func (i *Collection) GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error) {
//	val, err := i.get("decided", pk, uInt64ToByteSlice(seqNumber))
//	if err != nil {
//		return nil, err
//	}
//	ret := &proto.SignedMessage{}
//	if err := json.Unmarshal(val, ret); err != nil {
//		return nil, err
//	}
//	return ret, nil
//}
//
//// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
//func (i *Collection) SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error {
//	value, err := json.Marshal(signedMsg)
//	if err != nil {
//		i.logger.Error("failed serializing state", zap.Error(err))
//	}
//	return i.save(value, "highest", signedMsg.Message.ValidatorPk)
//}
//
//// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
//func (i *Collection) GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error) {
//	val, err := i.get("highest", pk)
//	if err != nil {
//		return nil, err
//	}
//	ret := &proto.SignedMessage{}
//	if err := json.Unmarshal(val, ret); err != nil {
//		return nil, err
//	}
//	return ret, nil
//}
//
//func (i *Collection) save(value []byte, id string, pk []byte, keyParams ...[]byte) error {
//	key := i.key(id, pk, keyParams...)
//	return i.db.Set(i.prefix, key, value)
//}
//
//func (i *Collection) get(id string, pk []byte, keyParams ...[]byte) ([]byte, error) {
//	key := i.key(id, pk, keyParams...)
//	obj, err := i.db.Get(i.prefix, key)
//	if err != nil {
//		return nil, err
//	}
//	return obj.Value, nil
//}
//
//func (i *Collection) key(id string, pk []byte, params ...[]byte) []byte {
//	ret := make([]byte, 0)
//	ret = append(ret, []byte(i.instanceType)...)
//	ret = append(ret, []byte(id)...)
//	ret = append(ret, pk...)
//	for _, p := range params {
//		ret = append(ret, p...)
//	}
//	return ret
//}
//
//func uInt64ToByteSlice(n uint64) []byte {
//	b := make([]byte, 8)
//	binary.LittleEndian.PutUint64(b, n)
//	return b
//}
