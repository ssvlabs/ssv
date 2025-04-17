# SSV Remote Signer

## Overview

For SSV Remote signer overview, check [design documentation](./DESIGN.md).

## Setup ssv-signer

To set up ssv-signer, we need to set up web3signer with a slashing protection DB.

### Setup slashing protection DB

Consensys tutorial: https://docs.web3signer.consensys.io/how-to/configure-slashing-protection

#### Short summary

- Run `postgresql` (either Docker or install it) and create DB named `web3signer`

Docker example:

```bash
  docker run -e POSTGRES_PASSWORD=password -e POSTGRES_USER=postgres -e POSTGRES_DB=web3signer -p 5432:5432 postgres
```

- Apply `web3signer` migrations to the DB
    - Download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
    - Apply all migrations from V1 to V12 in `web3signer`'s `migrations/postgresql` folder maintaining their order and
      replacing `${MIGRATION_NAME}.sql` with the migration name
    - It may be done using `psql` or `flyway`

PSQL example:

```bash
  psql --echo-all --host=localhost --port=5432 --dbname=web3signer --username=postgres -f ./web3signer/migrations/postgresql/V1_initial.sql
```

Flyway example (adjust variables):

```bash
  flyway migrate -url="jdbc:postgresql://localhost/web3signer" -locations="filesystem:/web3signer/migrations/postgresql"
```

### Run `web3signer`

Consensys tutorial: https://docs.web3signer.consensys.io/get-started/start-web3signer

#### Short summary

- Run `web3signer` with the arguments in the example. You might need to change HTTP port, Ethereum network, PostgreSQL
  address
    - Either download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
    - Or use `web3signer` Docker image

Docker example:

```bash
  docker run -p 9000:9000 consensys/web3signer:develop --http-listen-port=9000 eth2 --network=holesky --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" --slashing-protection-db-username=postgres --slashing-protection-db-password=password --key-manager-api-enabled=true
```

Release file example:

```bash
  web3signer --http-listen-port=9000 eth2 --network=holesky --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" --slashing-protection-db-username=postgres --slashing-protection-db-password=password --key-manager-api-enabled=true
```

### Run `ssv-signer`

- Run `./cmd/ssv-signer` passing the following arguments:
    - `LISTEN_ADDR` - address to listen on (`:8080` by default)
    - `WEB3SIGNER_ENDPOINT` - `web3signer`'s address from the previous step
    - `PRIVATE_KEY` or (`PRIVATE_KEY_FILE` and `PASSWORD_FILE`) - operator private key or path to operator keystore and
      password files

Example:

```bash
  PRIVATE_KEY=OPERATOR_PRIVATE_KEY LISTEN_ADDR=0.0.0.0:8080 WEB3SIGNER_ENDPOINT=http://localhost:9000 ./ssv-signer
```

### Configure TLS for SSV-Signer

SSV-Signer supports TLS for securing connections in two primary configurations:

1. **Server TLS** - Secures incoming connections to SSV-Signer
2. **Client TLS** - Secures connections from SSV-Signer to Web3Signer

#### Server TLS Configuration

To enable TLS for the SSV-Signer server:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
SERVER_CERT_FILE=/path/to/server.crt \
SERVER_KEY_FILE=/path/to/server.key \
SERVER_CA_CERT_FILE=/path/to/ca.crt \
./ssv-signer
```

- `SERVER_CERT_FILE` and `SERVER_KEY_FILE` are required to enable server TLS
- `SERVER_CA_CERT_FILE` enables client certificate verification (mutual TLS)
- Server TLS uses TLS 1.3 and modern cipher suites for optimal security

#### Client TLS Configuration

To enable TLS when connecting to Web3Signer:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8080 \
WEB3SIGNER_ENDPOINT=https://localhost:9000 \
CLIENT_CERT_FILE=/path/to/client.crt \
CLIENT_KEY_FILE=/path/to/client.key \
CLIENT_CA_CERT_FILE=/path/to/ca.crt \
./ssv-signer
```

- `CLIENT_CERT_FILE` and `CLIENT_KEY_FILE` are used for client authentication
- `CLIENT_CA_CERT_FILE` is used to verify the Web3Signer server certificate
- Both certificate and key files must be specified together

#### Full Mutual TLS Configuration

For complete end-to-end encryption with certificate verification:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=https://localhost:9000 \
SERVER_CERT_FILE=/path/to/server.crt \
SERVER_KEY_FILE=/path/to/server.key \
SERVER_CA_CERT_FILE=/path/to/server-ca.crt \
CLIENT_CERT_FILE=/path/to/client.crt \
CLIENT_KEY_FILE=/path/to/client.key \
CLIENT_CA_CERT_FILE=/path/to/client-ca.crt \
./ssv-signer
```
