from tempfile import mkstemp
from shutil import move, copymode
from os import fdopen, remove
import subprocess
import yaml


def save_yaml(file_path, data):
    with open(file_path, 'w') as outfile:
        yaml.dump(data, outfile, default_flow_style=False)


def replace_in_file(file_path, patterns):
    fh, abs_path = mkstemp()
    with fdopen(fh, 'w') as new_file:
        with open(file_path) as old_file:
            for line in old_file:
                newline = line
                for pattern in patterns:
                    newline = newline.replace(pattern.re, patterns.sub)
                new_file.write(newline)
    copymode(file_path, abs_path)
    remove(file_path)
    move(abs_path, file_path)


def exec_cmd(cmd, env):
    cli = subprocess.run(cmd, capture_output=True, env=env)
    return cli.stdout


# def apply_template()