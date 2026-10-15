#!/usr/bin/env bash
# Turns a Docker image into an ext4 file that Firecracker can boot as /dev/vda.
#
#   ./scripts/docker-to-rootfs.sh sparrow-claude assets/rootfs/claude.ext4 2G
#
# Set SSH_KEY to a public key to let root log in over ssh.
# Run as root so file owners in the image survive.
set -euo pipefail

IMAGE="$1"
OUT="$2"
SIZE="${3:-1G}"

dir="$(mktemp -d)"
container=""
trap 'rm -rf "$dir"; [ -z "$container" ] || docker rm -f "$container" >/dev/null' EXIT

container="$(docker create "$IMAGE")"
docker export "$container" | tar -x -C "$dir"

# Docker mounts its own resolv.conf and hosts at run time, so the exported
# copies are empty. openrc reads /.dockerenv and skips services it thinks a
# container does not need.
rm -f "$dir/.dockerenv"
echo "127.0.0.1 localhost" > "$dir/etc/hosts"
echo "nameserver 1.1.1.1" > "$dir/etc/resolv.conf"

if [ -n "${SSH_KEY:-}" ]; then
  install -D -m 600 "$SSH_KEY" "$dir/root/.ssh/authorized_keys"
fi

mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
truncate -s "$SIZE" "$OUT"
mkfs.ext4 -q -F -d "$dir" "$OUT"
echo "rootfs written to $OUT"
