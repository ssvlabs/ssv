import json
import os
import re
import docker
from .utils import save_yaml

reg_json = '(\{.+\})'


def _extract_key(line, k):
    j_str = re.search(reg_json, line)
    j = json.loads(j_str.group(1))
    return j[k]


def create_operator_keys():
    reg_priv_key = 'generated private key \(base64\)'
    reg_pub_key = 'generated public key \(base64\)'
    client = docker.from_env()
    log = client.containers.run('bloxstaking/ssv-node:latest', command='/go/bin/ssvnode generate-operator-keys',
                                auto_remove=True)
    lines = log.decode().split("\n")
    sk = ""
    pk = ""
    for line in lines:
        if re.search(reg_priv_key, line) is not None:
            sk = _extract_key(line, 'sk')
        elif re.search(reg_pub_key, line) is not None:
            pk = _extract_key(line, 'pk')
    return pk, sk


def create_main_config(lh_ip, reg_contract_addr, reg_contract_genesis):
    cfg = {
        "eth2": {
            "BeaconNodeAddr": f"http://{lh_ip}:8001"
        },
        "eth1": {
            "Eth1Addr": f"http://{lh_ip}:8545",
            "RegistryContractAddr": f"{reg_contract_addr}",
            "ETH1SyncOffset": reg_contract_genesis,
        }
    }
    return cfg


def create_operator_config(i, sk):
    cfg = {
        "db": {
            "Path": f"./data/db-{i}"
        },
        "MetricsAPIPort": f"15{i:03}",
        "OperatorPrivateKey": f"{sk}"
    }
    return cfg


def generate_operators(n, testnet_dir, cfg_path='data/config', lh_ip='127.0.0.1', reg_contract_addr='0x0',
                       reg_contract_genesis='0'):
    cfg_full_path = os.path.join(testnet_dir, cfg_path)
    os.makedirs(cfg_full_path)
    main_cfg = create_main_config(lh_ip, reg_contract_addr, reg_contract_genesis)
    save_yaml(f"{cfg_full_path}/config.yaml", main_cfg)
    ops = []
    for i in range(n):
        pk, sk = create_operator_keys()
        node_id = i + 1
        op = {
            "id": node_id,
            "pk": pk,
            "name": f"node-{str(node_id)}",
            "address": "0x0",
            "fee": 0
        }
        ops.append(op)
        cfg = create_operator_config(node_id, sk)
        save_yaml(f"{testnet_dir}/{cfg_path}/share{node_id}.yaml", cfg)
    save_yaml(f"{testnet_dir}/operators.yaml", ops)

# generate_operators(4, os.environ.get('SSV_TESTNET_DIR'))
