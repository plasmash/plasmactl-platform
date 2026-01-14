FROM alpine:3.22

WORKDIR /host
ARG USER_ID
ARG USER_NAME
ARG GROUP_ID

RUN adduser -D -u ${USER_ID} -g ${GROUP_ID} -h /home/${USER_NAME} ${USER_NAME} \
    && chown -R ${USER_NAME}:${USER_NAME} /host \
    && apk upgrade --update-cache -a && apk add \
      curl \
      openssh-client \
      python3 \
      sshpass \
      yaml \
    && rm -fr /var/cache/apk/*

USER ${USER_NAME}

ENV ANSIBLE_LOCAL_TEMP=/tmp/.ansible \
    ANSIBLE_CONFIG="./ansible.cfg" \
    PYTHON_DIR=/home/${USER_NAME}/.python \
    PYTHONPATH=:library:platform/entities/roles/resource/files/python:platform/entities/roles/application/files/python:platform/entities/roles/service/files/python:platform/entities/roles/flow/files/python:platform/entities/roles/executor/files/python:platform/entities/roles/skill/files/python:platform/entities/roles/sysentity/files/python:platform/entities/roles/software/files/python:platform/entities/roles/function/files/python:platform/entities/roles/helper/files/python:platform/entities/roles/builder/files/python:platform/entities/roles/library/files/python

RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    source /home/${USER_NAME}/.local/bin/env && \
    uv venv ${PYTHON_DIR} && \
    source ${PYTHON_DIR}/bin/activate && \
    uv pip install --no-cache --no-progress \
      ansible \
      ansible-core==2.15.13 \
      ansible-runner \
      ansible-vault \
      httplib2 \
      jmespath \
      jinja2 \
      lxml \
      opentelemetry-api opentelemetry-sdk opentelemetry-exporter-otlp \
      requests \
      six

ENV VIRTUAL_ENV=$PYTHON_DIR \
    PATH="${PYTHON_DIR}/bin:$PATH"
# TODO: remove overrides after ansible-core 2.19
COPY opentelemetry.py /home/${USER_NAME}/.python/lib/python3.12/site-packages/ansible_collections/community/general/plugins/callback/opentelemetry.py
COPY profile_roles.py /home/${USER_NAME}/.python/lib/python3.12/site-packages/ansible_collections/ansible/posix/plugins/callback/profile_roles.py
COPY profile_tasks.py /home/${USER_NAME}/.python/lib/python3.12/site-packages/ansible_collections/ansible/posix/plugins/callback/profile_tasks.py
COPY discovery.py /home/${USER_NAME}/.python/lib/python3.12/site-packages/ansible_collections/kubernetes/core/plugins/module_utils/client/discovery.py
