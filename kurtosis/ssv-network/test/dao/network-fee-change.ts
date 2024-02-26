// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, networkFee: any;

describe('Network Fee Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = (await helpers.initializeContract());
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Define minumum allowed network fee to pass shrinkable validation
    networkFee = helpers.CONFIG.minimalOperatorFee / 10;
  });

  it('Change network fee emits "NetworkFeeUpdated"', async () => {
    await expect(ssvNetworkContract.updateNetworkFee(networkFee
    )).to.emit(ssvNetworkContract, 'NetworkFeeUpdated').withArgs(0, networkFee);
  });

  it('Change network fee providing UINT64 max value reverts "Max value exceeded"', async () => {
    const amount = (ethers.BigNumber.from(2).pow(64)).mul(ethers.BigNumber.from(1e8));
    await expect(ssvNetworkContract.updateNetworkFee(amount
    )).to.be.revertedWith('Max value exceeded');
  });

  it('Change network fee when it was set emits "NetworkFeeUpdated"', async () => {
    const initialNetworkFee = helpers.CONFIG.minimalOperatorFee;
    await ssvNetworkContract.updateNetworkFee(initialNetworkFee);

    await expect(ssvNetworkContract.updateNetworkFee(networkFee
    )).to.emit(ssvNetworkContract, 'NetworkFeeUpdated').withArgs(initialNetworkFee, networkFee);
  });

  it('Change network fee gas limit', async () => {
    await trackGas(ssvNetworkContract.updateNetworkFee(networkFee), [GasGroup.NETWORK_FEE_CHANGE]);
  });

  it('Get network fee', async () => {
    expect(await ssvViews.getNetworkFee()).to.equal(0);
  });

  it('Change the network fee to a number below the minimum fee reverts "Max precision exceeded"', async () => {
    await expect(ssvNetworkContract.updateNetworkFee(networkFee - 1
    )).to.be.revertedWith('Max precision exceeded');
  });

  it('Change the network fee to a number that exceeds allowed type limit reverts "Max value exceeded"', async () => {
    await expect(ssvNetworkContract.updateNetworkFee(BigInt(2 ** 64) * BigInt(10000000) + BigInt(1),
    )).to.be.revertedWith('Max value exceeded');
  });

  it('Change network fee from an address thats not the DAO reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).updateNetworkFee(networkFee
    )).to.be.revertedWith('Ownable: caller is not the owner');
  });
});