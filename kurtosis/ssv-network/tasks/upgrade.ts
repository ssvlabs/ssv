import { task, types } from 'hardhat/config';

/**
@title Hardhat task to upgrade a UUPS proxy contract.
This task upgrades a UUPS proxy contract deployed in a network to a new version.
It uses the OpenZeppelin Upgrades plugin for Hardhat to safely perform the upgrade operation.
@param {string} proxyAddress - The address of the existing UUPS proxy contract: SSVNetwork or SSVNetworkViews.
@param {string} contract - The name of the new contract that will replace the old one.
The contract should already be compiled and exist in the artifacts directory.
@param {string} [initFunction] - An optional function to be executed after the upgrade.
This function should be a method of the new contract and will be invoked as part of the upgrade transaction.
If not provided, no function will be called.
@param {Array} [params] - An optional array of parameters to the 'initFunction'.
The parameters should be ordered as expected by the 'initFunction'.
If 'initFunction' is not provided, this parameter has no effect.
@returns {void} This function doesn't return anything. After successfully upgrading, it prints the new implementation address to the console.
@example
// Upgrade the SSVNetwork contract to a new version 'SSVNetworkV2', and call a function 'initializev2' with parameters after upgrade:
npx hardhat --network goerli_testnet upgrade:proxy --proxyAddress 0x1234... --contract SSVNetworkV2 --initFunction initializev2 --params param1 param2
*/
task('upgrade:proxy', 'Upgrade SSVNetwork / SSVNetworkViews proxy via hardhat upgrades plugin')
  .addParam('proxyAddress', 'Proxy address of SSVNetwork / SSVNetworkViews', null, types.string)
  .addParam('contract', 'New contract upgrade', null, types.string)
  .addOptionalParam('initFunction', 'Function to be executed after upgrading')
  .addOptionalVariadicPositionalParam('params', 'Function parameters')
  .setAction(async ({ proxyAddress, contract, initFunction, params }, hre) => {
    // Triggering compilation
    await hre.run('compile');

    // Upgrading proxy
    const [deployer] = await ethers.getSigners();
    console.log(`Upgading ${proxyAddress} with the account: ${deployer.address}`);

    const SSVUpgradeFactory = await ethers.getContractFactory(contract);

    const ssvUpgrade = await upgrades.upgradeProxy(proxyAddress, SSVUpgradeFactory, {
      kind: 'uups',
      call: initFunction
        ? {
            fn: initFunction,
            args: params ? params : '',
          }
        : '',
    });
    await ssvUpgrade.deployed();
    console.log(`${proxyAddress} upgraded successfully`);

    const implAddress = await upgrades.erc1967.getImplementationAddress(ssvUpgrade.address);
    console.log(`Implementation deployed to: ${implAddress}`);
  });

/**
@title Hardhat task to prepare the upgrade of the SSVNetwork or SSVNetworkViews proxy contract.
This task is responsible for preparing an upgrade to a SSVNetwork or SSVNetworkViews proxy contract using the Hardhat Upgrades Plugin.
The task deploys the new implementation contract for the upgrade and outputs the address of the new implementation.
The function takes as input the proxy address to be upgraded and the contract to use for the upgrade. 
The proxy address and contract name must be provided as parameters.
@param {string} proxyAddress - The proxy address of the SSVNetwork or SSVNetworkViews contract to be upgraded.
@param {string} contract - The name of the new implementation contract to deploy for the upgrade.
@example
// Prepare an upgrade for the SSVNetworkViews proxy contract with a new implementation contract named 'SSVNetworkViewsV2'
npx hardhat --network goerli_testnet upgrade:prepare --proxyAddress 0x1234... --contract SSVNetworkViewsV2
@remarks
The deployer account used will be the first one returned by ethers.getSigners().
Therefore, it should be appropriately configured in your Hardhat network configuration.
The new implementation contract specified should be already compiled and exist in the 'artifacts' directory.
*/
task('upgrade:prepare', 'Prepares the upgrade of SSVNetwork / SSVNetworkViews proxy')
  .addParam('proxyAddress', 'Proxy address of SSVNetwork / SSVNetworkViews', null, types.string)
  .addParam('contract', 'New contract upgrade', null, types.string)
  .setAction(async ({ proxyAddress, contract }, hre) => {
    // Triggering compilation
    await hre.run('compile');

    const [deployer] = await ethers.getSigners();
    console.log(`Preparing the upgrade of ${proxyAddress} with the account: ${deployer.address}`);

    const SSVUpgradeFactory = await ethers.getContractFactory(contract);

    const implAddress = await upgrades.prepareUpgrade(proxyAddress, SSVUpgradeFactory, {
      kind: 'uups',
    });
    console.log(`Implementation deployed to: ${implAddress}`);
  });
