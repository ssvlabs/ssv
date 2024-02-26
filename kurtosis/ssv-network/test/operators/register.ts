// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any;

describe('Register Operator Tests', () => {
  beforeEach(async () => {
    const metadata = (await helpers.initializeContract());
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;
  });

  it('Register operator emits "OperatorAdded"', async () => {
    const publicKey = helpers.DataGenerator.publicKey(0);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      publicKey,
      helpers.CONFIG.minimalOperatorFee
    )).to.emit(ssvNetworkContract, 'OperatorAdded').withArgs(1, helpers.DB.owners[1].address, publicKey, helpers.CONFIG.minimalOperatorFee);
  });

  it('Register operator gas limits', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee
    ), [GasGroup.REGISTER_OPERATOR]);
  });

  it('Get operator by id', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee,
    );

    expect(await ssvViews.getOperatorById(1)).to.deep.equal(
      [helpers.DB.owners[1].address, // owner
      helpers.CONFIG.minimalOperatorFee, // fee
        0, // validatorCount
        ethers.constants.AddressZero, // whitelisted
        false, // isPrivate
        true // active
      ]);
  });

  it('Get private operator by id', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee
    );

    await ssvNetworkContract.connect(helpers.DB.owners[1]).setOperatorWhitelist(1, helpers.DB.owners[2].address);

    expect(await ssvViews.getOperatorById(1)).to.deep.equal(
      [helpers.DB.owners[1].address, // owner
      helpers.CONFIG.minimalOperatorFee, // fee
        0, // validatorCount
        helpers.DB.owners[2].address, // whitelisted
        true, // isPrivate
        true // active
      ]);
  });

  it('Set operator whitelist gas limits', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee
    );

    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).setOperatorWhitelist(1, helpers.DB.owners[2].address), 
    [GasGroup.SET_OPERATOR_WHITELIST]);
   
  });

  it('Get non-existent operator by id', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee
    );

    expect(await ssvViews.getOperatorById(5)).to.deep.equal(
      [ethers.constants.AddressZero, // owner
        0, // fee
        0, // validatorCount
        ethers.constants.AddressZero, // whitelisted
        false, // isPrivate
        false // active
      ]);
  });

  it('Get operator removed by id', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      helpers.DataGenerator.publicKey(0),
      helpers.CONFIG.minimalOperatorFee
    );
    await ssvNetworkContract.connect(helpers.DB.owners[1]).removeOperator(1);

    expect(await ssvViews.getOperatorById(1)).to.deep.equal(
      [helpers.DB.owners[1].address, // owner
        0, // fee
        0, // validatorCount
        ethers.constants.AddressZero, // whitelisted
        false, // isPrivate
        false // active
      ]);
  });

  it('Register an operator with a fee thats too low reverts "FeeTooLow"', async () => {
    await expect(ssvNetworkContract.registerOperator(
      helpers.DataGenerator.publicKey(0),
      '10',
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeTooLow');
  });

  it('Register an operator with a fee thats too high reverts "FeeTooHigh"', async () => {
    await expect(ssvNetworkContract.registerOperator(
      helpers.DataGenerator.publicKey(0),
      2e14,
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeTooHigh');
  });

  it('Register same operator twice reverts "OperatorAlreadyExists"', async () => {
    const publicKey = helpers.DataGenerator.publicKey(1);
    await ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      publicKey,
      helpers.CONFIG.minimalOperatorFee
    );

    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).registerOperator(
      publicKey,
      helpers.CONFIG.minimalOperatorFee
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorAlreadyExists');
  });

});