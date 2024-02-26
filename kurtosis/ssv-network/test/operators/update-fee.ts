// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { progressTime } from '../helpers/utils';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, initialFee: any;

describe('Operator Fee Tests', () => {
  beforeEach(async () => {
    const metadata = (await helpers.initializeContract());
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    initialFee = helpers.CONFIG.minimalOperatorFee * 10;
    await helpers.registerOperators(2, 1, initialFee);
  });

  it('Declare fee emits "OperatorFeeDeclared"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10
    )).to.emit(ssvNetworkContract, 'OperatorFeeDeclared');
  });

  it('Declare fee gas limits"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10
    ), [GasGroup.DECLARE_OPERATOR_FEE]);
  });

  it('Declare fee with zero value emits "OperatorFeeDeclared"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, 0
    )).to.emit(ssvNetworkContract, 'OperatorFeeDeclared');
  });

  it('Declare a lower fee gas limits', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee - initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
  });

  it('Declare a higher fee gas limit', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
  });

  it('Cancel declared fee emits "OperatorFeeDeclarationCancelled"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).cancelDeclaredOperatorFee(1
    )).to.emit(ssvNetworkContract, 'OperatorFeeDeclarationCancelled');
  });

  it('Cancel declared fee gas limits', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).cancelDeclaredOperatorFee(1), [GasGroup.CANCEL_OPERATOR_FEE]);
  });

  it('Execute declared fee emits "OperatorFeeExecuted"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1
    )).to.emit(ssvNetworkContract, 'OperatorFeeExecuted');
  });

  it('Execute declared fee gas limits', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod);
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1), [GasGroup.EXECUTE_OPERATOR_FEE]);
  });

  it('Get operator fee', async () => {
    expect(await ssvViews.getOperatorFee(1)).to.equal(initialFee);
  });

  it('Get fee from operator that does not exist returns 0', async () => {
    expect(await ssvViews.getOperatorFee(12)).to.equal(0);
  });

  it('Get operator maximum fee limit', async () => {
    expect(await ssvViews.getMaximumOperatorFee()).to.equal(helpers.CONFIG.maximumOperatorFee);
  });

  it('Declare fee of operator I do not own reverts "CallerNotOwner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).declareOperatorFee(1, initialFee + initialFee / 10
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotOwner');
  });

  it('Declare fee with a wrong Publickey reverts "OperatorDoesNotExist"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).declareOperatorFee(12, initialFee + initialFee / 10
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorDoesNotExist');
  });

  it('Declare fee when previously set to zero reverts "FeeIncreaseNotAllowed"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, 0);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod);
    await ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeIncreaseNotAllowed');
  });

  it('Declare same fee value as actual reverts "SameFeeChangeNotAllowed"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee / 10);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod);
    await ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee / 10
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'SameFeeChangeNotAllowed');
  });

  it('Declare fee after registering an operator with zero fee reverts "FeeIncreaseNotAllowed"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).registerOperator(
      helpers.DataGenerator.publicKey(12),
      0);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(2, initialFee + initialFee / 10
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeIncreaseNotAllowed');
  });

  it('Declare fee above the operators max fee increase limit reverts "FeeExceedsIncreaseLimit"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 5
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeExceedsIncreaseLimit');
  });

  it('Declare fee above the operators max fee limit reverts "FeeTooHigh"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, 2e14
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeTooHigh');
  });

  it('Declare fee too high reverts "FeeTooHigh" -> DAO updates limit -> declare fee emits "OperatorFeeDeclared"', async () => {
    const maxOperatorFee = 8e14;
    await ssvNetworkContract.updateMaximumOperatorFee(maxOperatorFee);

    await ssvNetworkContract.connect(helpers.DB.owners[3]).registerOperator(helpers.DataGenerator.publicKey(10), maxOperatorFee);
    const newOperatorFee = maxOperatorFee + maxOperatorFee / 10;

    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).declareOperatorFee(2, newOperatorFee
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeTooHigh');

    await expect(ssvNetworkContract.updateMaximumOperatorFee(newOperatorFee
    )).to.emit(ssvNetworkContract, 'OperatorMaximumFeeUpdated')
      .withArgs(newOperatorFee);

    await expect(ssvNetworkContract.connect(helpers.DB.owners[3]).declareOperatorFee(2, newOperatorFee
    )).to.emit(ssvNetworkContract, 'OperatorFeeDeclared');
  });

  it('Cancel declared fee without a pending request reverts "NoFeeDeclared"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).cancelDeclaredOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'NoFeeDeclared');
  });

  it('Cancel declared fee of an operator I do not own reverts "CallerNotOwner"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).cancelDeclaredOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotOwner');
  });

  it('Execute declared fee of an operator I do not own reverts "CallerNotOwner"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).executeOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotOwner');
  });

  it('Execute declared fee without a pending request reverts "NoFeeDeclared"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'NoFeeDeclared');
  });

  it('Execute declared fee too early reverts "ApprovalNotWithinTimeframe"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod - 10);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'ApprovalNotWithinTimeframe');
  });

  it('Execute declared fee too late reverts "ApprovalNotWithinTimeframe"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    await progressTime(helpers.CONFIG.declareOperatorFeePeriod + helpers.CONFIG.executeOperatorFeePeriod + 1);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'ApprovalNotWithinTimeframe');
  });

  it('Reduce fee emits "OperatorFeeExecuted"', async () => {
    expect(await ssvNetworkContract.connect(helpers.DB.owners[2]).reduceOperatorFee(1, initialFee / 2)).to.emit(ssvNetworkContract, 'OperatorFeeExecuted');
    expect(await ssvViews.getOperatorFee(1)).to.equal(initialFee / 2);

    expect(await ssvNetworkContract.connect(helpers.DB.owners[2]).reduceOperatorFee(1, 0)).to.emit(ssvNetworkContract, 'OperatorFeeExecuted');
    expect(await ssvViews.getOperatorFee(1)).to.equal(0);
  });

  it('Reduce fee emits "OperatorFeeExecuted"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).reduceOperatorFee(1, initialFee / 2), [GasGroup.REDUCE_OPERATOR_FEE]);

  });

  it('Reduce fee with an increased value reverts "FeeIncreaseNotAllowed"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).reduceOperatorFee(1, initialFee * 2)).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeIncreaseNotAllowed');;
  });

  it('Reduce fee after declaring a fee change', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10);

    expect(await ssvNetworkContract.connect(helpers.DB.owners[2]).reduceOperatorFee(1, initialFee / 2)).to.emit(ssvNetworkContract, 'OperatorFeeExecuted');
    expect(await ssvViews.getOperatorFee(1)).to.equal(initialFee / 2);
    const [isFeeDeclared] = await ssvViews.getOperatorDeclaredFee(1);
    expect(isFeeDeclared).to.equal(false);
  });

  it('Reduce maximum fee limit after declaring a fee change reverts "FeeTooHigh', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10);
    await ssvNetworkContract.updateMaximumOperatorFee(1000);

    await progressTime(helpers.CONFIG.declareOperatorFeePeriod);

    await expect(ssvNetworkContract.connect(helpers.DB.owners[2]).executeOperatorFee(1
    )).to.be.revertedWithCustomError(ssvNetworkContract, 'FeeTooHigh');
  });

  //Dao
  it('DAO increase the fee emits "OperatorFeeIncreaseLimitUpdated"', async () => {
    await expect(ssvNetworkContract.updateOperatorFeeIncreaseLimit(1000
    )).to.emit(ssvNetworkContract, 'OperatorFeeIncreaseLimitUpdated');
  });

  it('DAO update the maximum operator fee emits "OperatorMaximumFeeUpdated"', async () => {
    await expect(ssvNetworkContract.updateMaximumOperatorFee(2e10
    )).to.emit(ssvNetworkContract, 'OperatorMaximumFeeUpdated')
      .withArgs(2e10);
  });

  it('DAO increase the fee gas limits"', async () => {
    await trackGas(ssvNetworkContract.updateOperatorFeeIncreaseLimit(1000
    ), [GasGroup.DAO_UPDATE_OPERATOR_FEE_INCREASE_LIMIT]);
  });

  it('DAO update the declare fee period emits "DeclareOperatorFeePeriodUpdated"', async () => {
    await expect(ssvNetworkContract.updateDeclareOperatorFeePeriod(1200
    )).to.emit(ssvNetworkContract, 'DeclareOperatorFeePeriodUpdated');
  });

  it('DAO update the declare fee period gas limits"', async () => {
    await trackGas(ssvNetworkContract.updateDeclareOperatorFeePeriod(1200
    ), [GasGroup.DAO_UPDATE_DECLARE_OPERATOR_FEE_PERIOD]);
  });

  it('DAO update the execute fee period emits "ExecuteOperatorFeePeriodUpdated"', async () => {
    await expect(ssvNetworkContract.updateExecuteOperatorFeePeriod(1200
    )).to.emit(ssvNetworkContract, 'ExecuteOperatorFeePeriodUpdated');
  });

  it('DAO update the execute fee period gas limits', async () => {
    await trackGas(ssvNetworkContract.updateExecuteOperatorFeePeriod(1200
    ), [GasGroup.DAO_UPDATE_EXECUTE_OPERATOR_FEE_PERIOD]);
  });

  it('DAO update the maximum fee for operators using SSV gas limits', async () => {
    await trackGas(ssvNetworkContract.updateMaximumOperatorFee(2e10
    ), [GasGroup.DAO_UPDATE_OPERATOR_MAX_FEE]);
  });

  it('DAO get fee increase limit', async () => {
    expect(await ssvViews.getOperatorFeeIncreaseLimit()).to.equal(helpers.CONFIG.operatorMaxFeeIncrease);
  });

  it('DAO get declared fee', async () => {
    const newFee = initialFee + initialFee / 10;
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, newFee), [GasGroup.REGISTER_OPERATOR]);
    const [_, feeDeclaredInContract] = await ssvViews.getOperatorDeclaredFee(1);
    expect(feeDeclaredInContract).to.equal(newFee);
  });

  it('DAO get declared and execute fee periods', async () => {
    expect(await ssvViews.getOperatorFeePeriods()).to.deep.equal([helpers.CONFIG.declareOperatorFeePeriod, helpers.CONFIG.executeOperatorFeePeriod]);
  });

  it('Increase fee from an address thats not the DAO reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).updateOperatorFeeIncreaseLimit(1000
    )).to.be.revertedWith('Ownable: caller is not the owner');
  });

  it('Update the declare fee period from an address thats not the DAO reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).updateDeclareOperatorFeePeriod(1200
    )).to.be.revertedWith('Ownable: caller is not the owner');
  });

  it('Update the execute fee period from an address thats not the DAO reverts "caller is not the owner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).updateExecuteOperatorFeePeriod(1200))
      .to.be.revertedWith('Ownable: caller is not the owner');
  });

  it('DAO declared fee without a pending request reverts "NoFeeDeclared"', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[2]).declareOperatorFee(1, initialFee + initialFee / 10), [GasGroup.REGISTER_OPERATOR]);
    const [isFeeDeclared] = await ssvViews.getOperatorDeclaredFee(2);
    expect(isFeeDeclared).to.equal(false);
  });
});