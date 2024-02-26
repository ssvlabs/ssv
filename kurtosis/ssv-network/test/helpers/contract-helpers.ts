// Imports
declare const ethers: any;
import { SSVKeys, KeyShares, EncryptShare } from 'ssv-keys';
import { trackGas, GasGroup } from './gas-usage';
import { Validator, Operator } from './types';
import validatorKeys from './json/validatorKeys.json';
import operatorKeys from './json/operatorKeys.json';

export let DB: any;
export let CONFIG: any;
export let SSV_MODULES: any;

export const DEFAULT_OPERATOR_IDS = {
  4: [1, 2, 3, 4],
  7: [1, 2, 3, 4, 5, 6, 7],
  10: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
  13: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
};

const getSecretSharedPayload = async (validator: Validator, operatorIds: number[], ownerId: number) => {
  const selOperators = DB.operators.filter((item: Operator) => operatorIds.includes(item.id));
  const operators = selOperators.map((item: Operator) => ({ id: item.id, operatorKey: item.operatorKey }));

  const ssvKeys = new SSVKeys();
  const keyShares = new KeyShares();

  const publicKey = validator.publicKey;
  const privateKey = validator.privateKey;

  const threshold = await ssvKeys.createThreshold(privateKey, operators);
  const encryptedShares: EncryptShare[] = await ssvKeys.encryptShares(operators, threshold.shares);
  const payload = await keyShares.buildPayload(
    {
      publicKey,
      operators,
      encryptedShares,
    },
    {
      ownerAddress: DB.owners[ownerId].address,
      ownerNonce: DB.ownerNonce,
      privateKey,
    },
  );
  return payload;
};

export const DataGenerator = {
  publicKey: (id: number) => {
    const validators = DB.validators.filter((item: Validator) => item.id === id);
    if (validators.length > 0) {
      return validators[0].publicKey;
    }
    return `0x${id.toString(16).padStart(48, '0')}`;
  },
  shares: async (ownerId: number, validatorId: number, operatorCount: number) => {
    let shared: any;
    const validators = DB.validators.filter((item: Operator) => item.id === validatorId);
    if (validators.length > 0) {
      const validator = validators[0];
      const operatorIds: number[] = [];
      for (let i = 1; i <= operatorCount; i++) {
        operatorIds.push(i);
      }
      const payload = await getSecretSharedPayload(validator, operatorIds, ownerId);
      shared = payload.sharesData;
    } else {
      shared = `0x${validatorId.toString(16).padStart(48, '0')}`;
    }
    return shared;
  },
  cluster: {
    new: (size = 4) => {
      const usedOperatorIds: any = {};
      for (const clusterId in DB.clusters) {
        for (const operatorId of DB.clusters[clusterId].operatorIds) {
          usedOperatorIds[operatorId] = true;
        }
      }

      const result = [];
      for (const operator of DB.operators) {
        if (operator && !usedOperatorIds[operator.id]) {
          result.push(operator.id);
          usedOperatorIds[operator.id] = true;

          if (result.length == size) {
            break;
          }
        }
      }
      if (result.length < size) {
        throw new Error('No new clusters. Try to register more operators.');
      }
      return result;
    },
    byId: (id: any) => DB.clusters[id].operatorIds,
  },
};

export const getClusterForValidator = (
  validatorCount: number,
  networkFeeIndex: number,
  index: number,
  balance: number,
  active: boolean,
) => {
  return { validatorCount, networkFeeIndex, index, balance, active };
};

