// Declare imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, minDepositAmount: any, burnPerBlock: any, networkFee: any;

describe('DAO Network Fee Withdraw Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Define minumum allowed network fee to pass shrinkable validation
    networkFee = helpers.CONFIG.minimalOperatorFee;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    burnPerBlock = helpers.CONFIG.minimalOperatorFee * 4 + networkFee;
    minDepositAmount = helpers.CONFIG.minimalBlocksBeforeLiquidation * burnPerBlock;

    // Set network fee
    await ssvNetworkContract.updateNetworkFee(networkFee);

    // Register validators
    // cold register
    await helpers.coldRegisterValidator();

    await helpers.registerValidators(
      4,
      minDepositAmount,
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    await utils.progressBlocks(10);
  });

  it('Withdraw network earnings emits "NetworkEarningsWithdrawn"', async () => {
    const amount = await ssvViews.getNetworkEarnings();
    await expect(ssvNetworkContract.withdrawNetworkEarnings(amount))
      .to.emit(ssvNetworkContract, 'NetworkEarningsWithdrawn')
      .withArgs(amount, helpers.DB.owners[0].address);
  });

  it('Withdraw network earnings gas limits', async () => {
    const amount = await ssvViews.getNetworkEarnings();
    await trackGas(ssvNetworkContract.withdrawNetworkEarnings(amount), [GasGroup.WITHDRAW_NETWORK_EARNINGS]);
  });

  it('Get withdrawable network earnings', async () => {
    expect(await ssvViews.getNetworkEarnings()).to.above(0);
  });

  it('Get withdrawable network earnings as not owner', async () => {
    await ssvViews.connect(helpers.DB.owners[3]).getNetworkEarnings();
  });

  it('Withdraw network earnings with not enough balance reverts "InsufficientBalance"', async () => {
    const amount = (await ssvViews.getNetworkEarnings()) * 2;
    await expect(ssvNetworkContract.withdrawNetworkEarnings(amount)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'InsufficientBalance',
    );
  });

  it('Withdraw network earnings from an address thats not the DAO reverts "caller is not the owner"', async () => {
    const amount = await ssvViews.getNetworkEarnings();
    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).withdrawNetworkEarnings(amount)).to.be.revertedWith(
      'Ownable: caller is not the owner',
    );
  });

  it('Withdraw network earnings providing UINT64 max value reverts "Max value exceeded"', async () => {
    const amount = ethers.BigNumber.from(2)
      .pow(64)
      .mul(ethers.BigNumber.from(1e8));
    await expect(ssvNetworkContract.withdrawNetworkEarnings(amount)).to.be.revertedWith('Max value exceeded');
  });

  it('Withdraw network earnings sequentially when not enough balance reverts "InsufficientBalance"', async () => {
    const amount = (await ssvViews.getNetworkEarnings()) / 2;

    await ssvNetworkContract.withdrawNetworkEarnings(amount);
    expect(await ssvViews.getNetworkEarnings()).to.be.equals(networkFee * 13 + networkFee * 11 - amount);

    await ssvNetworkContract.withdrawNetworkEarnings(amount);
    expect(await ssvViews.getNetworkEarnings()).to.be.equals(networkFee * 14 + networkFee * 12 - amount * 2);

    await expect(ssvNetworkContract.withdrawNetworkEarnings(amount)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'InsufficientBalance',
    );
  });
});
