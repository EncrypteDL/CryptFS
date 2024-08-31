#!/bin/sh

[ -n "${PUID}" ] && usermod -u "${PUID}" CryptFS
[ -n "${PGID}" ] && groupmod -g "${PGID}" CryptFS

printf "Configuring dinof ..."
[ -z "${DATA}" ] && DATA="/data"

export DATA

printf "Switching UID=%s and GID=%s\n" "${PUID}" "${PGID}"
exec su-exec CryptFS:yCryptFS "$@"
