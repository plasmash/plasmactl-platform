import re

# TODO get configuration (cluster name / online api key /  online uri /
# cache settings / playbook file / groups tree)

from ansible.plugins.inventory import BaseInventoryPlugin, Constructable, Cacheable
import sys
import re
import subprocess
import yaml
import glob
import ipaddress
from string import digits
from string import ascii_lowercase
from itertools import product

DOCUMENTATION = """
    extends_documentation_fragment:
        - inventory_cache
"""

try:
    import json
except ImportError:
    import simplejson as json


class CacheableInventory(dict):
    def add_group(self, group):
        if "groups" not in self:
            self["groups"] = []
        self["groups"].append(group)

    def add_child(self, children, parent):
        if "groups_children" not in self:
            self["groups_children"] = []
        self["groups_children"].append((children, parent))

    def add_host(self, host, group):
        if "hosts" not in self:
            self["hosts"] = []
        self["hosts"].append((host, group))

    def set_variable(self, group, key, value):
        if "variables" not in self:
            self["variables"] = []
        self["variables"].append((group, key, value))


class InventoryModule(BaseInventoryPlugin, Constructable, Cacheable):
    NAME = "platform_nodes"

    def __init__(self):
        super(InventoryModule, self).__init__()

        self.loader = None
        self.inventory = None
        self.cluster = None
        self.playbook_file = None
        self.groups = set()
        self.groups_tree = {}
        self.group_hosts = {}
        self.root_group = "platform"
        self.hosts = {}
        self.host_groups = {}
        self.host_paths = {}

        self.roles_cache = {}

        self.cluster = open("/tmp/cluster").read().strip("\n")
        self.playbook_file = "platform/platform.yaml"
        self.private_network = ipaddress.ip_network("192.168.0.0/13")
        self.private_vip_network = ipaddress.ip_network("192.168.100.0/24")
        self.private_vip = str(self.private_vip_network[1])
        self.etcd_port = "2379"
        self.etcd_uri_template = "https://%s:%s"
        self.default_group = "platform.foundation.core"
        self.configuration = {}
        self.clusters = []

    def load_configuration(self):
        files = glob.glob("library/inventories/platform_nodes/configuration/*.yaml")
        clusters = []
        for f in files:
            with open(f, "r") as stream:
                configuration = yaml.safe_load(stream)
                if configuration["cluster"]:
                    clusters.append(configuration["cluster"])

                if configuration["cluster"] == self.cluster:
                    self.configuration = configuration
        if not self.configuration:
            print("ERROR: Dictionary is empty")
            print(
                "ERROR: No configuration found in 'library/inventories/platform_nodes/configuration/*.yaml' for the cluster specified in /tmp/cluster: '{}'".format(
                    self.cluster
                )
            )
            sys.exit(1)

        clusters = list(set(clusters))
        clusters.sort()
        self.clusters = clusters

    def build_source_inventory_command(self):
        self.source_inventory_command = (
            sys.executable
            + " "
            + self.configuration["source_inventory"]["inventory_path"]
            + " --all "
            + " -u "
            + self.configuration["source_inventory"]["api_uri"]
            + " -t "
            + self.configuration["source_inventory"]["api_token"]
            + " --cache-path "
            + self.configuration["source_inventory"]["cache_path"]
            + " --cache-max_age "
            + str(self.configuration["source_inventory"]["cache_max_age"])
            + " --force-cache "
            + " --clusters "
            + " ".join(self.clusters).strip()
        )

    def verify_file(self, path):
        return True

    def parse(self, ansible_inventory, loader, path, cache=True):
        super(InventoryModule, self).parse(ansible_inventory, loader, path)
        self.load_cache_plugin()

        inventory = {}

        cache_key = self.cluster
        cache_needs_update = False
        if cache:
            try:
                inventory = self._cache[cache_key]
            except KeyError:
                cache_needs_update = True

        self.inventory = CacheableInventory(inventory)
        self.loader = loader
        if not cache or cache_needs_update:
            self.populate()
            self._cache[cache_key] = self.inventory
            self.set_cache_plugin()
        self.apply(ansible_inventory)

    def populate(self):
        self.load_configuration()
        self.build_source_inventory_command()
        self.build_groups(self.configuration["groups"])
        self.build_hosts()
        self.distribute_hosts()
        self.set_groups_variables()
        self.set_hosts_variables()

    def apply(self, inventory):
        for group in self.inventory["groups"]:
            inventory.add_group(group)
        for parent, children in self.inventory["groups_children"]:
            inventory.add_child(parent, children)
        for host, group in self.inventory["hosts"]:
            inventory.add_host(host, group)
        for group, key, value in self.inventory["variables"]:
            inventory.set_variable(group, key, value)

    def load_source_inventory(self):
        try:
            process = subprocess.Popen(
                self.source_inventory_command,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                shell=True,
            )
            out, err = process.communicate()
            exit_code = process.returncode
            if out:
                if b"Error" in out or b"error" in out or b"failed=True" in out:
                    bytes_to_string_out = out.decode()
                    error_msg = re.search(r"msg='([^']+)'", bytes_to_string_out)
                    print(
                        f"\nError while trying to compute platform_nodes inventory: {error_msg.group(1)}\n"
                    )
                    sys.exit(1)

            if exit_code != 0:
                error_message = out.decode("utf-8").strip()
                print(
                    f"Error: subprocess returned non-zero exit code {exit_code}. Error message: {error_message}"
                )
                sys.exit(1)
            else:
                return json.loads(out.decode("utf-8"))
        except Exception as e:
            print(f"Error occurred: {str(e)}")
            return 1, str(e)  # Returning non-zero exit code for generic error

    def build_groups(self, groups, group_path=None):
        for group in groups:
            if isinstance(group, str):
                self.add_group(group, group_path)
            elif isinstance(group, dict):
                for g, sub in group.items():
                    self.add_group(g, group_path)
                    self.build_groups(
                        sub, g if not group_path else group_path + "." + g
                    )

    def add_group(self, group, group_path=None):
        self.groups.add(group if not group_path else group_path + "." + group)
        self.inventory.add_group(group if not group_path else group_path + "." + group)
        if self.root_group != group:
            self.inventory.add_child(group_path, group_path + "." + group)
            if group_path not in self.groups_tree:
                self.groups_tree[group_path] = []
            self.groups_tree[group_path].append(group_path + "." + group)

    def build_hosts(self):
        inventory = self.load_source_inventory()
        for host in inventory:
            if self.filter_hosts(host):
                if host["hostname"] in self.configuration["tags"]:
                    for group in self.configuration["tags"][host["hostname"]]:
                        if group not in self.groups:
                            print(
                                '"'
                                + group
                                + '" is not a valid group name, cannot use it. exiting...'
                            )
                            sys.exit()
                if host["hostname"] in self.configuration["tags"]:
                    for group in self.configuration["tags"][host["hostname"]]:
                        if group not in self.groups:
                            group = self.root_group
                        self.add_host(host, group)
                else:
                    self.add_host(host, self.root_group)
                if self.default_group:
                    self.add_host(host, self.default_group)

    def filter_hosts(self, host):
        return host["hostname"].startswith(self.cluster + "-") and (
            "none" == host["os"]["name"] or "custom installation" == host["os"]["name"]
        )

    def add_host(self, host, group, only_if_leaf=False):
        host_id = host["hostname"]
        self.hosts[host_id] = host
        if host_id not in self.host_groups:
            self.host_groups[host_id] = []
        if group not in self.host_groups[host_id]:
            self.host_groups[host_id].append(group)
        if group not in self.group_hosts:
            self.group_hosts[group] = []
        if host_id not in self.group_hosts[group]:
            self.group_hosts[group].append(host_id)
        if host_id not in self.host_paths:
            self.host_paths[host_id] = []
        if group not in self.host_paths[host_id]:
            self.host_paths[host_id].append(group)
        host["network"]["private"][0] = str(
            self.private_network[list(self.hosts).index(host_id) + 1]
        )
        if (only_if_leaf and group not in self.groups_tree) or not only_if_leaf:
            self.inventory.add_host(host_id, group)

    def distribute_hosts(self):
        self.inherit_hosts(self.root_group, False)
        self.inherit_hosts(self.root_group, True)

    def inherit_hosts(self, group, reverse):
        if reverse:
            if group in self.groups_tree:
                for children in self.groups_tree[group]:
                    self.inherit_hosts(children, reverse)
                    if children in self.group_hosts:
                        hosts = self.group_hosts[children]
                        for host in hosts:
                            self.add_host(self.hosts[host], group)
        else:
            if group in self.groups_tree:
                hosts = []
                if group in self.group_hosts:
                    hosts = self.group_hosts[group]
                for children in self.groups_tree[group]:
                    if children not in self.group_hosts:
                        for host in hosts:
                            self.add_host(self.hosts[host], children, True)
                    self.inherit_hosts(children, reverse)

    def set_groups_variables(self):
        etcd_endpoints = []
        etcd_hosts = []
        hosts = []
        for host in self.group_hosts["platform.foundation.cluster.control"]:
            etcd_endpoints.append(
                self.etcd_uri_template
                % (self.hosts[host]["network"]["private"][0], self.etcd_port)
            )
            etcd_hosts.append(self.hosts[host]["network"]["private"][0])
        for host in self.group_hosts[self.root_group]:
            hosts.append(self.hosts[host]["network"]["private"][0])
        self.inventory.set_variable(
            self.root_group, "machine_private_vip", self.private_vip
        )
        self.inventory.set_variable(
            self.root_group, "machine_etcd_endpoints", etcd_endpoints
        )
        self.inventory.set_variable(self.root_group, "machine_etcd_hosts", etcd_hosts)
        self.inventory.set_variable(
            self.root_group, "machine_etcd_port", self.etcd_port
        )
        self.inventory.set_variable(self.root_group, "machine_hosts", hosts)
        self.inventory.set_variable(
            self.root_group,
            "machine_resources_main_groups",
            self.groups_tree[self.root_group],
        )
        storage_cluster_disk_count = 0
        self.inventory.set_variable(
            self.root_group, "machine_nodes_count", len(self.group_hosts["platform"])
        )
        for host in self.group_hosts[self.root_group]:
            # one disk for OS, others for storage cluster
            storage_cluster_disk_count += len(self.hosts[host]["disks"]) - 1
        self.inventory.set_variable(
            self.root_group, "storage_cluster_disk_count", storage_cluster_disk_count
        )
        self.inventory.set_variable(
            self.root_group,
            "machine_web_ingress_public_ip",
            self.hosts[self.group_hosts["platform.foundation.network.ingress.web"][0]][
                "network"
            ]["ip"][0],
        )
        self.inventory.set_variable(
            self.root_group,
            "machine_mail_ingress_public_ip",
            self.hosts[self.group_hosts["platform.foundation.network.ingress.mail"][0]][
                "network"
            ]["ip"][0],
        )

    def set_hosts_variables(self):
        def find_mac(ip, type):
            for interface in ip:
                if interface["type"] == type:
                    return interface["mac"]

        for host_id in self.hosts:
            host = self.hosts[host_id]
            self.inventory.set_variable(host_id, "hostname", host["hostname"])
            self.inventory.set_variable(
                host_id, "ansible_host", host["network"]["ip"][0]
            )
            self.inventory.set_variable(
                host_id, "private_mac", find_mac(host["ip"], "private")
            )
            self.inventory.set_variable(
                host_id, "public_mac", find_mac(host["ip"], "public")
            )
            self.inventory.set_variable(
                host_id, "private_ip", host["network"]["private"][0]
            )
            self.inventory.set_variable(host_id, "host_id", host["id"])
            self.inventory.set_variable(host_id, "public_ip", host["network"]["ip"][0])
            self.inventory.set_variable(
                host_id, "failover_ip", next(iter(host["network"]["ipfo"]), "")
            )
            self.inventory.set_variable(
                host_id, "private_network", host["network"]["private"][0] + "/24"
            )
            self.inventory.set_variable(
                host_id, "private_vip_network", "192.168.100.0/24"
            )
            self.inventory.set_variable(
                host_id, "public_network", host["network"]["ip"][0] + "/24"
            )
            fo_network = next(iter(host["network"]["ipfo"]), "") + "/32"
            self.inventory.set_variable(
                host_id, "failover_network", fo_network if fo_network != "/32" else ""
            )
            self.inventory.set_variable(
                host_id,
                "public_gateway",
                ".".join(host["network"]["ip"][0].split(".")[:3]) + ".1",
            )
            self.inventory.set_variable(
                host_id,
                "labels",
                "=true ".join([path for path in self.host_paths[host_id]]) + "=true",
            )
            disk_id_format = (
                digits if host["disks"][0]["type"] == "NVMe" else ascii_lowercase
            )
            disk_prefix = (
                "/dev/nvme" if host["disks"][0]["type"] == "NVMe" else "/dev/sd"
            )
            disk_suffix = "n1" if host["disks"][0]["type"] == "NVMe" else ""
            self.inventory.set_variable(
                host_id,
                "disks",
                [
                    disk[0]
                    for disk in zip(
                        [
                            disk_prefix + "".join(disk_id) + disk_suffix
                            for disk_id in [
                                product(disk_id_format, repeat=size)
                                for size in range(1, 2)
                            ][0]
                        ],
                        host["disks"],
                    )
                ],
            )
            self.inventory.set_variable(
                host_id,
                "display_os_rebuild_confirmation",
                self.configuration["display_os_rebuild_confirmation"],
            )
            self.inventory.set_variable(
                host_id,
                "display_data_wipe_confirmation",
                self.configuration["display_data_wipe_confirmation"],
            )
            self.inventory.set_variable(
                host_id, "os_wipe_data", self.configuration["os_wipe_data"]
            )
