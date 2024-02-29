import 'dotenv/config';

import {HardhatUserConfig} from 'hardhat/config';
import {NetworkUserConfig} from 'hardhat/types';
import '@nomicfoundation/hardhat-toolbox';
import '@openzeppelin/hardhat-upgrades';
import 'hardhat-tracer';
import '@nomiclabs/hardhat-solhint';
import 'hardhat-contract-sizer';
import 'hardhat-storage-layout-changes';
import 'hardhat-abi-exporter';
import './tasks/deploy';
import './tasks/update-module';
import './tasks/upgrade';

var local_rpc_uri = process.env.RPC_URI || "http://127.0.0.1:9650/ext/bc/C/rpc"

type SSVNetworkConfig = NetworkUserConfig & {
  ssvToken: string;
};

const config: HardhatUserConfig = {
  // Your type-safe config goes here
  mocha: {
    timeout: 1000,
  },
  solidity: {
    compilers: [
      {
        version: '0.8.4',
      },
      {
        version: '0.8.18',
        settings: {
          optimizer: {
            enabled: true,
            runs: 10000,
          },
        },
      },
    ],
  },
  networks: {
    ganache: {
      chainId: 1337,
      url: 'http://127.0.0.1:8585',
      ssvToken: process.env.SSVTOKEN_ADDRESS, // if empty, deploy SSV mock token
    } as SSVNetworkConfig,
    hardhat: {
      allowUnlimitedContractSize: true,
      gas: 5000000,
    },
    localnet: {
      url: local_rpc_uri,
      // These are private keys associated with prefunded test accounts created by the eth-network-package
      // https://github.com/kurtosis-tech/eth-network-package/blob/main/src/prelaunch_data_generator/genesis_constants/genesis_constants.star
      accounts: [
        "bcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31",
        "39725efee3fb28614de3bacaffe4cc4bd8c436257e2c8bb887c4b5c4be45e76d",
        "53321db7c1e331d93a11a41d16f004d7ff63972ec8ec7c25db329728ceeb1710",
        "ab63b23eb7941c1251757e24b3d2350d2bc05c3c388d06f8fe6feafefb1e8c70",
        "5d2344259f42259f82d2c140aa66102ba89b57b4883ee441a8b312622bd42491",
        "27515f805127bebad2fb9b183508bdacb8c763da16f54e0678b16e8f28ef3fff",
        "7ff1a4c1d57e5e784d327c4c7651e952350bc271f156afb3d00d20f5ef924856",
        "3a91003acaf4c21b3953d94fa4a6db694fa69e5242b2e37be05dd82761058899",
        "bb1d0f125b4fb2bb173c318cdead45468474ca71474e2247776b2b4c0fa2d3f5",
        "850643a0224065ecce3882673c21f56bcf6eef86274cc21cadff15930b59fc8c",
        "94eb3102993b41ec55c241060f47daa0f6372e2e3ad7e91612ae36c364042e44",
      ],
      allowUnlimitedContractSize: true,
      ssvToken: process.env.SSVTOKEN_ADDRESS,
    },
  },
  etherscan: {
    // Your API key for Etherscan
    // Obtain one at https://etherscan.io/
    apiKey: process.env.ETHERSCAN_KEY,
    customChains: [
      {
        network: 'holesky',
        chainId: 17000,
        urls: {
          apiURL: 'https://api-holesky.etherscan.io/api',
          browserURL: 'https://holesky.etherscan.io',
        },
      },
    ],
  },
  gasReporter: {
    enabled: true,
    currency: 'USD',
    gasPrice: 0.3,
  },
  contractSizer: {
    alphaSort: true,
    disambiguatePaths: false,
    runOnCompile: true,
    strict: false,
  },
  abiExporter: {
    path: './abis',
    runOnCompile: true,
    clear: true,
    flat: true,
    spacing: 2,
    pretty: false,
    only: ['contracts/SSVNetwork.sol', 'contracts/SSVNetworkViews.sol'],
  },
};

if (process.env.GOERLI_ETH_NODE_URL) {
  const sharedConfig = {
    url: process.env.GOERLI_ETH_NODE_URL,
    accounts: [`0x${process.env.GOERLI_OWNER_PRIVATE_KEY}`],
    gasPrice: +(process.env.GAS_PRICE || ''),
    gas: +(process.env.GAS || ''),
  };
  //@ts-ignore
  config.networks = {
    ...config.networks,
    goerli_development: {
      ...sharedConfig,
      ssvToken: '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
    } as SSVNetworkConfig,
    goerli_testnet: {
      ...sharedConfig,
      ssvToken: '0x3a9f01091C446bdE031E39ea8354647AFef091E7',
    } as SSVNetworkConfig,
  };
}

if (process.env.HOLESKY_ETH_NODE_URL) {
  const sharedConfig = {
    url: process.env.HOLESKY_ETH_NODE_URL,
    accounts: [`0x${process.env.HOLESKY_OWNER_PRIVATE_KEY}`],
    gasPrice: +(process.env.GAS_PRICE || ''),
    gas: +(process.env.GAS || ''),
  };
  //@ts-ignore
  config.networks = {
    ...config.networks,
    holesky_development: {
      ...sharedConfig,
      ssvToken: '0x68A8DDD7a59A900E0657e9f8bbE02B70c947f25F',
    } as SSVNetworkConfig,
    holesky_testnet: {
      ...sharedConfig,
      ssvToken: '0xad45A78180961079BFaeEe349704F411dfF947C6',
    } as SSVNetworkConfig,
  };
}

if (process.env.MAINNET_ETH_NODE_URL) {
  //@ts-ignore
  config.networks = {
    ...config.networks,
    mainnet: {
      url: process.env.MAINNET_ETH_NODE_URL,
      accounts: [`0x${process.env.MAINNET_OWNER_PRIVATE_KEY}`],
      gasPrice: +(process.env.GAS_PRICE || ''),
      gas: +(process.env.GAS || ''),
      ssvToken: '0x9D65fF81a3c488d585bBfb0Bfe3c7707c7917f54',
    } as SSVNetworkConfig,
  };
}

export default config;
