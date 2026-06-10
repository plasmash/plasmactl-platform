#!/usr/bin/env python3
"""platform:deploy container entrypoint (v2).

Drives ansible-playbook inside the deploy container spawned by launchr.

- /tmp/cluster holds the platform-directory name read by the platform_nodes
  inventory plugin to locate platforms/<name>/.
- ANSIBLE_VAULT_PASSWORD_FILE points at an askpass script that echoes
  PLASMA_VAULT_PASS, so the vault password never lands on disk in plaintext.
- SSH access to target hosts is configured by the inventory plugin via
  per-host ansible_ssh_private_key_file (e.g. /tmp/ovh-rescue-key for OVH).
  This script does not manage SSH keys.

The v1-vestige Environment-class wrapper (toolbox/config.yaml + v1 vault
layout) is intentionally not used; its responsibilities were either moved
to the action.yaml/keyring plumbing or dropped (extra_vars-from-config is
replaced by the single machine_target_config=<name> extra-var that the
v1 deploy.go also used).
"""
import argparse
import os
import shutil
import stat
import subprocess
import sys
import tarfile


CLUSTER_FILE = '/tmp/cluster'
LOG_FILE = 'deploy.log'
DEPLOY_DIR = '.deploy'


class BuildAction:
    def __init__(self, name: str, tags: str, image: str = "", debug: bool = False,
                 check: bool = False, password: str = "", logs: bool = False):
        self._name = name
        self._tags = tags
        self._debug = debug
        self._check = check
        self._password = password
        self._log_in_file = logs
        self._image = image
        self._original_dir = os.getcwd()

        if image:
            self._extract_image(image)

        with open(CLUSTER_FILE, 'w') as f:
            f.write(self._name)
        self._setup_otel_env()

    def _extract_image(self, image_path: str):
        if not os.path.isabs(image_path):
            image_path = os.path.join(self._original_dir, image_path)
        if not os.path.isfile(image_path):
            print(f"Error: Platform Image not found: {image_path}")
            sys.exit(1)
        if os.path.exists(DEPLOY_DIR):
            shutil.rmtree(DEPLOY_DIR)
        os.makedirs(DEPLOY_DIR)
        print(f"Extracting Platform Image: {image_path}")
        try:
            with tarfile.open(image_path, 'r:gz') as tar:
                tar.extractall(path=DEPLOY_DIR)
            print(f"Platform Image extracted to {DEPLOY_DIR}/")
            os.chdir(DEPLOY_DIR)
        except Exception as e:
            print(f"Error extracting Platform Image: {e}")
            sys.exit(1)

    def _setup_otel_env(self):
        if not os.environ.get('OTEL_EXPORTER_OTLP_ENDPOINT'):
            return
        attrs = os.environ.get('OTEL_RESOURCE_ATTRIBUTES', '')
        attrs_dict = {}
        if attrs:
            for attr in attrs.split(','):
                if '=' in attr:
                    k, v = attr.split('=', 1)
                    attrs_dict[k] = v
        attrs_dict['env'] = self._name
        os.environ['OTEL_RESOURCE_ATTRIBUTES'] = ','.join(
            f"{k}={v}" for k, v in attrs_dict.items()
        )

    def exec(self):
        try:
            sys.exit(self._run_deploy())
        finally:
            self._cleanup()

    def _run_deploy(self) -> int:
        info = f"Building {self._tags}..."
        print(info)

        if not self._nodes_dir_has_yaml():
            print("No node yaml files in platforms/<cluster>/nodes/; quitting.")
            return 0

        askpass = self._create_askpass_script()
        try:
            cmd = ['ansible-playbook',
                   'platform/platform.yaml',
                   '--tags', self._tags,
                   '--extra-vars', f'machine_target_config={self._name}']
            if self._debug:
                cmd.append('-vvv')
            if self._check:
                cmd.append('--check')

            env = os.environ.copy()
            env['ANSIBLE_VAULT_PASSWORD_FILE'] = askpass
            env['SSH_ASKPASS'] = askpass
            env['SSH_ASKPASS_REQUIRE'] = 'force'
            env['PLASMA_VAULT_PASS'] = self._password

            print(f"Running: {' '.join(cmd)}")
            return self._run_with_optional_log(cmd, env)
        finally:
            try:
                os.remove(askpass)
            except OSError:
                pass

    def _run_with_optional_log(self, cmd, env) -> int:
        if not self._log_in_file:
            return subprocess.run(cmd, env=env).returncode
        proc = subprocess.Popen(cmd, env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True, bufsize=1)
        with open(LOG_FILE, 'a') as logf:
            for line in proc.stdout or ():
                sys.stdout.write(line)
                logf.write(line)
        return proc.wait()

    def _create_askpass_script(self) -> str:
        path = f'/tmp/plasmactl-askpass-{os.getpid()}.sh'
        with open(path, 'w') as f:
            f.write('#!/bin/sh\necho "$PLASMA_VAULT_PASS"\n')
        st = os.stat(path)
        os.chmod(path, st.st_mode | stat.S_IXUSR)
        return path

    def _cleanup(self):
        if self._image:
            os.chdir(self._original_dir)
            if os.path.exists(DEPLOY_DIR):
                print(f"Cleaning up {DEPLOY_DIR}/")
                shutil.rmtree(DEPLOY_DIR)

    @staticmethod
    def _nodes_dir_has_yaml() -> bool:
        try:
            with open(CLUSTER_FILE) as f:
                cluster = f.read().strip()
        except FileNotFoundError:
            print(f"{CLUSTER_FILE} does not exist; deploy precondition failed.")
            return False
        nodes_dir = f"platforms/{cluster}/nodes"
        if not os.path.isdir(nodes_dir):
            return False
        return any(name.endswith(".yaml") for name in os.listdir(nodes_dir))


def str2bool(v):
    return v.lower() in ("true", "1")


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Platform builder.')
    parser.add_argument('name', help='The platform to deploy to')
    parser.add_argument('tags', help='The Ansible resources to deploy')
    parser.add_argument("--image", nargs="?", default="", help="Deploy from a Platform Image (.pi) file")
    parser.add_argument("--debug", "-d", type=str2bool, default=False)
    parser.add_argument("--check", "-c", type=str2bool, default=False)
    parser.add_argument("--password", "-pwd", nargs="?", default="")
    parser.add_argument("--logs", type=str2bool, default=False)
    BuildAction(**vars(parser.parse_args())).exec()
