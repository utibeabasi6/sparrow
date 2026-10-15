package vm

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"

	"github.com/utibeabasi6/sparrow/internal/network"
)

const (
	// quiet halves the boot time. Every byte on the serial console traps
	// out to Firecracker, and a full kernel log is about 250 lines.
	bootArgs = "console=ttyS0 reboot=k panic=1 pci=off quiet"

	DefaultRunDir = "/tmp/sparrow"
	runDirMarker  = ".sparrow"
)

var (
	ErrNotFound      = errors.New("vm not found")
	ErrUnknownRootfs = errors.New("unknown rootfs")
	ErrInvalidSize   = errors.New("vcpus must be 1-32 and memory_mib 128-8192")
)

type State string

const (
	StateBooting State = "booting"
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type VM struct {
	ID        string    `json:"id"`
	Rootfs    string    `json:"rootfs"`
	VCPUs     int64     `json:"vcpus"`
	MemoryMiB int64     `json:"memory_mib"`
	State     State     `json:"state"`
	Tap       string    `json:"tap"`
	GuestIP   string    `json:"guest_ip"`
	CreatedAt time.Time `json:"created_at"`
	// BootMS is the time from the create call until sshd in the guest
	// accepts a connection.
	BootMS int64 `json:"boot_ms,omitempty"`

	machine *firecracker.Machine
	stopped chan struct{}
}

type Manager struct {
	kernel string
	rootfs map[string]string
	runDir string
	net    *network.Network
	log    *slog.Logger
	sdkLog *logrus.Entry

	mu  sync.Mutex
	vms map[string]*VM
	wg  sync.WaitGroup
}

// NewManager keeps each VM's disk and console in runDir. It deletes runDir at
// startup to clear what a crash left behind, so it refuses a non-empty
// directory that it did not create.
func NewManager(kernel string, rootfs map[string]string, runDir string, n *network.Network, log *slog.Logger) (*Manager, error) {
	entries, err := os.ReadDir(runDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(runDir, runDirMarker)); err != nil {
			return nil, fmt.Errorf("%s is not empty and sparrow did not create it", runDir)
		}
	}
	if err := os.RemoveAll(runDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, runDirMarker), nil, 0o644); err != nil {
		return nil, err
	}
	sdkLog := logrus.New()
	sdkLog.SetLevel(logrus.ErrorLevel)
	return &Manager{
		kernel: kernel,
		rootfs: rootfs,
		runDir: runDir,
		net:    n,
		log:    log,
		sdkLog: logrus.NewEntry(sdkLog),
		vms:    make(map[string]*VM),
	}, nil
}

func (m *Manager) Create(rootfs string, vcpus, memoryMiB int64) (VM, error) {
	base, ok := m.rootfs[rootfs]
	if !ok {
		return VM{}, fmt.Errorf("%w: %q", ErrUnknownRootfs, rootfs)
	}
	if vcpus < 1 || vcpus > 32 || memoryMiB < 128 || memoryMiB > 8192 {
		return VM{}, ErrInvalidSize
	}

	start := time.Now()
	id := strings.ToLower(rand.Text()[:8])
	vm := &VM{
		ID:        id,
		Rootfs:    rootfs,
		VCPUs:     vcpus,
		MemoryMiB: memoryMiB,
		State:     StateBooting,
		Tap:       network.TapPrefix + id,
		CreatedAt: start,
		stopped:   make(chan struct{}),
	}

	// Everything the VM holds goes on this stack. A failed create unwinds it
	// here, and a stopped VM unwinds it in release.
	var undo []func()
	fail := func(err error) (VM, error) {
		for _, f := range slices.Backward(undo) {
			f()
		}
		return VM{}, err
	}

	lease, err := m.net.Allocate()
	if err != nil {
		return fail(err)
	}
	undo = append(undo, func() { m.net.Release(lease) })
	vm.GuestIP = lease.Guest.String()

	if err := network.CreateTap(vm.Tap, lease); err != nil {
		return fail(err)
	}
	undo = append(undo, func() {
		if err := network.DeleteTap(vm.Tap); err != nil {
			m.log.Warn("deleting tap", "vm", id, "tap", vm.Tap, "error", err)
		}
	})

	dir := filepath.Join(m.runDir, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = os.RemoveAll(dir) })

	disk := filepath.Join(dir, "rootfs.ext4")
	if out, err := exec.Command("cp", "--sparse=always", base, disk).CombinedOutput(); err != nil {
		return fail(fmt.Errorf("copying rootfs: %w: %s", err, out))
	}

	console, err := os.Create(m.consolePath(id))
	if err != nil {
		return fail(err)
	}
	defer console.Close()

	cfg := firecracker.Config{
		VMID:            id,
		SocketPath:      filepath.Join(dir, "firecracker.sock"),
		KernelImagePath: m.kernel,
		KernelArgs:      bootArgs,
		Drives: []models.Drive{{
			DriveID:      firecracker.String("rootfs"),
			PathOnHost:   firecracker.String(disk),
			IsRootDevice: firecracker.Bool(true),
			IsReadOnly:   firecracker.Bool(false),
		}},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(vcpus),
			MemSizeMib: firecracker.Int64(memoryMiB),
		},
		// The SDK turns IPConfiguration into the ip= boot argument.
		NetworkInterfaces: []firecracker.NetworkInterface{{
			StaticConfiguration: &firecracker.StaticNetworkConfiguration{
				MacAddress:  lease.GuestMAC().String(),
				HostDevName: vm.Tap,
				IPConfiguration: &firecracker.IPConfiguration{
					IPAddr:  net.IPNet{IP: lease.Guest, Mask: network.Mask},
					Gateway: lease.Host,
					IfName:  "eth0",
				},
			},
		}},
		ForwardSignals: []os.Signal{},
	}

	// The SDK kills firecracker when this context ends, so it must outlive
	// the HTTP request that created the VM.
	ctx := context.Background()
	cmd := firecracker.VMCommandBuilder{}.
		WithBin("firecracker").
		WithSocketPath(cfg.SocketPath).
		WithStdout(console).
		WithStderr(console).
		Build(ctx)
	machine, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd), firecracker.WithLogger(m.sdkLog))
	if err == nil {
		err = machine.Start(ctx)
	}
	if err != nil {
		if machine != nil {
			_ = machine.StopVMM()
		}
		out, _ := os.ReadFile(m.consolePath(id))
		_ = os.Remove(m.consolePath(id))
		return fail(fmt.Errorf("starting firecracker: %w: %s", err, bytes.TrimSpace(out)))
	}
	vm.machine = machine

	m.mu.Lock()
	m.vms[id] = vm
	snapshot := *vm
	m.mu.Unlock()

	m.wg.Add(2)
	go m.release(vm, undo)
	go m.waitForSSH(vm, start)

	m.log.Info("vm started", "vm", id, "rootfs", rootfs, "ip", vm.GuestIP, "tap", vm.Tap)
	return snapshot, nil
}

