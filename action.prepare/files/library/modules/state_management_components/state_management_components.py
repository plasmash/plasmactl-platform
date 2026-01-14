#!/usr/bin/python

from abc import ABC, abstractmethod
import os
import subprocess
from itertools import compress
import re
import concurrent.futures
from ansible.module_utils.basic import AnsibleModule
import sys
import logging
from datetime import datetime
import glob
import shutil

try:
    import json
except ImportError:
    import simplejson as json


class VersionFetcher(ABC):
    @abstractmethod
    def fetch_version(self):
        pass


class OSVersionFetcher(VersionFetcher):
    def fetch_version(self, state_builder_machine_resource_default_tag):
        return self.get_env_vars_with_mrv(state_builder_machine_resource_default_tag)

    def get_env_vars_with_mrv(self, state_builder_machine_resource_default_tag):
        result = os.popen("source /etc/profile && env").read()
        lines = [line for line in result.split("\n") if "MRV" in line]
        data = {}
        for line in lines:
            key, value = line.strip().split("=")
            key = key.replace("_MRV", "").lower()
            data[key] = {state_builder_machine_resource_default_tag: [value]}
        return data


class ClusterVersionFetcher(VersionFetcher):
    def fetch_version(self, state_builder_machine_resource_default_tag):
        resources = [
            "statefulset",
            "sparkapplication",
            "objectbucketclaim",
            "storageclass",
            "prometheus",
            "deployment",
            "daemonset",
        ]
        data = {}
        for kind in resources:
            cmd = f"""/opt/bin/kubectl get {kind} -A -o json | jq '.items[] |
            select(.metadata.annotations.mrn and .metadata.annotations.mrv) |
            {{(.metadata.annotations.mrn | gsub("-"; "_")):
            {{ "{state_builder_machine_resource_default_tag}" :
            [.metadata.annotations.mrv]}}}}' | jq -s 'add' """
            result = subprocess.check_output(cmd, shell=True).decode("utf-8")
            if result.rstrip() != "null":
                data.update(json.loads(result))
        return data


