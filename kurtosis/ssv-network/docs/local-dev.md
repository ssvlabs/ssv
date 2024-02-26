# SSV Network

### [Intro](../README.md) | [Architecture](architecture.md) | [Setup](setup.md) | [Tasks](tasks.md) | Local development | [Roles](roles.md) | [Publish](publish.md)

## Running against a local node / testnet

You can deploy and run these contracts in a local node like Hardhat's, Ganache, or public testnets. This guide will cover the process.

### Run [Setup](setup.md)

Execute the steps to set up all tools needed.

### Configure Environment

Copy [.env.example](../.env.example) to `.env` and edit to suit.

- `[NETWORK]_ETH_NODE_URL` RPC URL of the node
- `[NETWORK]_OWNER_PRIVATE_KEY` Private key of the deployer account, without 0x prefix
- `GAS_PRICE` Example 30000000000
- `GAS` Example 8000000
- `ETHERSCAN_KEY` Etherescan API key to verify deployed contracts
- `SSV_TOKEN_ADDRESS` SSV Token contract address to be used in custom networks. Keep it empty to deploy a mocked SSV token.
- `MINIMUM_BLOCKS_BEFORE_LIQUIDATION` A number of blocks before the cluster enters into a liquidatable state. Example: 214800 = 30 days
- `OPERATOR_MAX_FEE_INCREASE` The fee increase limit in percentage with this format: 100% = 10000, 10% = 1000 - using 10000 to represent 2 digit precision
- `DECLARE_OPERATOR_FEE_PERIOD` The period in which an operator can declare a fee change (seconds)
- `EXECUTE_OPERATOR_FEE_PERIOD` The period in which an operator fee change can be executed (seconds)
- `VALIDATORS_PER_OPERATOR_LIMIT` The number of validators an operator can manage
- `MINIMUM_LIQUIDATION_COLLATERAL` The lowest number in wei a cluster can have before its liquidatable

#### Network configuration

In [hardhat.config.ts](../hardhat.config.ts) you can find specific configs for different networks, that are taken into account only when the `[NETWORK]_ETH_NODE_URL` parameter in `.env` file is set.
For example, in `.env` file you can set:

```
GOERLI_ETH_NODE_URL="https://goerli.infura.io/v3/..."
GOERLI_OWNER_PRIVATE_KEY="d79d.."
```

That means Hardhat will pick `config.networks.goerli` section in `hardhat.config.ts` to set the network parameters.

### Start the local node

To run the local node, execute the command in a separate terminal.

```sh
npx hardhat node
```

For more details about it and how to use MainNet forking you can find [here](https://hardhat.org/hardhat-network/).

### Deployment

The inital deployment process involves the deployment of all main modules (SSVClusters, SSVOperators, SSVDAO and SSVViews), SSVNetwork and SSVNetworkViews contracts.

Note: The SSV token address used when deploying to live networks (goerli, mainnet) is set in the hardhat config file. To deploy the contracts to a custom network defined in the hardhat config file, leave `SSVTOKEN_ADDRESS` empty in the `.env` file. You can set a specific SSV token address for custom networks too, if needed.

To run the deployment, execute:

```sh
npx hardhat --network <network> deploy:all
```

Output of this action will be:

```sh
Deploying contracts with the account:0xf39...
SSVOperators module deployed to: 0x5Fb...
SSVClsuters module deployed to: 0xe7f1...
SSVDAO module deployed to: 0x9fE4...
SSVViews module deployed to: 0xCf7E...
Deploying SSVNetwork with ssvToken 0x3a9f...
SSVNetwork proxy deployed to: 0x5FC8...
SSVNetwork implementation deployed to: 0xDc64...
Deploying SSVNetworkViews with SSVNetwork 0x5FC8...
SSVNetworkViews proxy deployed to: 0xa513...
SSVNetworkViews implementation deployed to: 0x0165...
```

As general rule, you can target any network configured in the `hardhat.config.ts`, specifying the right [network]\_ETH_NODE_URL and [network]\_OWNER_PRIVATE_KEY in `.env` file.

### Verification on etherscan (only public networks)

You can now go to Etherscan and see:

- `SSVNetwork` proxy contract is deployed to the address shown previously in `SSVNetwork proxy deployed to`
- `SSVNetwork` implementation contract is deployed to the address shown previously in `SSVNetwork implementation deployed to`
- `SSVNetworkViews` proxy contract is deployed to the address shown previously in `SSVNetworkViews proxy deployed to`
- `SSVNetworkViews` implementation contract is deployed to the address shown previously in `SSVNetworkViews implementation deployed to`

Open `.openzeppelin/<network>.json` file and find `[impls.<hash>.address]` value which is the implementation smart contract address.
You will find 2 `[impls.<hash>]` entries, one for `SSVNetwork` and another for `SSVNetworkViews`.
Run this verification process for both.

You can take it from the output of the `npx hardhat --network <network> deploy:all` command.

To verify a proxy contract (SSVNetwork, SSVNetworkViews), run this:

```sh
npx hardhat verify --network <network> <proxy-address>
```

By verifying a contract using its proxy address, the verification process for both the proxy and the implementation contracts is conducted seamlessly.
The proxy contract is automatically linked to the implementation contract.
As a result, users will be able to view interfaces of both the proxy and the implementation contracts on the Etherscan website's contract page, ensuring comprehensive visibility and transparency.

To verify a module contract (SSVClusters, SSVOperators, SSVDAO, SSVViews), run this:

```sh
npx hardhat verify --network <network> <implementation-address>
```

Output of this action will be:

```sh
Nothing to compile
No need to generate any newer typings.
Successfully submitted source code for contract
contracts/SSVNetwork.sol:SSVNetwork at 0x2279B7...
for verification on the block explorer. Waiting for verification result...

Successfully verified contract SSVNetwork on Etherscan.
https://goerli.etherscan.io/address/0x227...#code
```

After this action, you can go to the proxy contract in Etherscan and start interacting with it.

### How to resolve issues during the verification

- Error: no such file or directory, open ‘…/artifacts/build-info/XXXX...XXXX.json’

This issue can be resolved by executing the following commands.

```sh
npx hardhat clean
npx hardhat compile
```
