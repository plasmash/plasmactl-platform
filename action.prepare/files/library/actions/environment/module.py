#!/usr/bin/env python3

from actions.config.module import Config
from ansible_vault import Vault
from configparser import ConfigParser
from getpass import getpass


class Environment:
    VAULT_PASS_FILE = '/tmp/vault'
    CLUSTER_FILE = '/tmp/cluster'
    VAULT_FILE = 'platform/group_vars/platform/vault.yaml'

    def __init__(self, env='dev'):
        self._env = env
        self._config = Config()
        self._password = None
        self._ssh_key = None

    def set_vault_pass(self, password: str = None):
        if password:
            self._password = password
        else:
            self._password = getpass("- Enter Ansible vault password:")
        with open(self.VAULT_PASS_FILE, 'w') as f:
            f.write(self._password)

    def set_sshkey_pass(self, password: str = None):
        if password:
            self._password = password
        else:
            self._password = getpass("- Enter Ansible SSH key and Vault passphrase: ")
        with open(self.VAULT_PASS_FILE, 'w') as f:
            f.write(self._password)

    def get_vault_pass(self):
        if not self._password:
            self.set_vault_pass()
        return self._password

    def set_cluster(self):
        cluster = self._config.get_value("environment", self._env, "name")
        with open(self.CLUSTER_FILE, 'w') as f:
            f.write(cluster)

    def get_ssh_key(self):
        if not self._ssh_key:
            vault = self._decrypt_vault()
            self._ssh_key = vault.get('vault_user_ssh_private_key')
        return self._ssh_key

    def get_env_vars(self):
        env_vars = {'ANSIBLE_VAULT_PASSWORD_FILE': self.VAULT_PASS_FILE}

        config = ConfigParser()
        config.read('ansible.cfg')
        # @see: https://github.com/ansible/ansible-runner/issues/601
        env_vars['ANSIBLE_CALLBACK_PLUGINS'] = config.get('defaults', 'callback_plugins')
        # @see: https://github.com/ansible/ansible-runner/issues/1122
        env_vars['ANSIBLE_STDOUT_CALLBACK'] = config.get('defaults', 'stdout_callback')

        conf_env_vars = self._config.get_value('environment', self._env, 'environment_variables')
        for key, val in conf_env_vars.items():
            env_vars[key.upper()] = val

        return env_vars

    def get_extra_vars(self):
        extra_vars = self._config.get_value('environment', self._env, 'extra_vars')
        extra_vars_string = ""
        for key, val in extra_vars.items():
            extra_vars_string += f"{key}={val} "
        return extra_vars_string

    def _decrypt_vault(self):
        vault = Vault(self.get_vault_pass())
        with open(self.VAULT_FILE) as f:
            return vault.load(f.read())

    def get_ip(self):
        return self._config.get_value('environment', self._env, 'ip')

    def get_vault_variable(self, variable_name: str = None):
        vault = self._decrypt_vault()
        return vault.get(variable_name)

