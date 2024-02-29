// Define imports
const hre = require("hardhat");
const { ethers } = hre;

let registeredOperators = 0

// Build provider on the Goerli network
const provider = ethers.getDefaultProvider(process.env.RPC_URI)

// Build wallets from the private keys
const account = new ethers.Wallet(`0x${process.env.OWNER_PRIVATE_KEY}`, provider)

const operatorPublicKeys = getOperatorPublicKeys()

// Start script
async function registerOperators() {
    // Attach SSV Network
    const ssvNetworkFactory = await ethers.getContractFactory('SSVNetwork')
    const ssvNetwork = ssvNetworkFactory.attach(process.env.SSV_NETWORK_ADDRESS_STAGE)
    console.log('Successfully Attached to the SSV Network Contract')

    for (let i = 0; i < operatorPublicKeys.length; i++) {
        const abiCoder = new ethers.utils.AbiCoder()
        const abiEncoded = abiCoder.encode(["string"], [operatorPublicKeys[i]])

        console.log('----------------------- register-operator -----------------------');
        console.log('public-key', operatorPublicKeys[i]);
        console.log('abi-encoded', abiEncoded);
        console.log(`Registering operator ${registeredOperators + 1} out of ${operatorPublicKeys.length}`)
        console.log('------------------------------------------------------------------');

        // Connect the account to use for contract interaction
        // const ssvNetworkContract = await ssvNetwork.connect(accounts[0])
        const ssvNetworkContract = await ssvNetwork.connect(account)
        // Register the validator
        const txResponse = await ssvNetworkContract.registerOperator(
            abiEncoded,
            '100000000',
            {
                gasPrice: process.env.GAS_PRICE,
                gasLimit: process.env.GAS_LIMIT
            }
        );
        console.log('registered operator, tx', txResponse.hash);

        await txResponse.wait();

        registeredOperators++
    }

    console.log(`successfully registered ${registeredOperators} operators`)
}

function getOperatorPublicKeys(): string[] {
  const nodeCount = parseInt(process.env.SSV_NODE_COUNT || '0', 10);

  const publicKeys: string[] = [];

  for (let index = 0; index < nodeCount; index++) {
    const envVarName = `OPERATOR_${index}_PUBLIC_KEY`;
    const publicKey = process.env[envVarName];

    if (publicKey) {
      publicKeys.push(publicKey);
    }
  }

  return publicKeys;
}

registerOperators()
    .then(() => process.exit(0))
    .catch(error => {
        console.error(error);
        process.exit(1);
    });
