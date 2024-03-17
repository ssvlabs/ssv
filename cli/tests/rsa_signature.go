package tests

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/executionclient"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/logging"
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
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"math/rand"
	"time"
)

type messageRouter struct {
	logger *zap.Logger
	ch     chan *queue.DecodedSSVMessage
}

func (r *messageRouter) Route(ctx context.Context, message *queue.DecodedSSVMessage) {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan *queue.DecodedSSVMessage {
	return r.ch
}

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options                   `yaml:"db"`
	SSVOptions                 operator.Options                 `yaml:"ssv"`
	ExecutionClient            executionclient.ExecutionOptions `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient            beaconprotocol.Options           `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig           p2pv1.Config                     `yaml:"p2p"`
	OperatorPrivateKey         string                           `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	MetricsAPIPort             int                              `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"Port to listen on for the metrics API."`
	EnableProfile              bool                             `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string                           `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`
	WsAPIPort                  int                              `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"Port to listen on for the websocket API."`
	WithPing                   bool                             `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`
	SSVAPIPort                 int                              `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port to listen on for the SSV API."`
	LocalEventsPath            string                           `yaml:"LocalEventsPath" env:"EVENTS_PATH" env-description:"path to local events"`
}

var cfg config
var globalArgs global_config.Args

// RsaCmd is the command to rsa test
var RsaCmd = &cobra.Command{
	Use:   "test-rsa",
	Short: "Test Rsa Wrong signature",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal()
		ctx := context.Background()
		if err != nil {
			log.Fatal("could not create logger", err)
		}

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

		if err != nil {
			logger.Fatal("could not connect to execution client", zap.Error(err))
		}

		var validatorCtrl validator.Controller
		cfg.P2pNetworkConfig.Permissioned = func() bool {
			return false
		}

		cfg.P2pNetworkConfig.NodeStorage = nodeStorage
		cfg.P2pNetworkConfig.OperatorPubKeyHash = format.OperatorID(operatorData.PublicKey)
		operatorId := 0
		cfg.P2pNetworkConfig.OperatorID = func() spectypes.OperatorID {
			operatorId++
			return spectypes.OperatorID(operatorId)
		}
		cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode
		cfg.P2pNetworkConfig.Network = networkConfig

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
		messageRoute := &messageRouter{
			logger: logger,
			ch:     make(chan *queue.DecodedSSVMessage, 1024),
		}
		p2pNetwork.UseMessageRouter(messageRoute)
		go func() {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			ch := messageRoute.GetMessageChan()
			sendMessageCount := 0
			for {
				select {
				case <-ctx.Done():
					logger.Debug("router message handler stopped")
					return
				case incomingMessage := <-ch:
					ssvMessage := incomingMessage.SSVMessage
					if mm, ok := incomingMessage.Body.(*spectypes.SignedPartialSignatureMessage); ok {
						// Seed the random number generator
						// #nosec G404
						seed := time.Now().UnixNano() // Using current time as a seed for demonstration
						// #nosec G404
						localRand := rand.New(rand.NewSource(seed))

						// Generate a random number, for example between 0 and 9
						// #nosec G404
						dynamicValue := localRand.Intn(10) // This will give you a random integer between 0 and 9

						// Using fmt.Sprintf to insert the dynamic value into the string
						signatureString := fmt.Sprintf("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc8uoaM5WpjbiYvvxUr1eCpz0ja7UT%dPGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv", dynamicValue)

						// Converting the formatted string to a byte slice
						signature := []byte(signatureString)
						mm.Signature = signature
						incomingMessage.Data, err = mm.Encode()
						incomingMessage.Body, err = incomingMessage.Encode()
						if err != nil {
							logger.Fatal("failed to encode message", zap.Error(err))
						}

						if sendMessageCount < 4 {
							err = p2pNetwork.Broadcast(ssvMessage)
							if err != nil {
								logger.Fatal("failed to broadcast message", zap.Error(err))
							}
							sendMessageCount++
						}
					}
				}
			}
		}()
		err = p2pNetwork.SubscribeAll(logger)
		if err != nil {
			logger.Fatal("can't subscribe all (rsa)", zap.Error(err))
		}
		p2pNetwork.UpdateSubnets(logger)
	},
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
			ID:        4,
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

func setupGlobal() (*zap.Logger, error) {
	if globalArgs.ConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read config: %w", err)
		}
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read share config: %w", err)
		}
	}

	err := logging.SetGlobalLogger(
		cfg.LogLevel,
		cfg.LogLevelFormat,
		cfg.LogFormat,
		&logging.LogFileOptions{
			FileName:   cfg.LogFilePath,
			MaxSize:    cfg.LogFileSize,
			MaxBackups: cfg.LogFileBackups,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("logging.SetGlobalLogger: %w", err)
	}

	return zap.L(), nil
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, RsaCmd)
}
