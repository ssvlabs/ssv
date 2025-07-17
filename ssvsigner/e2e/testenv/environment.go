package testenv

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/storage/basedb"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// TestEnvironment manages the complete E2E test infrastructure
type TestEnvironment struct {
	// Container references
	postgresContainer   testcontainers.Container
	web3SignerContainer testcontainers.Container
	ssvSignerContainer  testcontainers.Container

	// Essential components
	postgresDB       *sql.DB
	ssvSignerClient  *ssvsigner.Client
	web3SignerClient *web3signer.Web3Signer
	localKeyManager  *ekm.LocalKeyManager
	remoteKeyManager *ekm.RemoteKeyManager
	localDB          basedb.Database
	remoteDB         basedb.Database

	// Mock beacon for controlled testing
	mockController *gomock.Controller
	mockBeacon     *networkconfig.MockBeacon

	// Network
	networkName string

	// Volumes
	web3SignerVolume testcontainers.ContainerMount
	postgresVolume   testcontainers.ContainerMount

	// Configuration
	postgresConnStr      string
	ssvSignerURL         string
	web3SignerURL        string
	migrationsPath       string
	localKeyManagerPath  string
	remoteKeyManagerPath string

	// TLS certificates
	certDir            string
	web3SignerCertPath string
	ssvSignerCertPath  string
	e2eClientCertPath  string

	// Operator key for validator encryption
	operatorKey keys.OperatorPrivateKey

	// Context for container lifecycle
	ctx context.Context
}

// NewTestEnvironment creates a new test environment
func NewTestEnvironment(ctx context.Context, t gomock.TestReporter) (*TestEnvironment, error) {
	env := &TestEnvironment{
		ctx:            ctx,
		migrationsPath: "../testdata/migrations",
	}

	operatorKey, err := common.GenerateOperatorKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate operator key: %w", err)
	}
	env.operatorKey = operatorKey

	if err := env.setupWeb3SignerVolume(); err != nil {
		return nil, fmt.Errorf("failed to setup Web3Signer volume: %w", err)
	}

	if err := env.setupPostgreSQLVolume(); err != nil {
		return nil, fmt.Errorf("failed to setup PostgreSQL volume: %w", err)
	}

	if err := env.setupKeyManagerVolumes(); err != nil {
		return nil, fmt.Errorf("failed to setup key manager volumes: %w", err)
	}

	if err := env.setupTLSCertificates(); err != nil {
		return nil, fmt.Errorf("failed to setup TLS certificates: %w", err)
	}

	if err := env.setupMockBeacon(t); err != nil {
		return nil, fmt.Errorf("failed to setup mock beacon: %w", err)
	}

	return env, nil
}

// Start initializes and starts all containers and services
func (env *TestEnvironment) Start() error {
	if err := env.startContainers(); err != nil {
		return fmt.Errorf("failed to start containers: %w", err)
	}

	if err := env.initializeKeyManagers(); err != nil {
		return fmt.Errorf("failed to initialize key managers: %w", err)
	}

	return nil
}

// Stop gracefully stops all containers and closes connections
func (env *TestEnvironment) Stop() error {
	var errors []error

	containers := []testcontainers.Container{
		env.ssvSignerContainer,
		env.web3SignerContainer,
		env.postgresContainer,
	}

	for _, container := range containers {
		if container != nil {
			if err := container.Terminate(env.ctx); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if env.postgresDB != nil {
		if err := env.postgresDB.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close PostgreSQL database: %w", err))
		}
	}

	if env.localDB != nil {
		if err := env.localDB.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close local database: %w", err))
		}
	}

	if env.remoteDB != nil {
		if err := env.remoteDB.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close remote database: %w", err))
		}
	}

	// Clean up key manager directories
	if err := os.RemoveAll(env.localKeyManagerPath); err != nil {
		errors = append(errors, fmt.Errorf("failed to cleanup local key manager directory: %w", err))
	}

	if err := os.RemoveAll(env.remoteKeyManagerPath); err != nil {
		errors = append(errors, fmt.Errorf("failed to cleanup remote key manager directory: %w", err))
	}

	// Clean up TLS certificates directory
	if err := os.RemoveAll(env.certDir); err != nil {
		errors = append(errors, fmt.Errorf("failed to cleanup TLS certificates directory: %w", err))
	}

	if env.mockController != nil {
		env.mockController.Finish()
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}
	return nil
}

// RestartWeb3Signer stops and restarts the Web3Signer container while preserving the PostgreSQL database
func (env *TestEnvironment) RestartWeb3Signer() error {
	if env.web3SignerContainer != nil {
		if err := env.web3SignerContainer.Terminate(env.ctx); err != nil {
			return fmt.Errorf("failed to stop Web3Signer container: %w", err)
		}
		env.web3SignerContainer = nil
	}

	return env.startWeb3Signer()
}

// RestartSSVSigner stops and restarts the SSV-Signer container
func (env *TestEnvironment) RestartSSVSigner() error {
	if env.ssvSignerContainer != nil {
		if err := env.ssvSignerContainer.Terminate(env.ctx); err != nil {
			return fmt.Errorf("failed to stop SSV-Signer container: %w", err)
		}
		env.ssvSignerContainer = nil
	}

	return env.startSSVSigner()
}

