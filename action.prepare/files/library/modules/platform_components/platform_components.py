# platform_components.py

from ansible.plugins.vars import BaseVarsPlugin
from ansible.inventory.group import Group
import os
import re
import yaml
import glob
from typing import get_type_hints
from machine.resource import Resource
from application import Application
from service import Service
from flow import Flow, Skill as FlowSkill
from executor import Executor
from skill import Skill
from sysentity import SysEntity
from software import Software
from function import Function
from helper import Helper
from builder import Builder
from library import Library
from ansible.playbook import Playbook
from ansible.vars.manager import VariableManager
from ansible.playbook.role.include import RoleInclude
from ansible.playbook.role import Role

DOCUMENTATION = '''
    name: platform_components
    plugin_type: vars
    short_description: Discover and expose platform components metadata.
    description: Discovers the various platform components from the file-system and registers \
    a variable for each component to make their metadata accessible.
'''


def setattr_if_has_type_hint(resource, attr_name, attr_value):
    if attr_name in get_type_hints(resource.__class__):
        super(resource.__class__, resource).__setattr__(attr_name, attr_value)


class ApplicationResource(Resource, Application):
    pass


class ServiceResource(Resource, Service):
    pass


class FlowResource(Resource, Flow):
    pass


class ExecutorResource(Resource, Executor):
    pass


class SkillResource(Resource, Skill):
    pass


class EntityResource(Resource, SysEntity):
    pass


class SoftwareResource(Resource, Software):
    pass


class FunctionResource(Resource, Function):
    pass


class HelperResource(Resource, Helper):
    pass


class BuilderResource(Resource, Builder):
    pass


class LibraryResource(Resource, Library):
    pass


