package p2p

import (
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	prefix = []byte("p2p-")

	privateKeyKey = []byte("private-key")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	GetPrivateKey() (*ecdsa.PrivateKey, bool, error)
	SavePrivateKey(privateKey *ecdsa.PrivateKey) error
	SetupPrivateKey(NetworkPrivateKey string) error
}

type storage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewP2PStorage creates a new instance of Storage
func NewP2PStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := storage{db, logger}
	return &es
}

func (s storage) GetPrivateKey() (*ecdsa.PrivateKey, bool, error) {
	obj, found, err := s.db.Get(prefix, privateKeyKey)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	pk, err := decode(obj.Value)
	pk.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	if err != nil {
		return nil, found, errors.WithMessage(err, "failed to decode private key")
	}
	return pk, found, nil
}

func (s storage) SavePrivateKey(privateKey *ecdsa.PrivateKey) error {
	if err := s.db.Set(prefix, privateKeyKey, encode(privateKey)); err != nil {
		return errors.WithMessage(err, "failed to save to db")
	}
	return nil
}

func (s storage) SetupPrivateKey(networkPrivateKey string) error {
	existKey, found, err := s.GetPrivateKey()
	if err != nil{
		return errors.WithMessage(err, "failed to get privateKey")
	}
	if networkPrivateKey == "" && found && existKey != nil{
		s.logger.Debug("using p2p network privateKey from storage")
		return nil
	}
	privateKey, err := utils.ECDSAPrivateKey(s.logger.With(zap.String("who", "p2p storage")), networkPrivateKey)
	if err != nil{
		return errors.WithMessage(err, "failed to generate private key")
	}

	return s.SavePrivateKey(privateKey)
}

func encode(privateKey *ecdsa.PrivateKey) []byte {
	return gcrypto.FromECDSA(privateKey)
}

func decode(b []byte) (*ecdsa.PrivateKey, error) {
	return gcrypto.ToECDSA(b)
}