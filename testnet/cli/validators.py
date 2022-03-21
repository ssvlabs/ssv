import glob
import json
import yaml
from .utils import exec_cmd
from os import open


def prepare_payload(ks_file_path, pswd, ops):
    res = exec_cmd(
        ["ssv-cli", f"--filePath=\"{ks_file_path}\"", f"--password=\"{pswd}\"", f"--operators=\"{ops}\""],
    )
    return json.load(res)


def ops_generator(ops):
    x = 0
    while True:
        current_ops = []
        for i in range(x):
            current_ops.append(ops[i % len(ops)])
        yield current_ops
        x += 1


# TODO: complete
def generate_validators(base_path, lh_data_dir='.lighthouse/local-testnet'):
    with open(f"{base_path}/operators.yaml", 'r') as stream:
        all_ops = yaml.safe_load(stream)
    ops_gen = ops_generator(all_ops)
    for node_i in glob.glob(f"{lh_data_dir}/node_*"):
        validators_json = []
        for vks in glob.glob(f"{node_i}/validators/**/*.json"):
            # pk = ""
            # pswd = ""
            with open(vks) as f:
                key_store = json.load(f)
                pk = key_store['pubkey']
            with open(f"{node_i}/secrets/0x{pk}") as f:
                pswd = f.read()
            txn = prepare_payload(vks, pswd, next(ops_gen))
            validator_json = {
                "pk": pk,
                "address": txn[0],
                "operators": txn[1],
                "sharePublicKeys": txn[2],
                "encryptedKeys": txn[3]
            }
            validators_json.append(validator_json)
        with open(f"{node_i}/validators.json", 'w') as outfile:
            json.dump(validators_json, outfile)
            # TODO: set relevant env and call contract
        # cli = subprocess.run([])
        print(f"created {len(validators_json)} validators")