// RestartPostgreSQL stops and restarts the PostgreSQL container with persistent data
func (env *TestEnvironment) RestartPostgreSQL() error {
	if env.postgresContainer != nil {
		if err := env.postgresContainer.Terminate(env.ctx); err != nil {
			return fmt.Errorf("failed to stop PostgreSQL container: %w", err)
		}
		env.postgresContainer = nil
	}

	if env.postgresDB != nil {
		if err := env.postgresDB.Close(); err != nil {
			return fmt.Errorf("failed to close existing PostgreSQL connection during restart: %w", err)
		}
		env.postgresDB = nil
	}

	if err := env.startPostgreSQL(); err != nil {
		return fmt.Errorf("failed to restart PostgreSQL: %w", err)
	}

	// Wait for Web3Signer to be healthy and ready to sign
	// We need to wait until Web3Signer detects PostgreSQL is back
	return env.waitForWeb3SignerReady()
}

// GetSSVSignerClient returns the SSV-Signer client
func (env *TestEnvironment) GetSSVSignerClient() *ssvsigner.Client {
	return env.ssvSignerClient
}

// GetLocalKeyManager returns the local key manager
func (env *TestEnvironment) GetLocalKeyManager() *ekm.LocalKeyManager {
	return env.localKeyManager
}

// GetRemoteKeyManager returns the remote key manager
func (env *TestEnvironment) GetRemoteKeyManager() *ekm.RemoteKeyManager {
	return env.remoteKeyManager
}

// GetMockBeacon returns the mock beacon for test control
func (env *TestEnvironment) GetMockBeacon() *networkconfig.MockBeacon {
	return env.mockBeacon
}

// GetOperatorKey returns the operator key for validator encryption
func (env *TestEnvironment) GetOperatorKey() keys.OperatorPrivateKey {
	return env.operatorKey
}

// GetWeb3SignerClient returns the Web3Signer client for direct testing
func (env *TestEnvironment) GetWeb3SignerClient() *web3signer.Web3Signer {
	return env.web3SignerClient
}

// Implement signerClient interface by delegating to ssvSignerClient
func (env *TestEnvironment) AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) (statuses []web3signer.Status, err error) {
	return env.ssvSignerClient.AddValidators(ctx, shares...)
}

func (env *TestEnvironment) RemoveValidators(ctx context.Context, sharePubKeys ...phase0.BLSPubKey) (statuses []web3signer.Status, err error) {
	return env.ssvSignerClient.RemoveValidators(ctx, sharePubKeys...)
}

func (env *TestEnvironment) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error) {
	return env.ssvSignerClient.Sign(ctx, sharePubKey, payload)
}

func (env *TestEnvironment) OperatorIdentity(ctx context.Context) (string, error) {
	return env.ssvSignerClient.OperatorIdentity(ctx)
}

func (env *TestEnvironment) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	return env.ssvSignerClient.OperatorSign(ctx, payload)
}

// setupWeb3SignerVolume creates a persistent volume for Web3Signer keystore data
func (env *TestEnvironment) setupWeb3SignerVolume() error {
	volumeName := fmt.Sprintf("web3signer-data-%s", randomSuffix())
	env.web3SignerVolume = testcontainers.VolumeMount(volumeName, "/opt/web3signer")
	fmt.Printf("Created Web3Signer volume: %s (mounting to /opt/web3signer)\n", volumeName)
	return nil
}

// setupPostgreSQLVolume creates a persistent volume for PostgreSQL database data
func (env *TestEnvironment) setupPostgreSQLVolume() error {
	volumeName := fmt.Sprintf("postgres-data-%s", randomSuffix())
	env.postgresVolume = testcontainers.VolumeMount(volumeName, "/var/lib/postgresql/data")
	fmt.Printf("Created PostgreSQL volume: %s (mounting to /var/lib/postgresql/data)\n", volumeName)
	return nil
}

// setupKeyManagerVolumes creates temporary directories for LocalKeyManager and RemoteKeyManager BadgerDB data
func (env *TestEnvironment) setupKeyManagerVolumes() error {
	env.localKeyManagerPath = fmt.Sprintf("/tmp/local-keymanager-data-%s", randomSuffix())
	if err := os.MkdirAll(env.localKeyManagerPath, dirMode); err != nil {
		return fmt.Errorf("failed to create local key manager directory: %w", err)
	}
	fmt.Printf("Created LocalKeyManager directory: %s\n", env.localKeyManagerPath)

	env.remoteKeyManagerPath = fmt.Sprintf("/tmp/remote-keymanager-data-%s", randomSuffix())
	if err := os.MkdirAll(env.remoteKeyManagerPath, dirMode); err != nil {
		return fmt.Errorf("failed to create remote key manager directory: %w", err)
	}
	fmt.Printf("Created RemoteKeyManager directory: %s\n", env.remoteKeyManagerPath)

	return nil
}

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
