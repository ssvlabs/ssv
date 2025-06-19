package testenv

import (
	"database/sql"
	"fmt"
	"time"

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

// web3SignerWaitStrategy returns the standard wait strategy for Web3Signer
func web3SignerWaitStrategy() wait.Strategy {
	return wait.ForHTTP(web3signer.PathUpCheck).
		WithPort("9000/tcp").
		WithStatusCodeMatcher(func(status int) bool {
			return status == 200
		}).
		WithStartupTimeout(30 * time.Second).
		WithPollInterval(500 * time.Millisecond)
}

// startWeb3Signer starts the Web3Signer container with persistent volume for keystore data
func (env *TestEnvironment) startWeb3Signer() error {
	web3SignerReq := testcontainers.ContainerRequest{
		Image:        "consensys/web3signer:25.4.1",
		ExposedPorts: []string{"9000/tcp"},
		Networks:     []string{env.networkName},
		NetworkAliases: map[string][]string{
			env.networkName: {"web3signer"},
		},
		Mounts: testcontainers.ContainerMounts{env.web3SignerVolume},
		Cmd: []string{
			"--http-listen-host=0.0.0.0",
			"--http-host-allowlist=*",
			"eth2",
			"--network=mainnet",
			"--slashing-protection-enabled=true",
			fmt.Sprintf("--slashing-protection-db-url=jdbc:postgresql://postgres:5432/%s", postgresDB),
			fmt.Sprintf("--slashing-protection-db-username=%s", postgresUser),
			fmt.Sprintf("--slashing-protection-db-password=%s", postgresPassword),
			"--key-manager-api-enabled=true",
		},
		WaitingFor: web3SignerWaitStrategy(),
	}

	web3SignerContainer, err := testcontainers.GenericContainer(env.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: web3SignerReq,
		Started:          true,
	})
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

	env.web3SignerURL = fmt.Sprintf("http://%s:%s", host, mappedPort.Port())

	env.web3SignerClient = web3signer.New(env.web3SignerURL)

	return nil
}

// startSSVSigner starts the SSV-Signer container
func (env *TestEnvironment) startSSVSigner() error {
	operatorKeyB64 := env.operatorKey.Base64()

	ssvSignerReq := testcontainers.ContainerRequest{
		Image:        "ssv-signer:latest",
		ExposedPorts: []string{"8080/tcp"},
		Networks:     []string{env.networkName},
		NetworkAliases: map[string][]string{
			env.networkName: {"ssv-signer"},
		},
		Env: map[string]string{
			"LISTEN_ADDR":         "0.0.0.0:8080",
			"WEB3SIGNER_ENDPOINT": "http://web3signer:9000",
			"PRIVATE_KEY":         operatorKeyB64,
			"LOG_LEVEL":           "info",
			"LOG_FORMAT":          "console",
		},
		WaitingFor: wait.ForHTTP(ssvsigner.PathOperatorIdentity).
			WithPort("8080/tcp").
			WithStartupTimeout(30 * time.Second),
	}

	ssvSignerContainer, err := testcontainers.GenericContainer(env.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: ssvSignerReq,
		Started:          true,
	})
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

	env.ssvSignerURL = fmt.Sprintf("http://%s:%s", host, mappedPort.Port())

	env.ssvSignerClient = ssvsigner.NewClient(env.ssvSignerURL)

	return nil
}

// applyMigrations applies all pending Web3Signer migrations
func (env *TestEnvironment) applyMigrations() error {
	if env.postgresDB == nil {
		return fmt.Errorf("database connection not initialized")
	}

	if err := env.createMigrationTable(env.postgresDB); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	migrations, err := env.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	for _, migration := range migrations {
		applied, err := env.isMigrationApplied(env.postgresDB, migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if !applied {
			if err = env.applyMigration(env.postgresDB, migration); err != nil {
				return fmt.Errorf("failed to apply migration: %w", err)
			}
		}
	}

	return nil
}
