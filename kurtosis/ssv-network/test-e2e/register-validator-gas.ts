// Declare imports
import * as helpers from '../test/helpers/contract-helpers';

// Declare globals
let ssvNetworkContract: any, minDepositAmount: any;
type gasStruct = {
  Operators: number,
  New_Pod: number,
  Second_Val_With_Deposit: number,
  Third_Val_No_Deposit: number,
}
const gasTable: gasStruct[] = [];
let firstPodEverGas = 0;

describe('Register Validator Gas Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    ssvNetworkContract = (await helpers.initializeContract()).contract;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    // Define a minimum deposit amount
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 2) * helpers.CONFIG.minimalOperatorFee * 13;

    // Register first validator ever
    await helpers.DB.ssvToken.approve(helpers.DB.ssvNetwork.contract.address, minDepositAmount);
    const tx = await ssvNetworkContract.registerValidator(
      helpers.DataGenerator.publicKey(5),
      [1, 2, 3, 4],
      helpers.DataGenerator.shares(4),
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    );

    // Save gas fee for first validator ever
    const receipt = await tx.wait();
    firstPodEverGas = +receipt.gasUsed;

    // Approve SSV
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount * 500);
  });

  it('4 Operators Gas Usage', async () => {
    // New Pod
    const tx1 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(1),
      [1, 2, 3, 4],
      helpers.DataGenerator.shares(4),
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    );
    const receipt1 = await tx1.wait();
    const cluster1 = (receipt1.events[2].args[4]);

    // Second Validator with a deposit
    const tx2 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(2),
      [1, 2, 3, 4],
      helpers.DataGenerator.shares(4),
      minDepositAmount,
      cluster1
    );
    const receipt2 = await tx2.wait();
    const cluster2 = (receipt2.events[2].args[4]);

    // Third Validator without a deposit
    const tx3 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(3),
      [1, 2, 3, 4],
      helpers.DataGenerator.shares(4),
      0,
      cluster2
    );
    const receipt3 = await tx3.wait();

    // Save the gas costs
    gasTable.push({ Operators: 4, New_Pod: +receipt1.gasUsed, Second_Val_With_Deposit: +receipt2.gasUsed, Third_Val_No_Deposit: +receipt3.gasUsed });
  });

  it('7 Operators Gas Usage', async () => {
    // New Pod
    const tx1 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(1),
      [1, 2, 3, 4, 5, 6, 7],
      helpers.DataGenerator.shares(7),
      minDepositAmount * 2,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    );
    const receipt1 = await tx1.wait();
    const cluster1 = (receipt1.events[2].args[4]);

    // Second Validator with a deposit
    const tx2 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(2),
      [1, 2, 3, 4, 5, 6, 7],
      helpers.DataGenerator.shares(7),
      minDepositAmount * 2,
      cluster1
    );
    const receipt2 = await tx2.wait();
    const cluster2 = (receipt2.events[2].args[4]);

    // Third Validator without a deposit
    const tx3 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(3),
      [1, 2, 3, 4, 5, 6, 7],
      helpers.DataGenerator.shares(7),
      0,
      cluster2
    );
    const receipt3 = await tx3.wait();

    // Save the gas costs
    gasTable.push({ Operators: 7, New_Pod: +receipt1.gasUsed, Second_Val_With_Deposit: +receipt2.gasUsed, Third_Val_No_Deposit: +receipt3.gasUsed });
  });

  it('10 Operators Gas Usage', async () => {
    // New Pod
    const tx1 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(1),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
      helpers.DataGenerator.shares(10),
      minDepositAmount * 3,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    );
    const receipt1 = await tx1.wait();
    const cluster1 = (receipt1.events[2].args[4]);

    // Second Validator with a deposit
    const tx2 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(2),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
      helpers.DataGenerator.shares(10),
      minDepositAmount * 3,
      cluster1
    );
    const receipt2 = await tx2.wait();
    const cluster2 = (receipt2.events[2].args[4]);

    // Third Validator without a deposit
    const tx3 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(3),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
      helpers.DataGenerator.shares(10),
      0,
      cluster2
    );
    const receipt3 = await tx3.wait();

    // Save the gas costs
    gasTable.push({ Operators: 10, New_Pod: +receipt1.gasUsed, Second_Val_With_Deposit: +receipt2.gasUsed, Third_Val_No_Deposit: +receipt3.gasUsed });
  });

  it('13 Operators Gas Usage', async () => {
    // New Pod
    const tx1 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(1),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
      helpers.DataGenerator.shares(13),
      minDepositAmount * 4,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    );
    const receipt1 = await tx1.wait();
    const cluster1 = (receipt1.events[2].args[4]);

    // Second Validator with a deposit
    const tx2 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(2),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
      helpers.DataGenerator.shares(13),
      minDepositAmount * 4,
      cluster1
    );
    const receipt2 = await tx2.wait();
    const cluster2 = (receipt2.events[2].args[4]);

    // Third Validator without a deposit
    const tx3 = await ssvNetworkContract.connect(helpers.DB.owners[1]).registerValidator(
      helpers.DataGenerator.publicKey(3),
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
      helpers.DataGenerator.shares(13),
      0,
      cluster2
    );
    const receipt3 = await tx3.wait();

    // Save the gas costs
    gasTable.push({ Operators: 13, New_Pod: +receipt1.gasUsed, Second_Val_With_Deposit: +receipt2.gasUsed, Third_Val_No_Deposit: +receipt3.gasUsed });

    // Log the table
    console.log(`First validator ever: ${firstPodEverGas}`);
    console.table(gasTable);
  });
});