class ImagesVersionFetcher(VersionFetcher):
    def fetch_version(self, state_builder_images_uri, state_builder_images_auth):
        # Fetch from Docker Registry
        docker_registry_data = self.fetch_from_registry(
            state_builder_images_uri, state_builder_images_auth
        )

        # Fetch Images Locally
        local_images_data = self.fetch_local_images(state_builder_images_uri)
        merged_data = {}
        for key, value in docker_registry_data.items():
            if key in local_images_data:
                local_images_data[key].update(value)
            else:
                local_images_data[key] = value
        merged_data = local_images_data
        return merged_data

    def fetch_local_images(self, state_builder_images_uri):
        logging.info("Start to fetch local images")
        try:
            result = subprocess.run(
                "sudo crictl images | grep images",
                shell=True,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=30,
                text=True,
            )

            if result.stdout:
                stdout_msg = (
                    f"STDOUT: {result.stdout}\nCrictl command executed successfully."
                )
                logging.info(stdout_msg)

            if result.stderr:
                stderr_msg = (
                    f"STDERR: {result.stderr}\nError while executing crictl command."
                )
                logging.error(stderr_msg)
                return {}

            return self.process_crictl_output(result.stdout, state_builder_images_uri)

        except subprocess.TimeoutExpired:
            logging.error("Subprocess timed out.")
        except subprocess.CalledProcessError as e:
            stderr_msg = (
                f"STDERR: {e.stderr}\nCommand failed with exit status {e.returncode}."
            )
            logging.error(stderr_msg)
        except Exception as e:
            logging.error(f"Error has occurred while fetching local images: {e}")
        return {}

    def process_crictl_output(self, stdout, state_builder_images_uri):
        crictl_output = stdout.strip().split("\n")
        result = {}
        crictl_output.pop()
        for line in crictl_output:
            columns = line.split()
            if len(columns) < 2:
                continue
            image, tag = columns[0], columns[1]
            if "cur" not in tag:
                continue
            if "_" in tag and "__" not in tag:
                tag_parts = tag.split("_", 1)
                if len(tag_parts) == 2:
                    tag_id, tag_suffix = tag_parts
                    tag_suffix = tag_suffix.replace("-cur", "")
                else:
                    continue
            else:
                continue
            image_parts = image.split("/")
            mrn = "__".join(
                [part for part in image_parts if part != state_builder_images_uri]
            ).replace("-", "_")
            if mrn in result:
                result[mrn][tag_suffix] = [tag_id]
            else:
                result[mrn] = {tag_suffix: [tag_id]}

        return result

    def fetch_from_registry(self, state_builder_images_uri, state_builder_images_auth):
        def run_command(command, last):
            try:
                resp = (
                    subprocess.check_output(command, shell=True, timeout=40)
                    .decode("utf-8")
                    .strip()
                    .split("\n")
                )
                if last == "normal":
                    return resp
                values = json.loads(resp[-1])
                if "errors" in values:
                    error_message = json.loads(resp[-1])
                    for error in error_message["errors"]:
                        raise ConnectionRefusedError(
                            f"Error: Command failed with exit code {error['code']}, and message: {error['message']}."
                        )
                link_exists, isLinkPresentList = is_present(resp, "link")
                if link_exists:
                    last = get_last(isLinkPresentList, resp)
                return values, last, link_exists

            except subprocess.TimeoutExpired:
                logging.error("Error: Command timed out after 40 seconds.")
                sys.exit("Error: Command timed out after 40 seconds.")

        def catalog_expr(n=100, last=""):
            if last == "":
                return "/v2/_catalog?n={}".format(n)
            else:
                return "'" + last + "'"

        def is_present(responseLines, t_string):
            isLinkPresentList = list(t_string in a for a in responseLines)
            return (True in isLinkPresentList, isLinkPresentList)

        def get_last(isLinkPresentList, responseLines):
            pattern = r"<(.*?)>"
            match = re.search(
                pattern, (list(compress(responseLines, isLinkPresentList)))[0]
            )
            return match.group(1)

        def get_paths():
            cmd = 'curl -i -s -H "Authorization: Basic {}" '.format(
                state_builder_images_auth
            ) + "https://{}".format(state_builder_images_uri)
            last = ""
            paths = []
            link_exists = True
            try:
                while link_exists:
                    values, last, link_exists = run_command(
                        cmd + catalog_expr(n=10, last=last), last
                    )
                    paths = paths + values["repositories"]
            except Exception as e:
                logging.error(f"exception in fetching links {e}")
                pass
            return paths

        paths = get_paths()
        cmd = 'curl -s -H "Authorization: Basic {}" '.format(
            state_builder_images_auth
        ) + "https://{}".format(state_builder_images_uri)
        command_lines = [cmd + "/v2/" + p + "/tags/list" for p in paths]
        results = {}
        with concurrent.futures.ThreadPoolExecutor(max_workers=50) as executor:
            futures = {
                executor.submit(run_command, command, "normal"): command
                for command in command_lines
            }
            concurrent.futures.wait(futures)

            for future in concurrent.futures.as_completed(futures):
                res = future.result()
                result = json.loads(res[0])
                if result["tags"] is None:
                    result["tags"] = []
                exist, isCurrentTagPresentList = is_present(result["tags"], "cur")
                current_tag_dict = {}
                if exist:
                    for i, cur_bool in enumerate(isCurrentTagPresentList):
                        cur = result["tags"][i]
                        if "_" in cur and "__" not in cur:
                            prefix, suffix = cur.split("_")[0], (
                                cur.split("_")[1]
                            ).replace("-cur", "")
                            if suffix in current_tag_dict:
                                current_tag_dict[suffix].append(prefix)
                            else:
                                current_tag_dict[suffix] = [prefix]
                            if cur_bool:
                                current_tag_dict["current_version"] = prefix
                        else:
                            continue
                resource_name = result["name"].replace("/", "__").replace("-", "_")
                if resource_name in results:
                    results[resource_name].update(current_tag_dict)
                else:
                    results[resource_name] = current_tag_dict
        return results


class StateCreator:
    def __init__(self, merged_data):
        self.merged_data = merged_data

    def state_helper(self, new_version, tag="default", old_version=None):
        if old_version is None:
            old_version = {}
        return {
            tag: {
                "mrv": old_version.get(tag, []),
                "mrv_cur": old_version.get(
                    "current_version", old_version.get(tag, None)
                ),
                "exists": old_version.get(tag, []) is not None,
                "fresh": new_version in old_version.get(tag, []),
                "build": new_version not in old_version.get(tag, []),
            }
        }

    def create_state(self, state_builder_platform_components):
        current_states = self.merged_data
        states = {}
        for mrn, comp in state_builder_platform_components.items():
            try:
                mrv = comp["mrv"]
                states[mrn] = comp
                for tag in comp["mrt"]:
                    if mrn in list(current_states.keys()) and tag in list(
                        current_states[mrn].keys()
                    ):
                        machine_mrsn = comp["mrsn"].replace("-", "_")
                        states[mrn][
                            "password"
                        ] = '{{ %s_service_plain_password|default("") }}' % (
                            machine_mrsn
                        )
                        current_version = current_states[mrn]
                        state = self.state_helper(
                            new_version=mrv, tag=tag, old_version=current_version
                        )
                    else:
                        state = self.state_helper(tag=tag, new_version=mrv)

                    if "state" in states[mrn]:
                        states[mrn]["state"].update(state)
                    else:
                        states[mrn]["state"] = state
            except Exception as e:
                print(e)
        return states


