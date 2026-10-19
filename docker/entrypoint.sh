#!/bin/sh
set -eu

role="${1:-metanode}"
if [ "$#" -gt 0 ]; then
    shift
fi

case "$role" in
    metanode)
        exec /usr/local/bin/dfsha-metanode "$@"
        ;;
    datanode)
        exec /usr/local/bin/dfsha-datanode "$@"
        ;;
    client)
        exec /usr/local/bin/dfsha-client "$@"
        ;;
    *)
        echo "Unknown DFSHA role: $role. Expected: metanode, datanode, or client." >&2
        exit 64
        ;;
esac
