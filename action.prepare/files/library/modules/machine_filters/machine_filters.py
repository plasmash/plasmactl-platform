import yaml
import time
from decimal import Decimal
from machine.resource import Resource
from application import Application
from service import Service
from flow import Flow
from executor import Executor
from skill import Skill
from sysentity import SysEntity
from software import Software
from function import Function
from helper import Helper
from builder import Builder
from library import Library
from typing import get_type_hints


def load_configuration():
    with open('platform/group_vars/platform/vars.yaml') as file:
        vars = yaml.full_load(file)
    return {'machine_resource_default_tag': vars['machine_resource_default_tag'], 'current_tag_suffix': vars['current_tag_suffix'], 'machine_platform_name': vars['machine_platform_name']}


configuration = load_configuration()


def machine_state(resource, tag=configuration['machine_resource_default_tag'], current_version=None):

    if current_version:
        if 'skipped' in current_version and current_version['skipped']:
            version = None
        else:
            version = current_version['stdout'] if 'stdout' in current_version else ''
            if version == '':
                version = None
        if 'state' not in resource:
            resource['state'] = {}
        state = {tag: {'mrv': version, 'exists': version is not None,
                       'fresh': version == resource['mrv'], 'build': version != resource['mrv']}}
        resource['state'] = merge_dicts(resource['state'], state)
        return resource
    if 'state' not in resource:
        return None
    if tag not in resource['state']:
        return None

    return resource['state'][tag]


def machine_exists(resource, tag=configuration['machine_resource_default_tag']):
    state = machine_state(resource, tag)
    if 'exists' in state:
        return state['exists']
    return None


def machine_build(resource, tag=configuration['machine_resource_default_tag']):
    state = machine_state(resource, tag)
    if 'build' in state:
        return state['build']
    return None


def machine_built(resource, tag=configuration['machine_resource_default_tag'], deployment=None):
    if deployment == 'current':
        return not machine_state(resource, tag=tag)["build"] and not machine_state(resource, tag=tag)["fresh"]
    if deployment is None or deployment == "any":
        return not machine_state(resource, tag=tag)["build"]


def machine_requires_lookup(resource, hostvars, kind):
    resources = []
    for dependency_name in resource['requires']:
        dependency_resource = hostvars[list(
            hostvars)[0]][dependency_name.replace('-', '_')]
        if dependency_resource['mrk'] == kind:
            resources.append(dependency_resource)
    return resources


def machine_requires_one(resource, hostvars, kind):
    resources = machine_requires_lookup(resource, hostvars, kind)
    if len(resources) > 0:
        return resources[0]
    return None


def machine_requires_skill(resource, hostvars):
    return machine_requires_one(resource, hostvars, 'skill')


def machine_requires_function(resource, hostvars):
    return machine_requires_one(resource, hostvars, 'function')


def machine_deep_requires_lookup(resource, hostvars):
    resources = {}
    for dependency_name in resource['requires']:
        dependency_resource = hostvars[list(
            hostvars)[0]][dependency_name.replace('-', '_')]
        if dependency_resource['mrk'] not in resources:
            resources[dependency_resource['mrk']] = []
        resources[dependency_resource['mrk']].append(dependency_resource)
        resources = merge_dicts(resources, machine_deep_requires_lookup(
            dependency_resource, hostvars))
    return resources


def machine_requiredby_lookup(resource, hostvars, kind):
    resources = []
    for dependency_name in resource['requiredby']:
        dependency_resource = hostvars[list(
            hostvars)[0]][dependency_name.replace('-', '_')]
        if dependency_resource['mrk'] == kind:
            resources.append(dependency_resource)
    return resources


def machine_requiredby_flows(resource, hostvars):
    return machine_requiredby_lookup(resource, hostvars, 'flow')


def machine_stage(resource):
    return resource['stage']


def machine_mrsn(resource):
    return resource['mrsn']


def merge_dicts(left, right):
    dicts = {**left, **right}
    for key, value in dicts.items():
        if key in left and key in right:
            if isinstance(value, dict):
                dicts[key] = merge_dicts(left[key], value)
            elif isinstance(value, list):
                dicts[key] = unique(value + left[key])
            else:
                dicts[key] = right[key]
        elif isinstance(value, list):
            dicts[key] = unique(value)
    return dicts


def unique(items):
    u = []
    for item in items:
        if item not in u:
            u.append(item)
    return u


def machine_machine_name(resource):
    return resource['mrsn'].replace('-', '')


def machine_library_path(resource):
    return resource['mrsn'].replace('-', '/')


def machine_image_uri(mri):
    return ':'.join(mri.split(':')[0:-1])


