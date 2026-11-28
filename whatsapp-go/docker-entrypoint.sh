#!/bin/sh
# Railway (and most hosts) mount the volume owned by root: give /data to the
# askmywa user, then drop root before anything reads the session.
set -eu
chown -R askmywa:askmywa /data
chmod 700 /data
exec su-exec askmywa "$@"
