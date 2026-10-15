# sparrow

A small Go API and web UI that boots Firecracker microVMs. It is the demo
code for the talk "Don't Let Your AI Agents Run Wild" at GopherCon Africa.

A VM needs three things:

1. a kernel (`assets/vmlinux`)
2. a root filesystem (`assets/rootfs/claude.ext4`, an Alpine image with Claude Code)
3. a VMM, which is one `firecracker` process with an HTTP API on a unix socket

sparrow starts one `firecracker` process per VM and configures it with the
[firecracker-go-sdk](https://github.com/firecracker-microvm/firecracker-go-sdk).

## What you need

- Linux on x86_64 with `/dev/kvm`. Use bare metal or a cloud VM with nested
  virtualisation. macOS does not work.
- root, because sparrow creates tap devices and iptables rules
- Go, Docker, and the build tools for the kernel

On Ubuntu 24.04:

```bash
apt-get install -y build-essential flex bison bc libelf-dev libssl-dev docker.io e2fsprogs
```

Install Firecracker:

```bash
curl -fsSL https://github.com/firecracker-microvm/firecracker/releases/download/v1.17.0/firecracker-v1.17.0-x86_64.tgz | tar -xz -C /tmp
install /tmp/release-v1.17.0-x86_64/firecracker-v1.17.0-x86_64 /usr/local/bin/firecracker
```

The rootfs lets root log in over ssh with `~/.ssh/id_ed25519.pub`. Create
the key if you do not have one:

```bash
ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ed25519
```

## Build the kernel and the rootfs

```bash
make kernel
make rootfs
```

`make kernel` downloads Linux 6.1 and builds it with the config that
Firecracker uses in its own CI. It takes about 6 minutes on 4 CPUs.

`make rootfs` builds `images/claude/Dockerfile` and turns the image into an
ext4 file with `scripts/docker-to-rootfs.sh`. That script works with any
image, but the image must have an init. Alpine uses busybox init and openrc.

`sparrow.json` tells sparrow where to find the kernel and each rootfs:

```json
{
  "kernel": "assets/vmlinux",
  "rootfs": {
    "claude": "assets/rootfs/claude.ext4"
  }
}
```

## Boot one VM by hand

This uses only `firecracker` and `curl`. In terminal one:

```bash
rm -f /tmp/fc.sock && firecracker --api-sock /tmp/fc.sock
```

In terminal two:

```bash
./scripts/one-off-vm.sh
```

The boot log and a login prompt appear in terminal one. Log in as `root`
with no password. Run `reboot` to stop the VM.

## Run sparrow

```bash
go run ./cmd/sparrow
```

Open http://localhost:8080.

| Flag | Default | |
|---|---|---|
| `-config` | `sparrow.json` | the kernel and rootfs paths |
| `-listen` | `127.0.0.1:8080` | use `:8080` to listen on all interfaces |
| `-run-dir` | `/tmp/sparrow` | each VM's disk copy, API socket and console log |

The API has no authentication, so anyone who can reach the port can create
and delete VMs.

sparrow deletes the run directory at startup, to clear what a crash left
behind. It writes a `.sparrow` file into the directory, and it refuses to
start on a non-empty directory without that file.

The API:

| Method | Path | Body |
|---|---|---|
| `GET` | `/api/rootfs` | |
| `GET` | `/api/vms` | |
| `POST` | `/api/vms` | `{"rootfs": "claude", "vcpus": 1, "memory_mib": 512}` |
| `GET` | `/api/vms/{id}` | |
| `GET` | `/api/vms/{id}/console` | |
| `DELETE` | `/api/vms/{id}` | |

```bash
curl -X POST localhost:8080/api/vms -d '{"rootfs": "claude"}'
ssh root@172.16.0.2
```

Inside the VM, run `claude` and log in, or export `ANTHROPIC_API_KEY`.

## Disk copies

The guest writes to its disk, so each VM gets its own copy of the rootfs in
the run directory. How fast that copy is depends on the filesystem.

On ext4, `cp` makes a sparse copy. The 2 GB rootfs holds about 280 MB of
data, so each VM costs about 280 MB of disk and 0.17 s to copy.

On XFS or btrfs, `cp` makes a reflink. The copy shares every block with the
base file until the guest writes to it. It takes about 2 ms and uses no
extra disk. coreutils 9.0 and later try a reflink by default, so the code
does not change.

A reflink only works inside one filesystem. Put the base rootfs and the run
directory on the same XFS disk:

```bash
mkfs.xfs /dev/sdb          # erases the disk
mkdir -p /mnt/sparrow && mount /dev/sdb /mnt/sparrow
cp -r assets /mnt/sparrow/
```

Save a config that points at the copies as `/mnt/sparrow/sparrow.json`:

```json
{
  "kernel": "/mnt/sparrow/assets/vmlinux",
  "rootfs": {
    "claude": "/mnt/sparrow/assets/rootfs/claude.ext4"
  }
}
```

```bash
go run ./cmd/sparrow -config /mnt/sparrow/sparrow.json -run-dir /mnt/sparrow/run
```

Check that your filesystem supports reflinks with `xfs_info /mnt/sparrow`.
The output must contain `reflink=1`.

## Networking

Each VM gets its own tap device and its own /30 from `172.16.0.0/16`:

```
VM eth0 172.16.0.2  ── virtio-net ──  host tap sp-<id> 172.16.0.1
```

The guest gets its address from the `ip=` kernel argument, so no DHCP
server runs. When sparrow starts, it makes these changes on the host:

```bash
sysctl -w net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -s 172.16.0.0/16 -o eth0 -j MASQUERADE
iptables -I FORWARD 1 -i sp-+ -o eth0 -j ACCEPT
iptables -I FORWARD 2 -i eth0 -o sp-+ -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -I FORWARD 3 -i sp-+ -j DROP
```

The `FORWARD` rules are needed because Docker sets the `FORWARD` policy to
`DROP`. The last rule stops one VM from reaching another. sparrow removes
the iptables rules when it stops.

sparrow also adds a permanent ARP entry for the guest on each tap. sparrow
picks both MAC addresses, so the host does not need to ask. Without the
entry, the first connection to a new VM starts before the guest is up. Its
ARP request gets no answer, and Linux waits a second before it asks again.

sparrow sets the tap's MAC too. On Ubuntu 24.04, the system replaces a new
interface's random MAC a few milliseconds after the interface appears. A
MAC change deletes every ARP entry on that interface, the permanent one
included.

## Boot time

Measured on a 4 vCPU DigitalOcean droplet, for a 1 vCPU, 512 MiB guest,
one VM at a time. "To sshd" is the time from the create call until sshd in
the guest accepts a connection. The UI shows it as "Boot".

| Change | Create call | To sshd |
|---|---|---|
| first version | 0.20 s | 2.2 s |
| `quiet` on the kernel command line | 0.20 s | 1.2 s |
| permanent ARP entry for the guest | 0.20 s | 0.92 s |
| rootfs on XFS, so the copy is a reflink | 0.03 s | 0.83 s |

`quiet` matters because every byte on the serial console traps out of the
guest into Firecracker. A full kernel log is about 250 lines.
`scripts/one-off-vm.sh` leaves `quiet` out, so you can watch the full log.

Most of what is left is the guest: about 0.3 s for the kernel and about
0.3 s for openrc to start sshd.

## Layout

```
cmd/sparrow/            flags, startup and shutdown
internal/config/        reads sparrow.json
internal/network/       iptables setup, tap devices, the /30 pool
internal/vm/            creates and stops VMs with the firecracker SDK
internal/api/           the JSON API and the static UI
images/claude/          the Dockerfile for the rootfs
scripts/                kernel, rootfs, and the one-off VM
```

## What this does not do

sparrow runs Firecracker as root without the
[jailer](https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md).
In production, use the jailer. It puts each VMM in a chroot, as an
unprivileged user, inside a cgroup, with a seccomp filter.
