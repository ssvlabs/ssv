[<img src="./resources/ssv_header_image.png" >](https://www.ssvlabs.io/)

<br>
<br>

# SSV - Development Guide

- [SSV - Development Guide](#ssv---development-guide)
  - [Usage](#usage)
    - [Common Commands](#common-commands)
      - [Build](#build)
      - [Test](#test)
      - [Lint](#lint)
      - [Specify Version](#specify-version)
      - [Splitting a Validator Key](#splitting-a-validator-key)
      - [Generating an Operator Key](#generating-an-operator-key)
    - [Config Files](#config-files)
      - [Node Config](#node-config)
  - [Running a Local Network of Operators](#running-a-local-network-of-operators)
    - [Install](#install)
      - [Prerequisites](#prerequisites)
      - [Clone Repository](#clone-repository)
      - [Build Binary](#build-binary)
    - [Configuration](#configuration)
      - [Use script](#use-script)
      - [Use manual steps](#use-manual-steps)
    - [Run](#run)
      - [Local network with 4 nodes with Docker Compose](#local-network-with-4-nodes-with-docker-compose)
      - [Local network with 4 nodes for debugging with Docker Compose](#local-network-with-4-nodes-for-debugging-with-docker-compose)
  - [Coding Standards](#coding-standards)

## Usage

### Common Commands

#### Build

```bash
$ make build
```

#### Test

```bash
$ make full-test
```

#### Lint

```bash
$ make lint-prepare
$ make lint
```

#### Specify Version

```bash
$ ./bin/ssvnode version
```

#### Splitting a Validator Key

We split an eth2 BLS validator key into shares via Shamir-Secret-Sharing(SSS) to be used between the SSV nodes.

```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys )
$ ./bin/ssvnode export-keys --mnemonic="<mnemonic>" --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count <number of ssv nodes> --private-key <privateKey>
```

#### Generating an Operator Key

To generate an operator key, you can use `./bin/ssvnode generate-operator-keys`. This command can generate the key in three distinct ways:

1. Raw format
2. Encrypted format as keystore.json
3. Convert an existing key to encrypted keystore.json format

**IMPORTANT**: The raw format is **NOT recommended** for production use, as it can expose sensitive data. Use the encrypted format for added security.

**Option 1: Raw Operator Key Generation:**
This is the default mode of operation and does not require any additional parameters.

```bash
$ ./bin/ssvnode generate-operator-keys
```

This will generate an operator key in raw format.

**Option 2: Encrypted Operator Key Generation as keystore.json:**

To generate an operator key in encrypted format, use the `--password-file` option followed by your password file.

```bash
$ ./bin/ssvnode generate-operator-keys --password-file=path/to/your/file
```

Please replace `path/to/your/file` with your password file. This will generate an operator key in encrypted format.

**Option 3: Convert an Existing Key to keystore.json:**

To convert an existing key to keystore.json format, use both the `--password-file` and `--operator-key-file` options.

```bash
$ ./bin/ssvnode generate-operator-keys --password-file=path/to/your/password/file --operator-key-file=path/to/your/existing/key/file
```

Please replace `path/to/your/password/file` with your password file and `path/to/your/existing/key/file` with your existing key file. This will convert the existing key to a keystore.json file as an encrypted private key.

Keep your password safe as it will be required to decrypt the operator key for use.

### Config Files

Config files are located in `./config` directory:

#### Node Config

Specifies general configuration regards the current node. \
Example yaml - [config.yaml](../config/config.yaml)

## Operator Private Key

The operator private key can be provided in the `config.yaml` file in two different ways:

1. As a base64-encoded private key:

   ```yaml
   OperatorPrivateKey: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBbCtQT05ocUxpMDRtMUVpMlVjV0NEbzVaRE5SNzVaamF3cTZ1elFnTkFwb0E5aUxNClJ0ZzU2SHF3d0x3NnpIYWVVZmRHMlRNQm5sL0YwQW54MWFtd2RUcGRLcEIvbDNjRnpHWXl2Z3FZaVFpaDYxalYKNU1IV1JqTG9NWjFiQUJLWmJ5OGZaYnYvRi9oQVVNditRZ3EyMWw2SDVMeUF1UEhqSDhoczJGS0RDWUxWelRhdgpXWGYremRQdGZMdExoSURTd3lvYnQrS0pJTHlabjR1Mzl0M1E2WFFSSjNWNHBXOVNHNGw4TUJVMmVSRDgvV3lRCmVrb3dQUm1CSUw0dVhQU01oR3EzQUhTdWxVYnFYZ1R4TFBnTWxUYkNvWXFvU00wYXIvME9XOWpPV3AvSGllMW4KSTRFTC9VRXhpV2dhV0kwTENYTTZ6Z0NLdzI4cE1SNDgzTnBmQVFJREFRQUJBb0lCQUZkWUdnSkUyNUFkUGZqLwpZMUM4cW1DaWZSVUNyOGpGVUs5NWNtM1hQbHdMb1pmcFJOMU1oR2hxL1crb0RvdjdmbW1XTURqQXV5S080cHNTCnpPM1lhZS9Qd3pteDVKMStSV2hZTUwvV0tnZExYb21QQ1ZsR0dtazk1d1o0L1phYUczK3pjbk8zV3ljMmpBMnEKY1NrYkxpOHlKeVZqUFFhZG1zVnhKUjUwdklQZnhUWmYxQkVSbjBUa2sybmhkWlZkWmxFS3VWN3NLY2FTRkFtWAp2eElKazFjUzZWN2xFTk1iazlITXpwbjkxYm1vK3JITEd0R1dsZHN6dXRTTHcxNXE3N3ZyZm0xZ29rdE9YUlFqCmNCU3RIZGVkdEJYUE1EYnMyS3V1WFVYR3JZeWtKZUkwb1hLNjIzU0JRcmlSa0wxQ2cydCtZdEVWd1RhWWtOakgKcmZYTWlFRUNnWUVBeFpCY1NSYzQ0amhFLzV6VDBZVERuangyVEV4c2pHL3VvY2ZPRWRSUHVKTEJuL3VTNXRhNAoxTlZMcDBSTnFGYlNxNHppbHByS1F1ZU1US0VoSlJjMWxwZ2M1MzhyT2srYkZXM1lJUlJweldzWHQxNnNOdXdLCnRlSGFNTUNrbkxpRTNWMEgva1BXOGlkZkVoRkJTTkhFWXEzZHdzMGpDL0c5SzRSWk9WRTVrUWtDZ1lFQXhORDcKdU00MUZVeUF2S29lcHJsVE9lY3YyMy93QWhESDZ4MkMxUnZPTUhaV0Rwdjd5VXZ2YlZLRXhkNmE5bUxkZ3ZsbApEWlo3TWorZTFzVUlJQXdsMUdVU1MrcDVFUzNtRll3K0pZY3lBUDN1TVRkQ0Y4cXpScGdWOEhTUFhWS0NYWXh1CjZ5UzN5WUljRlpTSHJNQmlqcTVTUjNWNUpMZVYxTytubHRnSmREa0NnWUVBd3oxYzFpYUs0cFQxS3g3Uy9aV1UKdEVYUUtxckVBeTJDeUlKcWxaZ1ppTEFQaFlqYXJpRzQyeXhHN1hCRXhuMjNDQzNjcHpVbGVXVFdjOHd3c3pUeQprbmFVNmZuMHdGVjNUNEFVUE95dGVvSEJHRWdKTE9XcjEvN3czNGtocEhkOVpqM1A3bWtnZklLSUk1VEZ6YTd2Cnd3MUx3SDExaXhKRS9rSjI0bnZ3eGZFQ2dZQUw0N0FCSnZ2UDhKSXFVNENNZzgrQ1JQUUFKNGRoS0pCYkpLbzkKbzNOZVBCZlF4QjErdUlhYkxRdjJSQTlLYVFpR20vZzl6T1JlVWJlUHM5RmMxajhHeUtCRlU4SENodXBLVFBHSQpKTldoZDdXRzVaYXBoMFl6TW9iSXd0SFNTbVN6c0FNWFUxMkMzOGhBaVh0MHRSNS9EZ3JNWkUxUUtZTDBuUkdiCnJDdE9DUUtCZ0FJL0dtd2tUcHlVNHBNWEFsRWU0UXFuZjVoS09vMXp3NHJXTTBQaGFXRnRlZzJ1aUR2ODNHYXMKbUxOMkx0NGcyb0dYZDM5RGFZazFsdG5taU1ScUJBTG5JdUhSMjFiR1pMQ3RCWUtUMllNRDRBUlRyd0I4bzJiSQpMQlFwN3FLRC9BVzZhbkVYT1hLbE9zT01vZG5QZjUyS3o2STZMMXpyVmhlVWlXc2NBancxCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg==
   ```

   This private key can be generated using the CLI command `./bin/ssvnode generate-operator-keys`. However,
   this method merely encodes the private key, it doesn't provide additional encryption.
   For a more secure approach, it's recommended to use an encrypted keystore file with a password.

2. As an Encrypted Keystore file:
   ```yaml
   KeyStore:
     PrivateKeyFile: /path/to/your/file
     PasswordFile: /path/to/your/file
   ```
   In this approach, the private key is not only encoded but also encrypted, adding an extra layer of security.
   To generate the encrypted keystore file, run the `./bin/ssvnode generate-operator-keys` command with the `--password-file=path/to/your/file` flag.
   Replace `path/to/your/file` with password file path.
   This command will generate an encrypted keystore file that can be used securely in the `config.yaml` file.
   It's a more secure approach because the private key is not only encoded but also encrypted, adding an extra layer of security.

## Running a Local Network of Operators

This section details the steps to run a local network of operator nodes.

### Install

#### Prerequisites

In order to run a local environment, install the following:

- git
- go (>=1.24)
- docker
- make
- yq

#### Clone Repository

```shell
$ git clone https://github.com/ssvlabs/ssv.git
```

#### Build Binary

```shell
$ make build
```

### Configuration

#### Use script

By using this script, developers can simulate a real SSV environment, run multiple nodes, and start those nodes performing duties with the passed validator's keystore. This is incredibly beneficial for debugging, testing functionalities, or preparing for deployment in a live setting. It provides a realistic but controlled environment where developers can observe the interaction of multiple nodes in the SSV network. \
The script simplifies configuration by automatically generating YAML files for each operator and an 'events.yaml' file. The 'events.yaml' emulates a 'happy flow' scenario, which includes the registration of four operators and one validator

1. Download the executable version 1.0.1 from [ssv-keys](https://github.com/ssvlabs/ssv-keys/releases/tag/v1.0.1).

   - After downloading, follow these [steps](https://github.com/ssvlabs/ssv-keys#option-1-running-an-executable-recommended-route) to provide the necessary permissions to the executable.

2. Generate a local configuration using the provided [script](../scripts/generate_local_config.sh).

   - Execute the script by typing the following command in your terminal:
     ```shell
       ./generate_local_config.sh $OP_SIZE $KS_PATH $KS_PASSWORD $OA $NONCE $SSV_KEYS_PATH
     ```
   - Please replace each variable with the following details:
     - `OP_SIZE`: Number of operators to create [3f+1]. (e.g., 4 or 7 or 10, etc.)
     - `KS_PATH`: Path to your keystore.json file (e.g., ./keystore-m_12381_3600_0_0_0-1639058279.json).
     - `KS_PASSWORD`: Your keystore password (e.g., 12345678).
     - `OA`: Owner address (e.g., 0x1234567890123456789012345678901234567890).
     - `NONCE`: Nonce (e.g., 0).
     - `SSV_KEYS_PATH`: Path to ssv-keys executable [optional]. The default path is ./bin/ssv-keys-mac.

3. Move the generated .yaml files to the `./config` [directory](../config).

4. Update the [config.yaml](../config/config.yaml) file to include the local events path:

   ```yaml
   LocalEventsPath: ./config/events.yaml
   ```

5. Add "mdns" under the p2p configuration in the [config.yaml](../config/config.yaml) file:

   ```yaml
   p2p:
     Discovery: mdns
   ```

6. Finally, build and run 4 local nodes with this command:
   ```shell
   docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
   ```

#### Use manual steps

These steps offer a detailed manual alternative to the script. They provide a step-by-step guide for setting up a local SSV environment, generating the necessary keys, creating and configuring YAML files, and building and running multiple SSV nodes. They are beneficial for those who prefer more control over the setup process or need to understand it in greater detail.

1. Generate 4 operator keys. You can refer to the [Generating an Operator Key](#generating-an-operator-key) section for guidance.

2. Create 4 .yaml files (`share1.yaml`, `share2.yaml`, `share3.yaml`, `share4.yaml`) with the corresponding configurations, based on the provided [template file](../config/example_share.yaml).

   - Save these files in the `./config` directory.

3. Populate the `OperatorPrivateKey` field in the `share[1..4].yaml` files with the operator private keys that you generated in step 1.

4. Generate share keys using the 4 operator public keys generated in step 1. You can do this using [ssv-keys](https://github.com/ssvlabs/ssv-keys#example).

5. Create an `events.yaml` file based on the provided [template file](../config/events.example.yaml). Follow the validator registration happy flow example for proper configuration.

   - Populate the operator registration events with the data generated in step 4.
   - Populate the validator registration event with the data generated in step 4.

6. Place the `events.yaml` file in the `./config` directory (`./config/events.yaml`).

7. Add the local events path to the [config.yaml](../config/config.yaml) file:

   ```yaml
   LocalEventsPath: ./config/events.yaml
   ```

8. Add the discovery "mdns" under the p2p section in the [config.yaml](../config/config.yaml) file:

   ```yaml
   p2p:
     Discovery: mdns
   ```

9. Finally, build and run 4 local nodes with the following command:
   ```bash
   docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
   ```

### Run

Run a local network using `docker`

#### Local network with 4 nodes with Docker Compose

```shell
$ make docker-all
```

#### Local network with 4 nodes for debugging with Docker Compose

```shell
$ make docker-debug
```

## Coding Standards

Please make sure your contributions adhere to our coding guidelines:

- Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
  guidelines (i.e. see how to set up code formatting with [your IDE](./IDE_INTEGRATION.md)).
- Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
  guidelines.
- Pull requests need to be based on and opened against the `stage` branch, and its commits should be squashed on merge.
