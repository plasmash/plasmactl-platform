# Make coding more python3-ish, this is required for contributions to Ansible
from __future__ import (absolute_import, division, print_function)
__metaclass__ = type

from ansible.plugins.callback import CallbackBase

class CallbackModule(CallbackBase):

    CALLBACK_VERSION = 1.0
    CALLBACK_NAME = 'auto_tags'

    def v2_playbook_on_start(self, playbook):
        for play in  playbook.get_plays():
            play_tags = []
            for play_name_part in play.get_name().split('.'):
                play_tag = ('' if len(play_tags) == 0 else play_tags[-1] + '.') + play_name_part
                play_tags.append(play_tag)
                if play_tag not in play.tags:
                    play.tags += [play_tag]
            for role in play.get_roles():
                role_name = role.get_name()
                if role_name not in role.tags:
                    role.tags += [role_name]
