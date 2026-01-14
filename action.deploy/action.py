#!/usr/bin/env python3
import os
import stat
import sys
import yaml

import ansible_runner
import argparse
from actions.environment.module import Environment


class BuildAction:
    LOG_FILE = "./deploy.log"
    ASKPASS_KEY = 'askpass.sh'
    HOSTS_FILE = "ansible-online_net.cache"

    def __init__(self, environment: str, tags: str, debug: bool = False, check: bool = False, password: str = None,
                 logs: bool = False):
        self._tags = tags
        self._debug = debug
        self._check = check
        self._log_in_file = logs
        self._target_config = environment
        self._init_env(environment, password)
        self._setup_otel_env(environment)

    def _init_env(self, env: str, password: str = None):
        print(f"Configuring {env} environment...")
        self._env = Environment(env)

        pwd = password if password else None
        self._env.set_sshkey_pass(pwd)
        self._env.set_cluster()

    def _setup_otel_env(self, environment: str):
        if not os.environ.get('OTEL_EXPORTER_OTLP_ENDPOINT'):
            return

        existing_attrs = os.environ.get('OTEL_RESOURCE_ATTRIBUTES', '')
        attrs_dict = {}
        if existing_attrs:
            for attr in existing_attrs.split(','):
                if '=' in attr:
                    key, value = attr.split('=', 1)
                    attrs_dict[key] = value

        # Override env key with the actual deployment target environment
        # This ensures telemetry is tagged with the correct environment being deployed to,
        # rather than any generic env value that might be set in the host environment
        attrs_dict['env'] = environment
        new_attrs = ','.join(f"{key}={value}" for key, value in attrs_dict.items())
        os.environ['OTEL_RESOURCE_ATTRIBUTES'] = new_attrs

    @staticmethod
    def create_askpass_script(path: str, password: str):
        with open(path, 'w') as asp:
            asp.write('#!/bin/sh\n')
            asp.write(f'echo "{password}"\n')

        st = os.stat(path)
        os.chmod(path, st.st_mode | stat.S_IEXEC)

    def exec(self):
        info = "Building " + self._tags + "..."
        print(info)

        if not self.cache_exists():
            print(f"{self.HOSTS_FILE} does not exist,"
                  f" quiting.")
            return 0

        cmdline_args = ['platform/platform.yaml', '--tags', self._tags, '--extra-vars', self._env.get_extra_vars()]
        if self._debug:
            cmdline_args.append('-vvv')
        if self._check:
            cmdline_args.append('--check')

        ssh_key = None
        try:
            ssh_key = self._env.get_ssh_key()
        except Exception as e:
            print('\nError: Incorrect passphrase\n')
            sys.exit(1)

        r = ansible_runner.interface.init_command_config(
            ident="build_action",
            envvars=self._env.get_env_vars(),
            executable_cmd='ansible-playbook',
            cmdline_args=cmdline_args,
            ssh_key=ssh_key,
        )

        # https://github.com/ansible/ansible-runner/issues/993
        # https://projects.skilld.cloud/skilld/pla-plasmactl/-/issues/35
        # Instead of using ansible_runner.run() directly, it was changed to running command config with updated command.
        # Runner has bug related to `ssh_key` option. It tries to run ssh-agent + ssh-add to set identity.
        # Sometimes it fails and runner forever stuck. Stuck because FIFO file waiting to be read, but
        # ssh-add never read from it. Error often reproduced in pexpect mode, sometimes in subprocess.
        # To solve that issue and to feed passphrase automatically:
        # - runner mode set to subprocess
        # - passphrase is passed via SSH_ASKPASS=script. Script is created during action execution, stored in ansible
        # runner tmp folder
        command = r.config.command
        artifact_dir = r.config.artifact_dir
        askpass_script = f"{artifact_dir}/{self.ASKPASS_KEY}"
        self.create_askpass_script(askpass_script, self._env.get_vault_pass())

        main_cmd = command[3]
        replace_string = "EXIT && ssh-add"

        if replace_string in main_cmd:
            replace_to = f"EXIT && SSH_ASKPASS_REQUIRE=force SSH_ASKPASS='{askpass_script}' ssh-add"
            main_cmd = main_cmd.replace(replace_string, replace_to)
        else:
            print("Failed to set ASK_PASS for command")

        command[3] = main_cmd

        r.run()
        response = r.stdout.read()
        error = r.stderr.read()

        if self._log_in_file:
            with open(self.LOG_FILE, 'a+') as log_file:
                log_file.write(info + '\n')
                log_file.write(response)
                log_file.write(error)

        sys.exit(r.rc)

    def cache_exists(self) -> bool:
        """Check if cache file exists"""
        path = os.path.join(self.get_cache_path(), self.HOSTS_FILE)
        return os.path.isfile(path)

    def get_cache_path(self):
        path = f'library/inventories/platform_nodes/configuration/{self._target_config}.yaml'
        try:
            with open(path) as f:
                configuration = yaml.safe_load(f)
                return configuration['source_inventory']['cache_path']
        except Exception as e:
            print('Error loading configuration')
            sys.exit(1)


def str2bool(v):
    return v.lower() in ("true", "1")


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Platform builder.')
    parser.add_argument('environment', help='The environment to run the bump')
    parser.add_argument('tags', help='The Ansible resources to build')
    parser.add_argument("--debug", "-d", type=str2bool, default=False)
    parser.add_argument("--check", "-c", type=str2bool, default=False)
    parser.add_argument("--password", "-pwd", nargs="?", default="")
    parser.add_argument("--logs", type=str2bool, default=False)
    BuildAction(**vars(parser.parse_args())).exec()
