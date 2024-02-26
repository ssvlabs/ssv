import { task, types } from 'hardhat/config';
import { SSVModules } from './config';

/**
@title Hardhat task to update a module contract in the SSVNetwork.
This task first deploys a new version of a specified SSV module contract, and then updates the SSVNetwork contract to use this new module version.
The module's name is required and it's expected to match one of the SSVModules enumeration values.
The address of the SSVNetwork Proxy is expected to be set in the environment variable SSVNETWORK_PROXY_ADDRESS.
@param {string} module - The name of the SSV module to be updated.
@param {boolean} attachModule - Flag to attach new deployed module to SSVNetwork contract. Dafaults to true.
@param {string} proxyAddress - The proxy address of the SSVNetwork contract.
@example
// Update 'SSVOperators' module contract in the SSVNetwork
npx hardhat --network goerli_testnet update:module --module SSVOperators
@remarks
The deployer account used will be the first one returned by ethers.getSigners().
Therefore, it should be appropriately configured in your Hardhat network configuration.
The module's contract specified should be already compiled and exist in the 'artifacts' directory.
*/
task('update:module', 'Deploys a new module contract and links it to SSVNetwork')
  .addParam('module', 'SSV Module', null, types.string)
  .addOptionalParam('attachModule', 'Attach module to SSVNetwork contract', false, types.boolean)
  .addOptionalParam('proxyAddress', 'Proxy address of SSVNetwork / SSVNetworkViews', '', types.string)
  .setAction(async ({ module, attachModule, proxyAddress }, hre) => {
    if (attachModule && !proxyAddress) throw new Error('SSVNetwork proxy address not set.');

    const [deployer] = await ethers.getSigners();
    console.log(`Deploying contracts with the account: ${deployer.address}`);
    const moduleAddress = await hre.run('deploy:module', { module });

    if (attachModule) {
      if (!proxyAddress) throw new Error('SSVNetwork proxy address not set.');

      const ssvNetworkFactory = await ethers.getContractFactory('SSVNetwork');
      const ssvNetwork = await ssvNetworkFactory.attach(proxyAddress);

      await ssvNetwork.updateModule(SSVModules[module], moduleAddress);
      console.log(`${module} module attached to SSVNetwork succesfully`);
    }
  });
