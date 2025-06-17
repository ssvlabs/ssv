package testenv

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startContainers starts all Docker containers in the correct order
func (env *TestEnvironment) startContainers() error {
	var err error

	if err = env.createNetwork(); err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	if err = env.startPostgreSQL(); err != nil {
		return fmt.Errorf("failed to start PostgreSQL: %w", err)
	}

	if err = env.applyMigrations(); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	if err = env.startWeb3Signer(); err != nil {
		return fmt.Errorf("failed to start Web3Signer: %w", err)
	}

	if err = env.startSSVSigner(); err != nil {
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

	env.network = net
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
				"POSTGRES_DB":       "web3signer",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "password",
			},
			WaitingFor: wait.ForListeningPort("5432/tcp").
				WithStartupTimeout(60 * time.Second),
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

	postgresConnStr := fmt.Sprintf("host=%s port=%s user=postgres password=password dbname=web3signer sslmode=disable",
		host, mappedPort.Port())
	env.postgresConnStr = postgresConnStr

	return nil
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
			"--slashing-protection-db-url=jdbc:postgresql://postgres:5432/web3signer",
			"--slashing-protection-db-username=postgres",
			"--slashing-protection-db-password=password",
			"--key-manager-api-enabled=true",
		},
		WaitingFor: wait.ForHTTP("/upcheck").
			WithPort("9000/tcp").
			WithStartupTimeout(120 * time.Second),
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
		WaitingFor: wait.ForHTTP("/v1/operator/identity").
			WithPort("8080/tcp").
			WithStartupTimeout(90 * time.Second),
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
	return nil
}

// applyMigrations applies all pending Web3Signer migrations
func (env *TestEnvironment) applyMigrations() error {
	db, err := sql.Open("postgres", env.postgresConnStr)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	// Don't defer close here - we want to keep the connection open for the test environment

	if err = db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	env.postgresDB = db

	if err = env.createMigrationTable(db); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	migrations, err := env.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	for _, migration := range migrations {
		applied, err := env.isMigrationApplied(db, migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if !applied {
			if err = env.applyMigration(db, migration); err != nil {
				return fmt.Errorf("failed to apply migration: %w", err)
			}
		}
	}

	return nil
}
