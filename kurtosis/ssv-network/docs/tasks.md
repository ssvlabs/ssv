# SSV Network

### [Intro](../README.md) | [Architecture](architecture.md) | [Setup](setup.md) | Tasks | [Local development](local-dev.md) | [Roles](roles.md) | [Publish](publish.md)

## Development scripts

All scripts can be executed using `package.json` scripts.

### Build the contracts

This creates the build artifacts for deployment or testing

```
npm run build
```

### Test the contracts

This builds the contracts and runs the unit tests. It also runs the gas reporter and it outputs the report at the end of the tests.

```
npm run test
```

### Run the code coverage

This builds the contracts and runs the code coverage. This is slower than testing since it makes sure that every line of our contracts is tested. It outputs the report in folder `coverage`.

```
npm run solidity-coverage
```

### Slither

Runs the static analyzer [Slither](https://github.com/crytic/slither), to search for common solidity vulnerabilities. By default it analyzes all contracts.
`npm run slither`

### Size contracts

Compiles the contracts and report the size of each one. Useful to check to not surpass the 24k limit.

```
npm run size-contracts
```

## Development tasks

This project uses hardhat tasks to perform the deployment and upgrade of the main contracts and modules.

Following Hardhat's way of working, you must specify the network against which you want to run the task using the `--network` parameter. In all the following examples, the goerli network will be used, but you can specify any defined in your `hardhat.config` file.

### Deploy all contracts

Runs the deployment of the main SSVNetwork and SSVNetworkViews contracts, along with their associated modules:

```
npx hardhat --network goerli_testnet deploy:all
```

When deploying to live networks like Goerli or Mainnet, please double check the environment variables:

- MINIMUM_BLOCKS_BEFORE_LIQUIDATION
- MINIMUM_LIQUIDATION_COLLATERAL
- VALIDATORS_PER_OPERATOR_LIMIT
- DECLARE_OPERATOR_FEE_PERIOD
- EXECUTE_OPERATOR_FEE_PERIOD
- OPERATOR_MAX_FEE_INCREASE

## Upgrade process

We use [UUPS Proxy Upgrade pattern](https://docs.openzeppelin.com/contracts/4.x/api/proxy) for `SSVNetwork` and `SSVNetworkViews` contracts to have an ability to upgrade them later.

**Important**: It's critical to not add any state variable to `SSVNetwork` nor `SSVNetworkViews` when upgrading. All the state variables are managed by [SSVStorage](../contracts/libraries/SSVStorage.sol) and [SSVStorageProtocol](../contracts/libraries/SSVStorageProtocol.sol). Only modify the logic part of the main contracts or the modules.

### Upgrade SSVNetwork / SSVNetworkViews

#### Upgrade contract logic

In this case, the upgrade add / delete / modify a function, but no other piece in the system is changed (libraries or modules).

Set `SSVNETWORK_PROXY_ADDRESS` in `.env` file to the right value.

Run the upgrade task:

```
Usage: hardhat [GLOBAL OPTIONS] upgrade:proxy [--contract <STRING>] [--init-function <STRING>] [--proxy-address <STRING>] [...params]

OPTIONS:
  --contract            New contract upgrade
  --init-function       Function to be executed after upgrading
  --proxy-address       Proxy address of SSVNetwork / SSVNetworkViews

POSITIONAL ARGUMENTS:
  params        Function parameters

Example:
npx hardhat --network goerli_testnet upgrade:proxy --proxy-address 0x1234... --contract SSVNetworkV2 --init-function initializev2 param1 param2
```

It is crucial to verify the upgraded contract using its proxy address.
This ensures that users can interact with the correct, upgraded implementation on Etherscan.

### Update a module

Sometimes you only need to perform changes in the logic of a function of a module, add a private function or do something that doesn't affect other components in the architecture. Then you can use the task to update a module.

This task first deploys a new version of a specified SSV module contract, and then updates the SSVNetwork contract to use this new module version only if `--attach-module` flag is set to `true`.

```
Usage: hardhat [GLOBAL OPTIONS] update:module [--attach-module <BOOLEAN>] [--module <STRING>] [--proxy-address <STRING>]

OPTIONS:

  --attach-module       Attach module to SSVNetwork contract (default: false)
  --module              SSV Module
  --proxy-address       Proxy address of SSVNetwork / SSVNetworkViews (default: null)


Example:
Update 'SSVOperators' module contract in the SSVNetwork
npx hardhat --network goerli_testnet update:module --module SSVOperators --attach-module true --proxy-address 0x1234...
```

### Upgrade a library

When you change a library that `SSVNetwork` uses, you need to also update all modules where that library is used.

Set `SSVNETWORK_PROXY_ADDRESS` in `.env` file to the right value.

Run the task to upgrade SSVNetwork proxy contract as described in [Upgrade SSVNetwork / SSVNetworkViews](#upgrade-contract-logic)

Run the right script to update the module affected by the library change, as described in [Update a module](#update-a-module) section.

### Manual upgrade of SSVNetwork / SSVNetworkViews

Validates and deploys a new implementation contract. Use this task to prepare an upgrade to be run from an owner address you do not control directly or cannot use from Hardhat.

```
Usage: hardhat [GLOBAL OPTIONS] upgrade:prepare [--contract <STRING>] [--proxy-address <STRING>]

OPTIONS:

  --contract            New contract upgrade (default: null)
  --proxy-address       Proxy address of SSVNetwork / SSVNetworkViews (default: null)

Example:
npx hardhat --network goerli_testnet upgrade:prepare --proxy-address 0x1234... --contract SSVNetworkViewsV2
```

The task will return the new implementation address. After that, you can run `upgradeTo` or `upgradeToAndCall` in SSVNetwork / SSVNetworkViews proxy address, providing it as a parameter.