def manage_files(directory, prefix, max_files):
    files = sorted(
        glob.glob(os.path.join(directory, f"{prefix}_*.json")), key=os.path.getctime
    )
    while len(files) >= max_files:
        os.remove(files.pop(0))


def main():
    folder_path = "/tmp/state_management_logs/"
    if not os.path.exists(folder_path):
        os.mkdir(folder_path)

    prefix = "vars_with_state"
    max_files = 4

    logging.basicConfig(
        level=logging.INFO,
        filename=f"{folder_path}fetch_local_images.log",
        format="%(asctime)s:%(levelname)s:%(message)s",
        filemode="w",
    )
    logging.debug("Start")

    os_fetcher = OSVersionFetcher()
    images_fetcher = ImagesVersionFetcher()
    cluster_fetcher = ClusterVersionFetcher()

    module_args = dict(
        state_builder_platform_components=dict(type=dict, required=True),
        state_builder_images_uri=dict(type="str", required=True),
        state_builder_machine_resource_default_tag=dict(type="str", required=True),
        state_builder_images_auth=dict(type="str", required=True),
    )
    result = {}
    module = AnsibleModule(argument_spec=module_args, supports_check_mode=True)

    if module.check_mode:
        module.exit_json(**result)

    merged_data = {}
    states = {}
    try:
        os_data = os_fetcher.fetch_version(
            module.params["state_builder_machine_resource_default_tag"]
        )
        with open(folder_path + "os_data", "w") as json_file:
            json.dump(os_data, json_file, indent=4)
        if os_data is not None:
            merged_data.update(os_data)

        else:
            module.warn("Warning: OS data is None. Skipping update")

        if os_data == {}:
            module.warn("Warning: OS data is empty. Skipping update")

        cluster_data = cluster_fetcher.fetch_version(
            module.params["state_builder_machine_resource_default_tag"]
        )
        with open(folder_path + "cluster_data", "w") as json_file:
            json.dump(cluster_data, json_file, indent=4)
        if cluster_data is not None:
            merged_data.update(cluster_data)
        else:
            module.warn("Warning: Cluster data is None. Skipping update.")

        if cluster_data == {}:
            module.warn("Warning: cluster data is empty. Skipping update")

        images_data = images_fetcher.fetch_version(
            module.params["state_builder_images_uri"],
            module.params["state_builder_images_auth"],
        )
        with open(folder_path + "images_data", "w") as json_file:
            json.dump(images_data, json_file, indent=4)
        if images_data is not None:
            merged_data.update(images_data)

        else:
            module.warn("Warning: Images data is None. Skipping update")

        with open(folder_path + "merge_data", "w") as json_file:
            json.dump(merged_data, json_file, indent=4)
        if images_data == {}:
            module.warn("Warning: images data is empty. Skipping update")
        if merged_data != {}:
            state_creator = StateCreator(merged_data)
            states = state_creator.create_state(
                module.params["state_builder_platform_components"]
            )

        # Create a new state file
        new_file_path = "/tmp/vars_with_state.json"

        # Move the old vars_with_state.json to the log folder if it exists
        if os.path.exists(new_file_path):
            # Get the creation time of the existing file
            creation_time = os.path.getctime(new_file_path)
            timestamp = datetime.fromtimestamp(creation_time).strftime("%Y%m%d_%H%M%S")
            old_file_path = os.path.join(folder_path, f"{prefix}_{timestamp}.json")
            shutil.move(new_file_path, old_file_path)
            # Manage the files to keep only the last `max_files` files in the log folder
            manage_files(folder_path, prefix, max_files)

        # Write the new state to /tmp/vars_with_state.json
        with open(new_file_path, "w") as json_file:
            json.dump(states, json_file, indent=4)

        module.exit_json(**states)
    except subprocess.CalledProcessError as e:
        if merged_data != {}:
            state_creator = StateCreator(merged_data)
            states = state_creator.create_state(
                module.params["state_builder_platform_components"]
            )
        new_file_path = "/tmp/vars_with_state.json"
        with open(new_file_path, "w") as json_file:
            json.dump(states, json_file, indent=4)
        module.warn(str(e))
        module.exit_json(**states)
    except Exception as e:
        module.fail_json(msg=str(e))


if __name__ == "__main__":
    main()
