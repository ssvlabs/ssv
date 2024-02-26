declare let network: any;
declare let ethers: any;

export const strToHex = (str: any) => `0x${Buffer.from(str, 'utf8').toString('hex')}`;

export const asciiToHex = (str: any) => {
  const arr1 = [];
  for (let n = 0, l = str.length; n < l; n++) {
    const hex = Number(str.charCodeAt(n)).toString(16);
    arr1.push(hex);
  }
  return arr1.join('');
};

export const blockNumber = async function () {
  return await ethers.provider.getBlockNumber();
};

export const progress = async function (time: any, blocks: any, func: any = null) {
  let snapshot;

  if (func) {
    snapshot = await network.provider.send('evm_snapshot');
  }

  if (time) {
    await network.provider.send('evm_increaseTime', [time]);
  }
  await network.provider.send('hardhat_mine', ['0x' + blocks.toString(16)]);

  if (func) {
    await func();
    await network.provider.send('evm_revert', [snapshot]);
  }
};

export const progressTime = async function (time: any, func: any = null) {
  return progress(time, 1, func);
};

export const progressBlocks = async function (blocks: any, func = null) {
  return progress(0, blocks, func);
};

export const snapshot = async function (func: any) {
  return progress(0, 0, func);
};

export const mineOneBlock = async () => network.provider.send('evm_mine', []);

export const mineChunk = async (amount: number) =>
  Promise.all(
    Array.from({ length: amount }, () => mineOneBlock())
  ) as unknown as Promise<void>;

export const mine = async (amount: number) => {
  if (amount < 0) throw new Error('mine cannot be called with a negative value');
  const MAX_PARALLEL_CALLS = 1000;
  // Do it on parallel but do not overflow connections
  for (let i = 0; i < Math.floor(amount / MAX_PARALLEL_CALLS); i++) {
    await mineChunk(MAX_PARALLEL_CALLS);
  }
  return mineChunk(amount % MAX_PARALLEL_CALLS);
};