export const initializeContract = async () => {
  CONFIG = {
    initialVersion: 'v1.1.0',
    operatorMaxFeeIncrease: 1000,
    declareOperatorFeePeriod: 3600, // HOUR
    executeOperatorFeePeriod: 86400, // DAY
    minimalOperatorFee: 100000000,
    minimalBlocksBeforeLiquidation: 100800,
    minimumLiquidationCollateral: 200000000,
    validatorsPerOperatorLimit: 500,
    maximumOperatorFee: 76528650000000,
  };

  DB = {
    owners: [],
    validators: [],
    operators: [],
    registeredValidators: [],
    registeredOperators: [],
    clusters: [],
    ssvNetwork: {},
    ssvViews: {},
    ssvToken: {},
    ssvOperatorsMod: {},
    ssvClustersMod: {},
    ssvDAOMod: {},
    ssvViewsMod: {},
    ownerNonce: 0,
  };

  SSV_MODULES = {
    SSV_OPERATORS: 0,
    SSV_CLUSTERS: 1,
    SSV_DAO: 2,
    SSV_VIEWS: 3,
  };

  // Define accounts
  DB.owners = await ethers.getSigners();

  // Load validators
  DB.validators = validatorKeys as Validator[];

  // Load operators
  DB.operators = operatorKeys as Operator[];

  // Initialize contract
  const ssvNetwork = await ethers.getContractFactory('SSVNetwork');
  const ssvViews = await ethers.getContractFactory('SSVNetworkViews');

  const ssvViewsMod = await ethers.getContractFactory('contracts/modules/SSVViews.sol:SSVViews');
  const ssvToken = await ethers.getContractFactory('SSVToken');
  const ssvOperatorsMod = await ethers.getContractFactory('SSVOperators');
  const ssvClustersMod = await ethers.getContractFactory('SSVClusters');
  const ssvDAOMod = await ethers.getContractFactory('SSVDAO');

  DB.ssvToken = await ssvToken.deploy();
  await DB.ssvToken.deployed();

  DB.ssvViewsMod.contract = await ssvViewsMod.deploy();
  await DB.ssvViewsMod.contract.deployed();

  DB.ssvOperatorsMod.contract = await ssvOperatorsMod.deploy();
  await DB.ssvOperatorsMod.contract.deployed();

  DB.ssvClustersMod.contract = await ssvClustersMod.deploy();
  await DB.ssvClustersMod.contract.deployed();

  DB.ssvDAOMod.contract = await ssvDAOMod.deploy();
  await DB.ssvDAOMod.contract.deployed();

  DB.ssvNetwork.contract = await upgrades.deployProxy(
    ssvNetwork,
    [
      DB.ssvToken.address,
      DB.ssvOperatorsMod.contract.address,
      DB.ssvClustersMod.contract.address,
      DB.ssvDAOMod.contract.address,
      DB.ssvViewsMod.contract.address,
      CONFIG.minimalBlocksBeforeLiquidation,
      CONFIG.minimumLiquidationCollateral,
      CONFIG.validatorsPerOperatorLimit,
      CONFIG.declareOperatorFeePeriod,
      CONFIG.executeOperatorFeePeriod,
      CONFIG.operatorMaxFeeIncrease,
    ],
    {
      kind: 'uups',
      unsafeAllow: ['delegatecall'],
    },
  );

  await DB.ssvNetwork.contract.deployed();

  DB.ssvViews.contract = await upgrades.deployProxy(ssvViews, [DB.ssvNetwork.contract.address], {
    kind: 'uups',
  });
  await DB.ssvViews.contract.deployed();

  DB.ssvNetwork.owner = DB.owners[0];

  await DB.ssvToken.mint(DB.owners[1].address, '10000000000000000000');
  await DB.ssvToken.mint(DB.owners[2].address, '10000000000000000000');
  await DB.ssvToken.mint(DB.owners[3].address, '10000000000000000000');
  await DB.ssvToken.mint(DB.owners[4].address, '10000000000000000000');
  await DB.ssvToken.mint(DB.owners[5].address, '10000000000000000000');
  await DB.ssvToken.mint(DB.owners[6].address, '10000000000000000000');

  await DB.ssvNetwork.contract.updateMaximumOperatorFee(CONFIG.maximumOperatorFee);

  return {
    contract: DB.ssvNetwork.contract,
    owner: DB.ssvNetwork.owner,
    ssvToken: DB.ssvToken,
    ssvViews: DB.ssvViews.contract,
  };
};

export const deposit = async (
  ownerId: number,
  ownerAddress: string,
  operatorIds: number[],
  amount: string,
  cluster: any,
) => {
  await DB.ssvToken.connect(DB.owners[ownerId]).approve(DB.ssvNetwork.contract.address, amount);
  const depositedCluster = await trackGas(
    DB.ssvNetwork.contract.connect(DB.owners[ownerId]).deposit(ownerAddress, operatorIds, amount, cluster),
  );
  return depositedCluster.eventsByName.ClusterDeposited[0].args;
};

export const liquidate = async (ownerAddress: string, operatorIds: number[], cluster: any) => {
  const liquidatedCluster = await trackGas(DB.ssvNetwork.contract.liquidate(ownerAddress, operatorIds, cluster));
  return liquidatedCluster.eventsByName.ClusterLiquidated[0].args;
};

export const withdraw = async (ownerId: number, operatorIds: number[], amount: string, cluster: any) => {
  const withdrawnCluster = await trackGas(
    DB.ssvNetwork.contract.connect(DB.owners[ownerId]).withdraw(operatorIds, amount, cluster),
  );

  return withdrawnCluster.eventsByName.ClusterWithdrawn[0].args;
};

export const reactivate = async (ownerId: number, operatorIds: number[], amount: string, cluster: any) => {
  await DB.ssvToken.connect(DB.owners[ownerId]).approve(DB.ssvNetwork.contract.address, amount);
  const reactivatedCluster = await trackGas(
    DB.ssvNetwork.contract.connect(DB.owners[ownerId]).reactivate(operatorIds, amount, cluster),
  );
  return reactivatedCluster.eventsByName.ClusterReactivated[0].args;
};

