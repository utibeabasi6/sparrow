#!/usr/bin/env bash
# Builds an uncompressed guest kernel (vmlinux) for Firecracker.
#
#   ./scripts/build-kernel.sh            # writes assets/vmlinux
#   KERNEL_VERSION=6.1.150 ./scripts/build-kernel.sh
#
# Needs: gcc make flex bison bc libelf-dev libssl-dev
set -euo pipefail

KERNEL_VERSION="${KERNEL_VERSION:-6.1.189}"
OUT="${OUT:-assets/vmlinux}"
WORKDIR="${WORKDIR:-build/kernel}"
CONFIG_URL="https://raw.githubusercontent.com/firecracker-microvm/firecracker/main/resources/guest_configs/microvm-kernel-ci-x86_64-6.1.config"

mkdir -p "$WORKDIR" "$(dirname "$OUT")"
OUT="$(realpath "$OUT")"
cd "$WORKDIR"

if [ ! -d "linux-$KERNEL_VERSION" ]; then
  echo "downloading linux $KERNEL_VERSION"
  curl -fsSL "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-$KERNEL_VERSION.tar.xz" | tar -xJ
fi
cd "linux-$KERNEL_VERSION"

# Firecracker's own CI config. There is no initramfs, so every driver the
# guest needs to find and mount its root disk must be =y, not =m.
curl -fsSL -o .config "$CONFIG_URL"
make olddefconfig

for opt in VIRTIO_MMIO VIRTIO_BLK VIRTIO_NET EXT4_FS SERIAL_8250_CONSOLE IP_PNP PCI; do
  grep -q "^CONFIG_$opt=y" .config || { echo "CONFIG_$opt is not =y" >&2; exit 1; }
done

make vmlinux -j"$(nproc)"
cp vmlinux "$OUT"
echo "kernel written to $OUT"
