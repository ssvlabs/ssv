package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

type RsaSignature struct {
}
type config struct {
	DBOptions          basedb.Options   `yaml:"db"`
	P2pNetworkConfig   p2pv1.Config     `yaml:"p2p"`
	SSVOptions         operator.Options `yaml:"ssv"`
	SSVAPIPort         int              `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port to listen on for the SSV API."`
	NetworkPrivateKey  string           `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`
	OperatorPrivateKey string           `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
}

var cfg config
var operatorNode operator.Node
var globalArgs global_config.Args

//type messageRouter struct {
//	logger *zap.Logger
//	ch     chan *queue.DecodedSSVMessage
//}
//
//func (r *messageRouter) Route(ctx context.Context, message *queue.DecodedSSVMessage) {
//	println("works!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
//	println(message.MsgType)
//	println(len(r.ch))
//	println("works!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
//	select {
//	case <-ctx.Done():
//		r.logger.Warn("context canceled, dropping message")
//	case r.ch <- message:
//	default:
//		r.logger.Warn("message router buffer is full, dropping message")
//	}
//}

func (cmd *RsaSignature) Run(logger *zap.Logger, globals Globals) error {
	ctx := context.Background()
	//defer cancel()

	err := setupGlobal(globals.ConfigFile, globals.ShareFile)
	if err != nil {
		logger.Fatal("fuck!!!", zap.Error(err))
	}
	logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

	metricsReporter := metricsreporter.New(
		metricsreporter.WithLogger(logger),
	)

	networkConfig, err := setupSSVNetwork(logger)
	if err != nil {
		logger.Fatal("could not setup network", zap.Error(err))
	}
	cfg.DBOptions.Ctx = ctx
	db, err := setupDB(logger, networkConfig.Beacon.GetNetwork())
	if err != nil {
		logger.Fatal("could not setup db", zap.Error(err))
	}

	nodeStorage, operatorData := setupOperatorStorage(logger, db)

	operatorKey, _, _ := nodeStorage.GetPrivateKey()
	keyBytes := x509.MarshalPKCS1PrivateKey(operatorKey)
	hashedKey, _ := rsaencryption.HashRsaKey(keyBytes)
	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, cfg.SSVOptions.ValidatorOptions.BuilderProposals, hashedKey)
	if err != nil {
		logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
	}

	cfg.P2pNetworkConfig.Ctx = ctx

	permissioned := func() bool {
		currentEpoch := networkConfig.Beacon.EstimatedCurrentEpoch()
		return currentEpoch < networkConfig.PermissionlessActivationEpoch
	}

	if err != nil {
		logger.Fatal("could not connect to execution client", zap.Error(err))
	}

	var validatorCtrl validator.Controller
	cfg.P2pNetworkConfig.Permissioned = permissioned
	cfg.P2pNetworkConfig.NodeStorage = nodeStorage
	cfg.P2pNetworkConfig.OperatorPubKeyHash = format.OperatorID(operatorData.PublicKey)
	cfg.P2pNetworkConfig.OperatorID = func() spectypes.OperatorID {
		return validatorCtrl.GetOperatorData().ID
	}
	cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode
	cfg.P2pNetworkConfig.Network = networkConfig

	validatorsMap := validatorsmap.New(ctx)

	dutyStore := dutystore.New()
	cfg.SSVOptions.DutyStore = dutyStore

	messageValidator := validation.NewMessageValidator(
		networkConfig,
		validation.WithLogger(logger),
		validation.WithMetrics(metricsReporter),
		validation.WithOwnOperatorID(operatorData.ID),
	)

	cfg.P2pNetworkConfig.Metrics = metricsReporter
	cfg.P2pNetworkConfig.MessageValidator = messageValidator
	cfg.SSVOptions.ValidatorOptions.MessageValidator = messageValidator

	p2pNetwork := setupP2P(logger, db, metricsReporter)

	cfg.SSVOptions.DB = db
	cfg.SSVOptions.Context = ctx
	cfg.SSVOptions.Network = networkConfig
	cfg.SSVOptions.P2PNetwork = p2pNetwork
	cfg.SSVOptions.ValidatorOptions.DB = db
	cfg.SSVOptions.ValidatorOptions.Context = ctx
	cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
	cfg.SSVOptions.ValidatorOptions.KeyManager = keyManager
	cfg.SSVOptions.ValidatorOptions.OperatorData = operatorData
	cfg.SSVOptions.ValidatorOptions.ValidatorsMap = validatorsMap
	cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
	cfg.SSVOptions.ValidatorOptions.BeaconNetwork = networkConfig.Beacon.GetNetwork()
	cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = nodeStorage.GetPrivateKey

	validatorCtrl = validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
	cfg.SSVOptions.ValidatorController = validatorCtrl
	if err := p2pNetwork.Setup(logger); err != nil {
		logger.Fatal("failed to setup network", zap.Error(err))
	}
	if err := p2pNetwork.Start(logger); err != nil {
		logger.Fatal("failed to start network", zap.Error(err))
	}

	err = p2pNetwork.SubscribeAll(logger)
	if err != nil {
		return err
	}

	//messageRoute := &messageRouter{
	//	logger: logger,
	//	ch:     make(chan *queue.DecodedSSVMessage, 1024),
	//}
	//p2pNetwork.UseMessageRouter(messageRoute)
	//ch := messageRoute.ch
	//ticker := time.NewTicker(15 * time.Second)
	//defer ticker.Stop() // This is important to stop the ticker when the function exits
	//
	//for {
	//	select {
	//	case <-ticker.C:
	//	}
	//}
	//validatorCtrl.StartNetworkHandlers()

	time.Sleep(1000 * time.Second)
	return nil

	//roleAttester := spectypes.BNRoleAttester
	//sk1Str := "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	//sk1 := &bls.SecretKey{}
	//err = sk1.SetHexString(sk1Str)
	//if err != nil {
	//	return err
	//}
	//msgID := spectypes.NewMsgID(networkConfig.Domain, sk1.GetPublicKey().Serialize(), roleAttester)
	//logger.Info("before send>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	//logger.Info(msgID.String())
	//logger.Info(sk1.GetPublicKey().GetHexString())
	//logger.Info("before send>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	//message := &spectypes.SSVMessage{
	//	MsgID:   msgID,
	//	MsgType: spectypes.SSVConsensusMsgType,
	//	Data:    bytes.Repeat([]byte{1}, 256),
	//}
	//time.Sleep(10 * time.Second)
	//err = p2pNetwork.Broadcast(message)
	//if err != nil {
	//	println("here>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	//	println(err.Error())
	//	println("here>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	//	return err
	//}
	//logger.Info("send>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	cfg.P2pNetworkConfig.OperatorPrivateKey, err = decodePrivateKey(cfg.OperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not decode operator private key", zap.Error(err))
	}

	operatorPubKey, err := nodeStorage.SetupPrivateKey(cfg.OperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, operatorPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: operatorPubKey,
		}
	}

	return nodeStorage, operatorData
}

func decodePrivateKey(key string) (*rsa.PrivateKey, error) {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	sk, err := rsaencryption.ConvertPemToPrivateKey(string(operatorKeyByte))
	if err != nil {
		return nil, err
	}

	return sk, err
}

func setupDB(logger *zap.Logger, eth2Network beaconprotocol.Network) (*kv.BadgerDB, error) {
	db, err := kv.New(logger, cfg.DBOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open db")
	}
	reopenDb := func() error {
		if err := db.Close(); err != nil {
			return errors.Wrap(err, "failed to close db")
		}
		db, err = kv.New(logger, cfg.DBOptions)
		return errors.Wrap(err, "failed to reopen db")
	}

	migrationOpts := migrations.Options{
		Db:      db,
		DbPath:  cfg.DBOptions.Path,
		Network: eth2Network,
	}
	applied, err := migrations.Run(cfg.DBOptions.Ctx, logger, migrationOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run migrations")
	}
	if applied == 0 {
		return db, nil
	}

	// If migrations were applied, we run a full garbage collection cycle
	// to reclaim any space that may have been freed up.
	// Close & reopen the database to trigger any unknown internal
	// startup/shutdown procedures that the storage engine may have.
	start := time.Now()
	if err := reopenDb(); err != nil {
		return nil, err
	}
	// Run a long garbage collection cycle with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := db.FullGC(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to collect garbage")
	}

	// Close & reopen again.
	if err := reopenDb(); err != nil {
		return nil, err
	}
	logger.Debug("post-migrations garbage collection completed", fields.Duration(start))

	return db, nil
}

func setupSSVNetwork(logger *zap.Logger) (networkconfig.NetworkConfig, error) {
	networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.SSVOptions.NetworkName)
	if err != nil {
		return networkconfig.NetworkConfig{}, err
	}

	types.SetDefaultDomain(networkConfig.Domain)

	nodeType := "light"
	if cfg.SSVOptions.ValidatorOptions.FullNode {
		nodeType = "full"
	}
	builderProposals := "disabled"
	if cfg.SSVOptions.ValidatorOptions.BuilderProposals {
		builderProposals = "enabled"
	}

	logger.Info("setting ssv network",
		fields.Network(networkConfig.Name),
		fields.Domain(networkConfig.Domain),
		zap.String("nodeType", nodeType),
		zap.String("builderProposals(MEV)", builderProposals),
		zap.Any("beaconNetwork", networkConfig.Beacon.GetNetwork().BeaconNetwork),
		zap.Uint64("genesisEpoch", uint64(networkConfig.GenesisEpoch)),
		zap.String("registryContract", networkConfig.RegistryContractAddr),
	)

	return networkConfig, nil
}

func setupP2P(logger *zap.Logger, db basedb.Database, mr metricsreporter.MetricsReporter) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db)
	netPrivKey, err := istore.SetupNetworkKey(logger, cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}
	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey

	return p2pv1.New(logger, &cfg.P2pNetworkConfig, mr)
}

func setupGlobal(configPath string, sharePath string) error {
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		return fmt.Errorf("could not read config: %w", err)
	}
	if err := cleanenv.ReadConfig(sharePath, &cfg); err != nil {
		return fmt.Errorf("could not read share config: %w", err)
	}
	return nil
}
