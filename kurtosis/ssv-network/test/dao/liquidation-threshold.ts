// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, networkFee: any;

describe('Liquidation Threshold Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = (await helpers.initializeContract());
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Define minumum allowed network fee to pass shrinkable validation
    networkFee = helpers.CONFIG.minimalOperatorFee / 10;
  });

  it('Change liquidation threshold period emits "LiquidationThresholdPeriodUpdated"', async () => {
    await expect(ssvNetworkContract.updateLiquidationThresholdPeriod(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10)).to.emit(ssvNetworkContract, 'LiquidationThresholdPeriodUpdated').withArgs(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
  });

  it('Change liquidation threshold period gas limits', async () => {
    await trackGas(ssvNetworkContract.updateLiquidationThresholdPeriod(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10), 
    [GasGroup.CHANGE_LIQUIDATION_THRESHOLD_PERIOD]);
  });

  it('Get liquidation threshold period', async () => {
    expect(await ssvViews.getLiquidationThresholdPeriod()).to.equal(helpers.CONFIG.minimalBlocksBeforeLiquidation);
  });

  it('Change liquidation threshold period reverts "NewBlockPeriodIsBelowMinimum"', async () => {
    await expect(ssvNetworkContract.updateLiquidationThresholdPeriod(helpers.CONFIG.minimalBlocksBeforeLiquidation - 10)).to.be.revertedWithCustomError(ssvNetworkContract, 'NewBlockPeriodIsBelowMinimum');
  });

  it('Change liquidation threshold period reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).updateLiquidationThresholdPeriod(helpers.CONFIG.minimalBlocksBeforeLiquidation)).to.be.revertedWith('Ownable: caller is not the owner');
  });
});