def machine_image_tag(mri):
    """
    Extracts the tag portion from a Machine Resource Image (MRI)

    Args:
        mri: Full MRI string (e.g., 'registry/path/image:version_tag')

    Returns:
        The tag portion after the colon (e.g., 'version_tag')

    Example:
        'images.foundation.svc.skilld/interaction/services/name:d141a45278ac5_default'
        -> 'd141a45278ac5_default'
    """
    return mri.split(':')[-1]


def machine_image_path(mri):
    return '/'.join(mri.split('/')[1:]).split(':')[:1][0]


def machine_cluster_dns_name(resource, service_ext=''):
    return (resource['mrsn'] if service_ext == '' else resource['mrsn'] + '-' + service_ext) + "." + resource['mrns'] + '.svc.' + configuration['machine_platform_name']


def machine_resource(obj):
    type_hints = get_type_hints(Resource)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_application(obj):
    type_hints = get_type_hints(Application)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_service(obj):
    type_hints = get_type_hints(Service)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_flow(obj):
    type_hints = get_type_hints(Flow)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_executor(obj):
    type_hints = get_type_hints(Executor)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_skill(obj):
    type_hints = get_type_hints(Skill)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_entity(obj):
    type_hints = get_type_hints(SysEntity)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_software(obj):
    type_hints = get_type_hints(Software)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_function(obj):
    type_hints = get_type_hints(Function)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_helper(obj):
    type_hints = get_type_hints(Helper)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_builder(obj):
    type_hints = get_type_hints(Builder)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_library(obj):
    type_hints = get_type_hints(Library)
    d = {}
    for k, v in obj.items():
        if k in type_hints:
            d[k] = v
    return d


def machine_append_timestamp(s):
    return s + str(int(Decimal("%.9f" % time.time()) * 1000000000))

def machine_nodeselector_key(resource):
    return resource['nodeselector'].split(': ')[0]

def machine_nodeselector_value(resource):
    return resource['nodeselector'].split(': ')[1]

def machine_json_schema(entity):
    """
    Loads JSON Schema content from entity object.

    UPDATED: Now accepts entity object to bypass entity builder schema mutation issue.

    Works around the problem where entity.schema gets mutated from local path
    to S3 URI before template rendering, breaking build-time schema access.

    Reconstructs the local JSON schema path from entity metadata (path + mrsn)
    following the same pattern used in platform_components.py line 226.

    Args:
        entity: Entity object with 'path' and 'mrsn' fields

    Returns:
        dict: Parsed JSON Schema content, or error dict if file not found/invalid

    Example:
        {{ platform__entities__person | json_schema | to_json }}
    """
    import json

    # Validate entity object
    if not entity or not isinstance(entity, dict):
        return {"error": "Invalid entity object"}

    # Extract entity path and short name
    path = entity.get('path', '')
    mrsn = entity.get('mrsn', '')

    if not path or not mrsn:
        return {"error": "Entity missing path or mrsn fields"}

    # Convert mrsn to machine_mrsn (replace hyphens with underscores)
    # Following platform_components.py line 216 pattern
    machine_mrsn = mrsn.replace('-', '_')

    # Construct JSON schema path following same pattern as proto schema
    # platform_components.py line 226: '%s/files/%s.proto' % (path, machine_mrsn)
    json_path = f"{path}/files/{machine_mrsn}.json"

    try:
        with open(json_path, 'r') as f:
            return json.load(f)
    except FileNotFoundError:
        return {"error": f"JSON Schema not found: {json_path}"}
    except json.JSONDecodeError as e:
        return {"error": f"Invalid JSON in {json_path}: {str(e)}"}

class FilterModule(object):

    def filters(self):
        return {
            'state': machine_state,
            'build': machine_build,
            'built': machine_built,
            'exists': machine_exists,
            'requires_lookup': machine_requires_lookup,
            'requires_skill': machine_requires_skill,
            'requires_function': machine_requires_function,
            'deep_requires_lookup': machine_deep_requires_lookup,
            'machine_name': machine_machine_name,
            'library_path': machine_library_path,
            'requiredby_flows': machine_requiredby_flows,
            'requiredby_lookup': machine_requiredby_lookup,
            'stage': machine_stage,
            'mrsn': machine_mrsn,
            'image_uri': machine_image_uri,
            'image_tag': machine_image_tag,
            'image_path': machine_image_path,
            'cluster_dns_name': machine_cluster_dns_name,
            'resource': machine_resource,
            'application': machine_application,
            'service': machine_service,
            'flow': machine_flow,
            'executor': machine_executor,
            'skill': machine_skill,
            'entity': machine_entity,
            'software': machine_software,
            'function': machine_function,
            'helper': machine_helper,
            'builder': machine_builder,
            'library': machine_library,
            'append_timestamp': machine_append_timestamp,
            'nodeselector_key': machine_nodeselector_key,
            'nodeselector_value': machine_nodeselector_value,
            'json_schema': machine_json_schema
        }
