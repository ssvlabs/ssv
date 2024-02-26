// Imports
import { CONFIG, DB, initializeContract, DataGenerator } from '../helpers/contract-helpers';
import { trackGas } from '../helpers/gas-usage';
import { ethers, upgrades } from 'hardhat';
import { expect } from 'chai';

describe('Deployment tests', () => {
  let ssvNetworkContract: any, ssvNetworkViews: any, ssvToken: any;

  beforeEach(async () => {
    const metadata = await initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvNetworkViews = metadata.ssvViews;
    ssvToken = metadata.ssvToken;
  });

  it('Check default values after deploying', async () => {
    expect(await ssvNetworkViews.getNetworkValidatorsCount()).to.equal(0);
    expect(await ssvNetworkViews.getNetworkEarnings()).to.equal(0);
    expect(await ssvNetworkViews.getOperatorFeeIncreaseLimit()).to.equal(CONFIG.operatorMaxFeeIncrease);
    expect(await ssvNetworkViews.getOperatorFeePeriods()).to.deep.equal([
      CONFIG.declareOperatorFeePeriod,
      CONFIG.executeOperatorFeePeriod,
    ]);
    expect(await ssvNetworkViews.getLiquidationThresholdPeriod()).to.equal(CONFIG.minimalBlocksBeforeLiquidation);
    expect(await ssvNetworkViews.getMinimumLiquidationCollateral()).to.equal(CONFIG.minimumLiquidationCollateral);
    expect(await ssvNetworkViews.getValidatorsPerOperatorLimit()).to.equal(CONFIG.validatorsPerOperatorLimit);
    expect(await ssvNetworkViews.getOperatorFeeIncreaseLimit()).to.equal(CONFIG.operatorMaxFeeIncrease);
  });

  it('Upgrade SSVNetwork contract. Check new function execution', async () => {
    await ssvNetworkContract
      .connect(DB.owners[1])
      .registerOperator(DataGenerator.publicKey(0), CONFIG.minimalOperatorFee);

    const BasicUpgrade = await ethers.getContractFactory('SSVNetworkBasicUpgrade');
    const ssvNetworkUpgrade = await upgrades.upgradeProxy(ssvNetworkContract.address, BasicUpgrade, {
      kind: 'uups',
      unsafeAllow: ['delegatecall'],
    });
    await ssvNetworkUpgrade.deployed();

    await ssvNetworkUpgrade.resetNetworkFee(10000000);
    expect(await ssvNetworkViews.getNetworkFee()).to.equal(10000000);
  });

  it('Upgrade SSVNetwork contract. Deploy implemetation manually', async () => {
    const SSVNetwork = await ethers.getContractFactory('SSVNetwork');
    const BasicUpgrade = await ethers.getContractFactory('SSVNetworkBasicUpgrade');

    // Get current SSVNetwork proxy
    const ssvNetwork = SSVNetwork.attach(ssvNetworkContract.address);

    // Deploy a new implementation with another account
    const contractImpl = await BasicUpgrade.connect(DB.owners[1]).deploy();
    await contractImpl.deployed();

    const newNetworkFee = ethers.utils.parseUnits('10000000', 'wei');
    const calldata = contractImpl.interface.encodeFunctionData('resetNetworkFee', [newNetworkFee]);

    // The owner of SSVNetwork contract peforms the upgrade
    await ssvNetwork.upgradeToAndCall(contractImpl.address, calldata);

    expect(await ssvNetworkViews.getNetworkFee()).to.equal(10000000);
  });

  it('Upgrade SSVNetwork contract. Check base contract is not re-initialized', async () => {
    const BasicUpgrade = await ethers.getContractFactory('SSVNetworkBasicUpgrade');
    const ssvNetworkUpgrade = await upgrades.upgradeProxy(ssvNetworkContract.address, BasicUpgrade, {
      kind: 'uups',
      unsafeAllow: ['delegatecall'],
    });
    await ssvNetworkUpgrade.deployed();

    const address = await upgrades.erc1967.getImplementationAddress(ssvNetworkUpgrade.address);
    const instance = await ssvNetworkUpgrade.attach(address);

    await expect(
      instance
        .connect(DB.owners[1])
        .initialize(
          '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
          '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
          '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
          '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
          '0x6471F70b932390f527c6403773D082A0Db8e8A9F',
          2000000,
          2000000,
          2000000,
          2000000,
          2000000,
          2000,
        ),
    ).to.be.revertedWith('Initializable: contract is already initialized');
  });

  it('Upgrade SSVNetwork contract. Check state is only changed from proxy contract', async () => {
    const BasicUpgrade = await ethers.getContractFactory('SSVNetworkBasicUpgrade');
    const ssvNetworkUpgrade = await upgrades.upgradeProxy(ssvNetworkContract.address, BasicUpgrade, {
      kind: 'uups',
      unsafeAllow: ['delegatecall'],
    });
    await ssvNetworkUpgrade.deployed();

    const address = await upgrades.erc1967.getImplementationAddress(ssvNetworkUpgrade.address);
    const instance = await ssvNetworkUpgrade.attach(address);

    await trackGas(instance.connect(DB.owners[1]).resetNetworkFee(100000000000));

    expect(await ssvNetworkViews.getNetworkFee()).to.be.equals(0);
  });

  it('Update a module (SSVOperators)', async () => {
    const ssvNetworkFactory = await ethers.getContractFactory('SSVNetwork');
    const ssvNetwork = await ssvNetworkFactory.attach(ssvNetworkContract.address);

    const ssvOperatorsFactory = await ethers.getContractFactory('SSVOperatorsUpdate');

    const operatorsImpl = await ssvOperatorsFactory.connect(DB.owners[1]).deploy();
    await operatorsImpl.deployed();

    await expect(ssvNetwork.updateModule(0, ethers.constants.AddressZero)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'TargetModuleDoesNotExist',
    );

    await ssvNetwork.updateModule(0, operatorsImpl.address);

    await expect(ssvNetworkContract.declareOperatorFee(0, 0)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'NoFeeDeclared',
    );
  });

  it('ETH can not be transferred to SSVNetwork / SSVNetwork views', async () => {
    const amount = ethers.utils.parseUnits('10000000', 'wei');

    const sender = ethers.provider.getSigner(0);

    await expect(
      sender.sendTransaction({
        to: ssvNetworkContract.address,
        value: amount,
      }),
    ).to.be.reverted;

    await expect(
      sender.sendTransaction({
        to: ssvNetworkViews.address,
        value: amount,
      }),
    ).to.be.reverted;

    expect(await ethers.provider.getBalance(ssvNetworkContract.address)).to.be.equal(0);
    expect(await ethers.provider.getBalance(ssvNetworkViews.address)).to.be.equal(0);
  });
});