export const registerOperators = async (
  ownerId: number,
  numberOfOperators: number,
  fee: string,
  gasGroups: GasGroup[] = [GasGroup.REGISTER_OPERATOR],
) => {
  for (let i = 0; i < numberOfOperators && i < DB.operators.length; i++) {
    const operator = DB.operators[i];
    operator.publicKey = ethers.utils.keccak256(ethers.utils.toUtf8Bytes(operator.operatorKey));
    const { eventsByName } = await trackGas(
      DB.ssvNetwork.contract.connect(DB.owners[ownerId]).registerOperator(operator.publicKey, fee),
      gasGroups,
    );
    const event = eventsByName.OperatorAdded[0];
    operator.id = event.args[0].toNumber();
    DB.operators[i] = operator;
    DB.registeredOperators.push({ id: operator.id, ownerId: ownerId });
  }
};

export const registerValidators = async (
  ownerId: number,
  amount: string,
  validatorIds: number[],
  operatorIds: number[],
  cluster: any,
  gasGroups?: GasGroup[],
) => {
  const regValidators: any = [];
  let args: any;

  const selValidators = DB.validators.filter((item: Validator) => validatorIds.includes(item.id));
  for (const validator of selValidators) {
    const payload = await getSecretSharedPayload(validator, operatorIds, ownerId);

    await DB.ssvToken.connect(DB.owners[ownerId]).approve(DB.ssvNetwork.contract.address, amount);
    const result = await trackGas(
      DB.ssvNetwork.contract
        .connect(DB.owners[ownerId])
        .registerValidator(payload.publicKey, payload.operatorIds, payload.sharesData, amount, cluster),
      gasGroups,
    );
    DB.ownerNonce++;
    DB.registeredValidators.push({ id: validator.id, payload });
    regValidators.push({ publicKey: payload.publicKey, shares: payload.sharesData });
    args = result.eventsByName.ValidatorAdded[0].args;
  }

  return { regValidators, args };
};

export const bulkRegisterValidators = async (
  ownerId: number,
  numberOfValidators: number,
  operatorIds: number[],
  minDepositAmount: any,
  cluster: any,
  gasGroups?: GasGroup[],
) => {
  const pks = Array.from({ length: numberOfValidators }, (_, index) => DataGenerator.publicKey(index + 1));
  const shares = Array.from({ length: numberOfValidators }, (_, index) =>
    DataGenerator.shares(1, index, operatorIds.length),
  );
  const depositAmount = minDepositAmount * numberOfValidators;

  await DB.ssvToken.connect(DB.owners[ownerId]).approve(DB.ssvNetwork.contract.address, depositAmount);

  const result = await trackGas(
    DB.ssvNetwork.contract
      .connect(DB.owners[ownerId])
      .bulkRegisterValidator(pks, operatorIds, shares, depositAmount, cluster),
    gasGroups,
  );

  return {
    args: result.eventsByName.ValidatorAdded[0].args,
    pks,
  };
};

export const coldRegisterValidator = async () => {
  const ssvKeys = new SSVKeys();
  const keyShares = new KeyShares();

  const validator = DB.validators[0];
  const operators = DB.operators.slice(0, 4).map((item: Operator) => ({ id: item.id, operatorKey: item.operatorKey }));
  const publicKey = validator.publicKey;
  const privateKey = validator.privateKey;
  const threshold = await ssvKeys.createThreshold(privateKey, operators);
  const encryptedShares: EncryptShare[] = await ssvKeys.encryptShares(operators, threshold.shares);
  const payload = await keyShares.buildPayload(
    {
      publicKey,
      operators,
      encryptedShares,
    },
    {
      ownerAddress: DB.owners[0].address,
      ownerNonce: 1,
      privateKey,
    },
  );

  const amount = '1000000000000000';
  await DB.ssvToken.approve(DB.ssvNetwork.contract.address, amount);
  await DB.ssvNetwork.contract
    .connect(DB.owners[0])
    .registerValidator(payload.publicKey, payload.operatorIds, payload.sharesData, amount, {
      validatorCount: 0,
      networkFeeIndex: 0,
      index: 0,
      balance: 0,
      active: true,
    });
};

export const removeValidator = async (ownerId: number, pk: string, operatorIds: number[], cluster: any) => {
  const removedValidator = await trackGas(
    DB.ssvNetwork.contract.connect(DB.owners[ownerId]).removeValidator(pk, operatorIds, cluster),
  );
  return removedValidator.eventsByName.ValidatorRemoved[0].args;
};

export const getCluster = (payload: any) =>
  ethers.utils.AbiCoder.prototype.encode(
    [
      'tuple(uint32 validatorCount, uint64 networkFee, uint64 networkFeeIndex, uint64 index, uint64 balance, bool active) cluster',
    ],
    [payload],
  );
