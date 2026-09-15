#!/bin/sh
# Fallback path: build the image on the docker host under the same name the stack
# pulls, for when CI cannot publish it (wb-image.yml red, ghcr unreachable, no
# credential in Portainer). Normally the image comes from ghcr and the host only pulls.
#
# Run this from a checkout of the ref you want to deploy, then deploy the stack with
# PULL_POLICY=never so it uses this local image instead of pulling the tag.
#
# The host is slow (2 CPUs): expect several minutes. That is exactly why the stack
# does not build during a deploy — the Portainer request dies on the proxy timeout.
set -eu
cd "$(dirname "$0")/../.."
IMAGE=${IMAGE:-ghcr.io/wb-aleksandr-khlebnikov/tg-spam:master}
docker build -t "$IMAGE" .
echo "built $IMAGE - deploy the stack with PULL_POLICY=never"