class VarsModule(BaseVarsPlugin):
    _instance = None
    REQUIRES_ENABLED = True

    def __new__(cls, *args, **kwargs):
        if not cls._instance:
            cls._instance = super(VarsModule, cls).__new__(cls)
        return cls._instance

    def __init__(self):
        if not hasattr(self, 'loader'):
            super(BaseVarsPlugin, self).__init__()
            self.loader = None
            self.inventory = None
            self.playbook_file = None
            self.groups = set()
            self.resources = {}
            self.normalized_resources = {}
            self.resource_namespaces = []
            self.machine_resources = []
            self.root_group = 'platform'
            self.roles_cache = {}
            self.playbook_file = 'platform/platform.yaml'
            self.kind_singular = {
                'applications': 'application',
                'services': 'service',
                'flows': 'flow',
                'executors': 'executor',
                'skills': 'skill',
                'helpers': 'helper',
                'builders': 'builder',
                'libraries': 'library',
                'entities': 'entity',
                'softwares': 'software',
                'functions': 'function',
            }
            self.kind_plural = {v: k for k, v in self.kind_singular.items()}

            self.kind_scheme = {
                'application': 'app',
                'service': 'svc',
                'flow': 'flow',
                'executor': 'executor',
                'skill': 'skill',
                'entity': 'ent',
                'software': 'soft',
                'function': 'function',
                'helper': 'hp',
                'builder': 'build',
                'library': 'lib',
            }

            self.kind_constructor = {
                'application': ApplicationResource,
                'service': ServiceResource,
                'flow': FlowResource,
                'executor': ExecutorResource,
                'skill': SkillResource,
                'entity': EntityResource,
                'software': SoftwareResource,
                'function': FunctionResource,
                'helper': HelperResource,
                'builder': BuilderResource,
                'infrastructure': Resource,
                'library': LibraryResource,
            }

    def get_vars(self, loader, path, entities, cache=True):
        ''' Load variables from the platform components '''
        try:
            self.loader = loader
            if len(self.resources) == 0:
                self.populate()
                self.normalized_resources = self.normalize_resources()
                self.normalized_resources['platform_components'] = self.normalized_resources.copy()
            if entities != []:
                if isinstance(entities[0], Group) and (
                        entities[0].name == "platform" or entities[0].name.startswith("platform.")):
                    return self.normalized_resources
        except Exception as error:
            raise RuntimeError(error)
        return {}

    def normalize_resources(self):
        normalized = {}
        for k, v in self.resources.items():
            if not isinstance(v, dict) and not isinstance(v, list):
                normalized[k] = v.__dict__
            else:
                normalized[k] = v
        return normalized

    def populate(self):
        self.build_resources()
        self.process_resources_dependencies()
        self.resources['machine_resources'] = list(self.resources.keys())
        self.resources['machine_resource_namespaces'] = self.resource_namespaces

    def build_resources(self):
        playbook = Playbook.load(
            self.playbook_file,
            loader=self.loader,
            variable_manager=VariableManager(
                loader=self.loader))
        for play in playbook.get_plays():
            play_name = play.get_name()
            for role in play.get_roles():
                self.add_resource(role, play_name)

    def process_resources_dependencies(self):
        for resource_name in self.resources:
            resource = self.resources[resource_name]
            for dep_name in resource.requires:
                dep_resource = self.resources[dep_name]
                dep_resource.requiredby.append(resource_name)

    def add_resource(self, role, group_path):
        resource_name = role.get_name()
        resource_normalized_name = self.normalize_name(resource_name)
        if resource_normalized_name in self.resources:
            resource = self.resources[resource_normalized_name]
        else:
            metadata = self.metadata(role)
            path = role._role_path
            kind = self.resource_kind_from_role_path(path)
            resource = self.kind_constructor[kind]()
            mrsn = resource_name.split('.')[-1].replace("_", "-")
            mrns = resource_name.split('.')[0]
            setattr_if_has_type_hint(resource, 'mrns', mrns)
            if mrns not in self.resource_namespaces:
                self.resource_namespaces.append(resource.mrns)
            machine_mrsn = mrsn.replace('-', '_')
            mrn = resource_normalized_name
            setattr_if_has_type_hint(resource, 'mrn', mrn)
            setattr_if_has_type_hint(resource, 'mrsn', mrsn)
            setattr_if_has_type_hint(resource, 'name', resource_normalized_name)
            setattr_if_has_type_hint(
                resource,
                'public_uri',
                '%s.{{ machine_domain_name }}.{{ machine_domain_ext }}' %
                (mrsn))
            setattr_if_has_type_hint(resource, 'schema', '%s/files/%s.proto' % (path, machine_mrsn))
            setattr_if_has_type_hint(resource, 'author', metadata['author'])
            setattr_if_has_type_hint(resource, 'description', metadata['description'])
            setattr_if_has_type_hint(
                resource,
                'password',
                '{{ %s_service_plain_password|default("") }}' %
                (machine_mrsn))
            setattr_if_has_type_hint(resource, 'relpath', os.path.relpath(role._role_path).replace('ansible_collections/', ''))
            setattr_if_has_type_hint(resource, 'path', path)
            if 'labels' in metadata:
                setattr_if_has_type_hint(resource, 'labels', metadata['labels'])
            version = 'unknown'
            if 'version' in metadata:
                version = metadata['version']
            setattr_if_has_type_hint(resource, 'mrv', version)
            setattr_if_has_type_hint(resource, 'version', version)
            scope = 'global'
            if 'scope' in metadata:
                scope = metadata['scope']
            setattr_if_has_type_hint(resource, 'mrs', scope)
            setattr_if_has_type_hint(resource, 'mrk', kind)
            setattr_if_has_type_hint(
                resource,
                'mrl',
                self.kind_scheme[kind] + '://' + group_path + '/' + mrsn)

            mrc = group_path.replace('/', '.') + '.' + mrsn
            setattr_if_has_type_hint(resource, 'mrc', mrc)

            setattr_if_has_type_hint(
                resource,
                'nodeselector',
                group_path.split('/')[0] + ': "true"')
            tags = {}
            if 'tags' in metadata:
                tags = metadata['tags']
                setattr_if_has_type_hint(resource, 'mrt', tags)
            else:
                setattr_if_has_type_hint(resource, 'mrt', ['default'])

            self.init_state(resource)

            if kind == 'library':
                setattr_if_has_type_hint(resource, 'languages', tags)
            if kind == 'function':
                function_ext = glob.glob(path + "/files/*")[0].split('/')[-1].split('.')[-1]
                language = ''
                if function_ext == 'go':
                    language = 'golang'
                elif function_ext == 'scala':
                    language = 'scala'
                setattr_if_has_type_hint(resource, 'language', language)

            dependencies = role.get_direct_dependencies() + self.get_included_dependencies(role)
            for r in dependencies:
                self.add_resource(r, group_path + '/' + mrsn if kind != 'executor' else group_path)
            requires = [self.normalize_name(r.get_name()) for r in dependencies]
            setattr_if_has_type_hint(resource, 'requires', requires)
            setattr_if_has_type_hint(resource, 'mri', self.images_by_tags(resource))
            require_names_by_kind = {}
            for require_name in requires:
                require = self.resources[require_name]
                if require.mrk not in require_names_by_kind:
                    require_names_by_kind[require.mrk] = []
                require_names_by_kind[require.mrk].append(require.mrn)
            for mrk, require_names in require_names_by_kind.items():
                setattr_if_has_type_hint(resource, self.kind_plural[mrk], require_names)
                if len(require_names) > 0:
                    setattr_if_has_type_hint(resource, mrk, require_names[0])
            if 'stage' in metadata:
                setattr_if_has_type_hint(resource, 'stage', metadata['stage'])
            if kind == 'flow':
                for task in role._load_role_yaml('tasks'):
                    if 'vars' in task:
                        v = task['vars']
                        if 'flow_output' in v:
                            inp = v['flow_input']
                            if '{{ ' + mrn + '.path' + ' }}' in inp:
                                inp = inp.replace('{{ ' + mrn + '.path' + ' }}', path)
                            setattr_if_has_type_hint(resource, 'input', inp)
                            trigger = v['flow_trigger']
                            output = v['flow_output']
                            if mrn + '.mrc' in trigger:
                                trigger = trigger.replace(
                                    '{{',
                                    '').replace(
                                    '}}',
                                    '').replace(mrn + '.mrc', mrc).replace(
                                    ' ',
                                    '')
                            if mrn + '.mrc' in output:
                                output = output.replace(
                                    '{{',
                                    '').replace(
                                    '}}',
                                    '').replace(mrn + '.mrc', mrc).replace(
                                    ' ',
                                    '')
                            setattr_if_has_type_hint(resource, 'trigger', trigger)
                            setattr_if_has_type_hint(resource, 'output', output)
                            fs = FlowSkill()
                            fs.name = resource.skill
                            fs.stage = self.resources[resource.skill].stage
                            setattr_if_has_type_hint(resource, 'skill', fs.__dict__)
            self.resources[resource_normalized_name] = resource

    def init_state(self, resource):

        default_state = {"mrv": "", "exists": False, "fresh": False, "build": True}
        state = {}
        for tag in resource.mrt:
            state[tag] = default_state
        setattr_if_has_type_hint(resource, 'state', state)

    def images_by_tags(self, resource):
        images = {}
        if self.resource_has_image(resource):
            for tag in resource.mrt:
                images[tag] = "%s{{ machine_platform_name }}/%s:%s_%s" % (
                    'images.foundation.svc.',
                    resource.mrn.replace('__', '/').replace('_', '-'), resource.mrv, tag)
        return images

    def resource_has_image(self, resource):
        return resource.mrk in [
            'entity',
            'flow',
            'function',
            'library',
            'service',
            'skill',
            'software',
            'executor']

    def get_included_dependencies(self, role):
        def findfiles(path):
            res = []
            for root, dirs, fnames in os.walk(path):
                for fname in fnames:
                    if fname.endswith('.yaml'):
                        res.append(os.path.join(root, fname))
            return res

        def findIncludes(filepath):
            regObj = re.compile("^\\s*include_role:.*")
            res = []
            with open(filepath) as f:
                found = False
                for line in f:
                    if found:
                        found = False
                        name = line.replace('name:', '').strip()
                        if name not in res:
                            res.append(name)
                    if regObj.match(line):
                        found = True
            return res

        includes = []
        files = findfiles(role._role_path.replace(os.getcwd(), '')[1:])
        for f in files:
            matches = findIncludes(f)
            if len(matches) > 0:
                includes += matches
        roles = []
        for include in includes:
            if include not in self.roles_cache:
                i = RoleInclude.load(
                    include,
                    play=role._play,
                    loader=role._loader,
                    variable_manager=role._variable_manager)
                self.roles_cache[include] = Role.load(i, role._play, from_include=True)
            roles.append(self.roles_cache[include])
        return roles

    def resource_kind_from_role_path(self, path):
        return self.kind_singular[path.split('/')[-3]]

    def metadata(self, role):
        meta_file = "%s/meta/plasma.yaml" % role._role_path
        if os.path.exists(meta_file):
            with open(meta_file, 'r') as stream:
                meta = yaml.load(stream, Loader=yaml.FullLoader)
        else:
            print("Meta file (%s) is missing" % meta_file)
            return {}
        if 'plasma' not in meta:
            print("Meta miss plasma object")
            return {}
        return meta['plasma']

    def normalize_name(self, name):
        return name.replace('-', '_').replace('.', '__')
