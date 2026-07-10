#!/usr/bin/env sh
set -eu

status="$(supervisorctl -c /etc/supervisor/conf.d/oxygen.conf status)"

for process in octane queue webhooks scheduler go-queue go-live; do
    if ! printf '%s\n' "$status" | grep -Eq "^${process}[[:space:]]+RUNNING"; then
        printf '%s\n' "$status" >&2
        exit 1
    fi
done

curl -fsS http://127.0.0.1:8000/up >/dev/null
curl -fsS http://127.0.0.1:8081/readyz >/dev/null
