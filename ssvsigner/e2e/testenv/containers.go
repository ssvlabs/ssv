package testenv

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// PostgreSQL configuration constants
const (
	postgresDB       = "web3signer"
	postgresUser     = "postgres"
	postgresPassword = "password"
)

// buildPostgresConnStr creates a PostgreSQL connection string for the given host and port
func buildPostgresConnStr(host, port string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, postgresUser, postgresPassword, postgresDB)
}

// startContainers starts all Docker containers in the correct order
func (env *TestEnvironment) startContainers() error {
	if err := env.createNetwork(); err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	if err := env.startPostgreSQL(); err != nil {
		return fmt.Errorf("failed to start PostgreSQL: %w", err)
	}

	if err := env.applyMigrations(); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	if err := env.startWeb3Signer(); err != nil {
		return fmt.Errorf("failed to start Web3Signer: %w", err)
	}

	if err := env.startSSVSigner(); err != nil {
		return fmt.Errorf("failed to start SSV-Signer: %w", err)
	}

	return nil
}

// createNetwork creates a Docker network using the modern API
func (env *TestEnvironment) createNetwork() error {
	net, err := network.New(env.ctx)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	env.networkName = net.Name
	return nil
}

// startPostgreSQL starts the PostgreSQL container
func (env *TestEnvironment) startPostgreSQL() error {
	postgresReq := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Networks:     []string{env.networkName},
			NetworkAliases: map[string][]string{
				env.networkName: {"postgres"},
			},
			Mounts: testcontainers.ContainerMounts{env.postgresVolume},
			Env: map[string]string{
				"POSTGRES_DB":       postgresDB,
				"POSTGRES_USER":     postgresUser,
				"POSTGRES_PASSWORD": postgresPassword,
			},
			WaitingFor: wait.ForSQL("5432/tcp", "postgres", func(host string, port nat.Port) string {
				return buildPostgresConnStr(host, port.Port())
			}).WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	}

	postgresContainer, err := testcontainers.GenericContainer(env.ctx, postgresReq)
	if err != nil {
		return fmt.Errorf("failed to start PostgreSQL: %w", err)
	}

	env.postgresContainer = postgresContainer

	host, err := postgresContainer.Host(env.ctx)
	if err != nil {
		return fmt.Errorf("failed to get PostgreSQL host: %w", err)
	}

	mappedPort, err := postgresContainer.MappedPort(env.ctx, "5432/tcp")
	if err != nil {
		return fmt.Errorf("failed to get PostgreSQL port: %w", err)
	}

	env.postgresConnStr = buildPostgresConnStr(host, mappedPort.Port())

	// Create database connection
	db, err := sql.Open("postgres", env.postgresConnStr)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	if err = db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			// Best effort cleanup, don't mask the original ping error
		}
		return fmt.Errorf("failed to ping postgres: %w", err)
	}
	env.postgresDB = db

	return nil
}

// startWeb3Signer starts the Web3Signer container with persistent volume for keystore data
func (env *TestEnvironment) startWeb3Signer() error {
	if _, err := os.Stat(env.certDir); os.IsNotExist(err) {
		return fmt.Errorf("certificate directory does not exist: %s", env.certDir)
	}

	waitStrategy, err := env.web3SignerWaitStrategy()
	if err != nil {
		return fmt.Errorf("failed to create wait strategy: %w", err)
	}

	web3SignerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "consensys/web3signer:25.4.1",
			ExposedPorts: []string{"9000/tcp"},
			Networks:     []string{env.networkName},
			NetworkAliases: map[string][]string{
				env.networkName: {"web3signer"},
			},
			Mounts: testcontainers.ContainerMounts{env.web3SignerVolume},
			HostConfigModifier: func(hostConfig *container.HostConfig) {
				hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: env.certDir,
					Target: "/certs",
				})
			},
			Cmd: []string{
				"--http-listen-host=0.0.0.0",
				"--http-host-allowlist=*",
				"--tls-keystore-file=/certs/web3signer.p12",
				"--tls-keystore-password-file=/certs/web3signer_password.txt",
				"--tls-known-clients-file=/certs/web3signer_known_clients.txt",
				"eth2",
				"--network=mainnet",
				"--slashing-protection-enabled=true",
				fmt.Sprintf("--slashing-protection-db-url=jdbc:postgresql://postgres:5432/%s", postgresDB),
				fmt.Sprintf("--slashing-protection-db-username=%s", postgresUser),
				fmt.Sprintf("--slashing-protection-db-password=%s", postgresPassword),
				"--key-manager-api-enabled=true",
			},
			WaitingFor: waitStrategy,
		},
		Started: true,
	}

	web3SignerContainer, err := testcontainers.GenericContainer(env.ctx, web3SignerReq)
	if err != nil {
		return fmt.Errorf("failed to start web3signer: %w", err)
	}

	env.web3SignerContainer = web3SignerContainer

	// Get Web3Signer URL for direct client access
	host, err := web3SignerContainer.Host(env.ctx)
	if err != nil {
		return fmt.Errorf("failed to get web3signer host: %w", err)
	}

	mappedPort, err := web3SignerContainer.MappedPort(env.ctx, "9000/tcp")
	if err != nil {
		return fmt.Errorf("failed to get web3signer port: %w", err)
	}

	env.web3SignerURL = fmt.Sprintf("https://%s:%s", host, mappedPort.Port())

	tlsConfig, err := createMutualTLSConfig(env.web3SignerCertPath, env.e2eClientCertPath)
	if err != nil {
		return fmt.Errorf("failed to create TLS config: %w", err)
	}
	env.web3SignerClient = web3signer.New(env.web3SignerURL, web3signer.WithTLS(tlsConfig))

	return nil
}

