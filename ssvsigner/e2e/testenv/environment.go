package testenv

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
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
	web3SignerPostgresDB *sql.DB
	ssvSignerClient      *ssvsigner.Client
	web3SignerClient     *web3signer.Web3Signer
	localKeyManager      *ekm.LocalKeyManager
	remoteKeyManager     *ekm.RemoteKeyManager
	localDB              basedb.Database
	remoteDB             basedb.Database

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

	if err := env.setupMockBeacon(t); err != nil {
		return nil, fmt.Errorf("failed to setup mock beacon: %w", err)
	}

	return env, nil
}

// Start initializes and starts all containers and services
func (env *TestEnvironment) Start() error {
	var err error

	if err = env.startContainers(); err != nil {
		return fmt.Errorf("failed to start containers: %w", err)
	}

	if err = env.initializeClients(); err != nil {
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	if err = env.initializeKeyManagers(); err != nil {
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

	if env.web3SignerPostgresDB != nil {
		_ = env.web3SignerPostgresDB.Close()
	}

	if env.localDB != nil {
		_ = env.localDB.Close()
	}

	if env.remoteDB != nil {
		_ = env.remoteDB.Close()
	}

	// Clean up key manager directories
	if env.localKeyManagerPath != "" {
		if err := os.RemoveAll(env.localKeyManagerPath); err != nil {
			errors = append(errors, fmt.Errorf("failed to cleanup local key manager directory: %w", err))
		}
	}

	if env.remoteKeyManagerPath != "" {
		if err := os.RemoveAll(env.remoteKeyManagerPath); err != nil {
			errors = append(errors, fmt.Errorf("failed to cleanup remote key manager directory: %w", err))
		}
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

	if err := env.startWeb3Signer(); err != nil {
		return fmt.Errorf("failed to restart Web3Signer: %w", err)
	}

	// Wait for Web3Signer to be ready using testcontainers
	if err := env.waitForWeb3SignerReady(); err != nil {
		return err
	}

	// Reinitialize Web3Signer client with the new container URL
	return env.initializeWeb3SignerClient()
}

// RestartSSVSigner stops and restarts the SSV-Signer container
func (env *TestEnvironment) RestartSSVSigner() error {
	if env.ssvSignerContainer != nil {
		if err := env.ssvSignerContainer.Terminate(env.ctx); err != nil {
			return fmt.Errorf("failed to stop SSV-Signer container: %w", err)
		}
		env.ssvSignerContainer = nil
	}

	if err := env.startSSVSigner(); err != nil {
		return fmt.Errorf("failed to restart SSV-Signer: %w", err)
	}

	// Wait for SSV-Signer to be ready using testcontainers
	if err := env.waitForSSVSignerReady(); err != nil {
		return err
	}

	if err := env.initializeSSVSignerClient(); err != nil {
		return fmt.Errorf("failed to reinitialize SSV-Signer client: %w", err)
	}

	if err := env.reinitializeRemoteKeyManager(); err != nil {
		return fmt.Errorf("failed to reinitialize remote key manager: %w", err)
	}

	return nil
}

// RestartPostgreSQL stops and restarts the PostgreSQL container with persistent data
func (env *TestEnvironment) RestartPostgreSQL() error {
	if env.postgresContainer != nil {
		if err := env.postgresContainer.Terminate(env.ctx); err != nil {
			return fmt.Errorf("failed to stop PostgreSQL container: %w", err)
		}
		env.postgresContainer = nil
	}

	if env.web3SignerPostgresDB != nil {
		_ = env.web3SignerPostgresDB.Close()
		env.web3SignerPostgresDB = nil
	}

	if err := env.startPostgreSQL(); err != nil {
		return fmt.Errorf("failed to restart PostgreSQL: %w", err)
	}

	// Wait for PostgreSQL to be ready using testcontainers
	if err := env.waitForPostgreSQLReady(); err != nil {
		return err
	}

	// Wait for Web3Signer to be ready using testcontainers
	return env.waitForWeb3SignerReady()
}

// waitForPostgreSQLReady waits for PostgreSQL to be ready using testcontainers wait strategy
func (env *TestEnvironment) waitForPostgreSQLReady() error {
	if env.postgresContainer == nil {
		return fmt.Errorf("PostgreSQL container is nil")
	}

	// Use testcontainers wait.ForSQL strategy
	waitStrategy := wait.ForSQL("5432/tcp", "postgres", func(host string, port nat.Port) string {
		return fmt.Sprintf("host=%s port=%s user=postgres password=password dbname=web3signer sslmode=disable", host, port.Port())
	}).WithStartupTimeout(60 * time.Second)

	ctx, cancel := context.WithTimeout(env.ctx, 60*time.Second)
	defer cancel()

	// Apply the wait strategy to the container
	if err := waitStrategy.WaitUntilReady(ctx, env.postgresContainer); err != nil {
		return fmt.Errorf("PostgreSQL not ready: %w", err)
	}

	// Re-establish the environment's database connection
	var err error
	env.web3SignerPostgresDB, err = sql.Open("postgres", env.postgresConnStr)
	if err != nil {
		return fmt.Errorf("failed to re-establish database connection: %w", err)
	}

	return nil
}

// waitForWeb3SignerReady waits for Web3Signer to be ready using testcontainers wait strategy
func (env *TestEnvironment) waitForWeb3SignerReady() error {
	if env.web3SignerContainer == nil {
		return fmt.Errorf("Web3Signer container is nil")
	}

	// Use testcontainers wait.ForHTTP strategy - same as used during startup
	waitStrategy := wait.ForHTTP("/upcheck").
		WithPort("9000/tcp").
		WithStartupTimeout(60 * time.Second)

	ctx, cancel := context.WithTimeout(env.ctx, 60*time.Second)
	defer cancel()

	// Apply the wait strategy to the container
	return waitStrategy.WaitUntilReady(ctx, env.web3SignerContainer)
}

// waitForSSVSignerReady waits for SSV-Signer to be ready using testcontainers wait strategy
func (env *TestEnvironment) waitForSSVSignerReady() error {
	if env.ssvSignerContainer == nil {
		return fmt.Errorf("SSV-Signer container is nil")
	}

	// Use testcontainers wait.ForHTTP strategy - same as used during startup
	waitStrategy := wait.ForHTTP("/v1/operator/identity").
		WithPort("8080/tcp").
		WithStartupTimeout(90 * time.Second)

	ctx, cancel := context.WithTimeout(env.ctx, 90*time.Second)
	defer cancel()

	// Apply the wait strategy to the container
	return waitStrategy.WaitUntilReady(ctx, env.ssvSignerContainer)
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

// initializeClients initializes the client connections
func (env *TestEnvironment) initializeClients() error {
	env.ssvSignerClient = ssvsigner.NewClient(env.ssvSignerURL)
	env.web3SignerClient = web3signer.New(env.web3SignerURL)
	return nil
}

// initializeSSVSignerClient reinitializes just the SSV-Signer client after container restart
func (env *TestEnvironment) initializeSSVSignerClient() error {
	env.ssvSignerClient = ssvsigner.NewClient(env.ssvSignerURL)
	return nil
}

// initializeWeb3SignerClient reinitializes just the Web3Signer client after container restart
func (env *TestEnvironment) initializeWeb3SignerClient() error {
	env.web3SignerClient = web3signer.New(env.web3SignerURL)
	return nil
}

// setupWeb3SignerVolume creates a persistent volume for Web3Signer keystore data
func (env *TestEnvironment) setupWeb3SignerVolume() error {
	volumeName := fmt.Sprintf("web3signer-data-%d", os.Getpid())
	env.web3SignerVolume = testcontainers.VolumeMount(volumeName, "/opt/web3signer")
	fmt.Printf("Created Web3Signer volume: %s (mounting to /opt/web3signer)\n", volumeName)
	return nil
}

// setupPostgreSQLVolume creates a persistent volume for PostgreSQL database data
func (env *TestEnvironment) setupPostgreSQLVolume() error {
	volumeName := fmt.Sprintf("postgres-data-%d-%d", os.Getpid(), time.Now().UnixNano())
	env.postgresVolume = testcontainers.VolumeMount(volumeName, "/var/lib/postgresql/data")
	fmt.Printf("Created PostgreSQL volume: %s (mounting to /var/lib/postgresql/data)\n", volumeName)
	return nil
}

// setupKeyManagerVolumes creates temporary directories for LocalKeyManager and RemoteKeyManager BadgerDB data
func (env *TestEnvironment) setupKeyManagerVolumes() error {
	timestamp := time.Now().UnixNano()

	// Local KeyManager directory
	env.localKeyManagerPath = fmt.Sprintf("/tmp/local-keymanager-data-%d-%d", os.Getpid(), timestamp)
	if err := os.MkdirAll(env.localKeyManagerPath, 0750); err != nil {
		return fmt.Errorf("failed to create local key manager directory: %w", err)
	}
	fmt.Printf("Created LocalKeyManager directory: %s\n", env.localKeyManagerPath)

	// Remote KeyManager directory
	env.remoteKeyManagerPath = fmt.Sprintf("/tmp/remote-keymanager-data-%d-%d", os.Getpid(), timestamp+1)
	if err := os.MkdirAll(env.remoteKeyManagerPath, 0750); err != nil {
		return fmt.Errorf("failed to create remote key manager directory: %w", err)
	}
	fmt.Printf("Created RemoteKeyManager directory: %s\n", env.remoteKeyManagerPath)

	return nil
}
