// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, networkFee: any;

describe('Liquidation Collateral Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = (await helpers.initializeContract());
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Define minumum allowed network fee to pass shrinkable validation
    networkFee = helpers.CONFIG.minimalOperatorFee / 10;
  });

  it('Change minimum collateral emits "MinimumLiquidationCollateralUpdated"', async () => {
    await expect(ssvNetworkContract.updateMinimumLiquidationCollateral(helpers.CONFIG.minimumLiquidationCollateral * 2))
      .to.emit(ssvNetworkContract, 'MinimumLiquidationCollateralUpdated')
      .withArgs(helpers.CONFIG.minimumLiquidationCollateral * 2);
  });

  it('Change minimum collateral gas limits', async () => {
    await trackGas(ssvNetworkContract.updateMinimumLiquidationCollateral(helpers.CONFIG.minimumLiquidationCollateral * 2),
    [GasGroup.CHANGE_MINIMUM_COLLATERAL]);
  });

  it('Get minimum collateral', async () => {
    expect(await ssvViews.getMinimumLiquidationCollateral()).to.equal(helpers.CONFIG.minimumLiquidationCollateral);
  });


  it('Change minimum collateral reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).updateMinimumLiquidationCollateral(helpers.CONFIG.minimumLiquidationCollateral * 2))
    .to.be.revertedWith('Ownable: caller is not the owner');
  });
});
