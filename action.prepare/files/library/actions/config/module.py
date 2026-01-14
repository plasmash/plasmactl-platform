#!/usr/bin/env python3

import yaml


class Config:
    def __init__(self, file_path='toolbox/config.yaml'):
        self._file = file_path
        self._config = self._load_config()

    def _load_config(self):
        try:
            with open(self._file, "r") as file:
                return yaml.safe_load(file)
        except FileNotFoundError:
            print(f"Error: config file {self._file} not found")
            return {}

    def get_value(self, kind, name, key):
        if kind not in self._config:
            print(f'Error: no "{kind}" found in config')
            return None

        if name not in self._config[kind]:
            print(f'Error: no "{name}" found in "{kind}" config')
            return None

        val = self._config.get(kind).get(name)
        for component in key.split('.'):
            val = val.get(component)
        if val is None:
            print(f'Error: no "{key}" found in "{kind}.{name}" config')
        return val