// web3SignerWaitStrategy returns the wait strategy for Web3Signer
func (env *TestEnvironment) web3SignerWaitStrategy() (wait.Strategy, error) {
	tlsConfig, err := createMutualTLSConfig(env.web3SignerCertPath, env.e2eClientCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config for wait strategy: %w", err)
	}

	return wait.ForHTTP(web3signer.PathUpCheck).
		WithPort("9000/tcp").
		WithTLS(true, tlsConfig).
		WithStatusCodeMatcher(func(status int) bool {
			return status == 200
		}).
		WithStartupTimeout(30 * time.Second).
		WithPollInterval(500 * time.Millisecond), nil
}

// startSSVSigner starts the SSV-Signer container
func (env *TestEnvironment) startSSVSigner() error {
	operatorKeyB64 := env.operatorKey.Base64()

	if _, err := os.Stat(env.certDir); os.IsNotExist(err) {
		return fmt.Errorf("certificate directory does not exist: %s", env.certDir)
	}

	tlsConfig, err := createMutualTLSConfig(env.ssvSignerCertPath, env.e2eClientCertPath)
	if err != nil {
		return fmt.Errorf("failed to create TLS config for SSV-Signer client: %w", err)
	}

	waitStrategy, err := env.ssvSignerWaitStrategy(tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to create wait strategy: %w", err)
	}

	ssvSignerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "ssv-signer:latest",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{env.networkName},
			NetworkAliases: map[string][]string{
				env.networkName: {"ssv-signer"},
			},
			Env: map[string]string{
				// Basic configuration
				"LISTEN_ADDR":         "0.0.0.0:8080",
				"PRIVATE_KEY":         operatorKeyB64,
				"LOG_LEVEL":           "info",
				"LOG_FORMAT":          "console",
				"WEB3SIGNER_ENDPOINT": "https://web3signer:9000",

				// Server TLS configuration
				"KEYSTORE_FILE":          "/certs/ssv-signer.p12",
				"KEYSTORE_PASSWORD_FILE": "/certs/ssv-signer_password.txt",
				"KNOWN_CLIENTS_FILE":     "/certs/known_clients.txt",

				// Client TLS for Web3Signer
				"WEB3SIGNER_KEYSTORE_FILE":          "/certs/ssv-signer.p12",
				"WEB3SIGNER_KEYSTORE_PASSWORD_FILE": "/certs/ssv-signer_password.txt",
				"WEB3SIGNER_SERVER_CERT_FILE":       "/certs/web3signer.crt",
			},
			HostConfigModifier: func(hostConfig *container.HostConfig) {
				hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: env.certDir,
					Target: "/certs",
				})
			},
			WaitingFor: waitStrategy,
		},
		Started: true,
	}

	ssvSignerContainer, err := testcontainers.GenericContainer(env.ctx, ssvSignerReq)
	if err != nil {
		return fmt.Errorf("failed to start ssv-signer: %w", err)
	}

	env.ssvSignerContainer = ssvSignerContainer

	host, err := ssvSignerContainer.Host(env.ctx)
	if err != nil {
		return fmt.Errorf("failed to get ssv-signer host: %w", err)
	}

	mappedPort, err := ssvSignerContainer.MappedPort(env.ctx, "8080/tcp")
	if err != nil {
		return fmt.Errorf("failed to get ssv-signer port: %w", err)
	}

	env.ssvSignerURL = fmt.Sprintf("https://%s:%s", host, mappedPort.Port())
	env.ssvSignerClient = ssvsigner.NewClient(env.ssvSignerURL, ssvsigner.WithTLSConfig(tlsConfig))

	return nil
}

// ssvSignerWaitStrategy returns the wait strategy for SSV-Signer
func (env *TestEnvironment) ssvSignerWaitStrategy(tlsConfig *tls.Config) (wait.Strategy, error) {
	return wait.ForHTTP(ssvsigner.PathOperatorIdentity).
		WithPort("8080/tcp").
		WithTLS(true, tlsConfig).
		WithStartupTimeout(30 * time.Second), nil
}

// waitForContainerReady waits for a container to be ready using a wait strategy
func (env *TestEnvironment) waitForContainerReady(container testcontainers.Container, strategy wait.Strategy) error {
	if container == nil {
		return fmt.Errorf("container is nil")
	}

	ctx, cancel := context.WithTimeout(env.ctx, 60*time.Second)
	defer cancel()

	return strategy.WaitUntilReady(ctx, container)
}

// waitForWeb3SignerReady waits for Web3Signer to be ready
func (env *TestEnvironment) waitForWeb3SignerReady() error {
	strategy, err := env.web3SignerWaitStrategy()
	if err != nil {
		return fmt.Errorf("failed to create wait strategy: %w", err)
	}
	return env.waitForContainerReady(env.web3SignerContainer, strategy)
}