func (m *Manager) release(vm *VM, undo []func()) {
	defer m.wg.Done()
	_ = vm.machine.Wait(context.Background())
	for _, f := range slices.Backward(undo) {
		f()
	}

	m.mu.Lock()
	vm.State = StateStopped
	m.mu.Unlock()
	close(vm.stopped)
	m.log.Info("vm stopped", "vm", vm.ID)
}

func (m *Manager) waitForSSH(vm *VM, start time.Time) {
	defer m.wg.Done()
	deadline := time.After(time.Minute)
	answered := false
wait:
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(vm.GuestIP, "22"), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			answered = true
			break
		}

		select {
		case <-vm.stopped:
			return
		case <-deadline:
			m.log.Warn("sshd did not answer within a minute", "vm", vm.ID)
			break wait
		case <-time.After(10 * time.Millisecond):
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if vm.State == StateBooting {
		vm.State = StateRunning
		if answered {
			vm.BootMS = time.Since(start).Milliseconds()
		}
	}
}

func (m *Manager) List() []VM {
	m.mu.Lock()
	defer m.mu.Unlock()
	vms := make([]VM, 0, len(m.vms))
	for _, vm := range m.vms {
		vms = append(vms, *vm)
	}
	slices.SortFunc(vms, func(a, b VM) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return vms
}

func (m *Manager) Get(id string) (VM, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vm, ok := m.vms[id]
	if !ok {
		return VM{}, ErrNotFound
	}
	return *vm, nil
}

// Console returns the last 64 KiB of the guest's serial console.
func (m *Manager) Console(id string) ([]byte, error) {
	if _, err := m.Get(id); err != nil {
		return nil, err
	}
	f, err := os.Open(m.consolePath(id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const tail = 64 << 10
	if info, err := f.Stat(); err == nil && info.Size() > tail {
		if _, err := f.Seek(-tail, io.SeekEnd); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	vm, ok := m.vms[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}

	// The VM stays in the map until it has stopped, so Close still sees it if
	// StopVMM fails.
	if err := vm.machine.StopVMM(); err != nil {
		return err
	}
	<-vm.stopped

	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()
	m.log.Info("vm deleted", "vm", id)
	if err := os.Remove(m.consolePath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Close stops every VM and waits until each one has released its tap,
// address and disk.
func (m *Manager) Close() {
	m.mu.Lock()
	vms := slices.Collect(maps.Values(m.vms))
	m.mu.Unlock()
	for _, vm := range vms {
		if err := vm.machine.StopVMM(); err != nil {
			m.log.Warn("stopping vm", "vm", vm.ID, "error", err)
		}
	}
	m.wg.Wait()
}

func (m *Manager) consolePath(id string) string {
	return filepath.Join(m.runDir, id+".log")
}
