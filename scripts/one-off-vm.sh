#!/usr/bin/env bash
# Boots one VM by hand, with nothing but curl. This is slide 19.
#
# Terminal one:   rm -f /tmp/fc.sock && firecracker --api-sock /tmp/fc.sock
# Terminal two:   ./scripts/one-off-vm.sh
#
# The boot log and the login prompt appear in terminal one. Log in as root.
set -euo pipefail

KERNEL="${KERNEL:-assets/vmlinux}"
ROOTFS="${ROOTFS:-assets/rootfs/claude.ext4}"

# The guest writes to its disk, so give it a copy.
cp --sparse=always "$ROOTFS" /tmp/fc-rootfs.ext4

FC="curl -fsS --unix-socket /tmp/fc.sock -H Content-Type:application/json -X PUT"

$FC http://localhost/boot-source -d "{
    \"kernel_image_path\": \"$KERNEL\",
    \"boot_args\": \"console=ttyS0 reboot=k panic=1 pci=off\"
  }"

$FC http://localhost/drives/rootfs -d '{
    "drive_id": "rootfs",
    "path_on_host": "/tmp/fc-rootfs.ext4",
    "is_root_device": true,
    "is_read_only": false
  }'

$FC http://localhost/machine-config -d '{
    "vcpu_count": 1,
    "mem_size_mib": 128
  }'

$FC http://localhost/actions -d '{"action_type": "InstanceStart"}'
