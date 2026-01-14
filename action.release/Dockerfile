FROM alpine:3.20

ARG GIT_AUTHOR_NAME
ARG GIT_AUTHOR_EMAIL

ENV GIT_CONFIG_GLOBAL="/.gitconfig"
RUN apk upgrade --update-cache -a && apk add \
    bash \
    git \
    git-cliff && \
    rm -fr /var/cache/apk/* && \
    git config --global user.name "${GIT_AUTHOR_NAME}" && \
    git config --global user.email "${GIT_AUTHOR_EMAIL}"