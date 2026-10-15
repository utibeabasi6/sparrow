package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/utibeabasi6/sparrow/internal/api"
	"github.com/utibeabasi6/sparrow/internal/config"
	"github.com/utibeabasi6/sparrow/internal/network"
	"github.com/utibeabasi6/sparrow/internal/vm"
)

func main() {
	configFile := flag.String("config", config.DefaultFile, "path to the rootfs config")
	listen := flag.String("listen", "127.0.0.1:8080", "address for the API and UI")
	runDir := flag.String("run-dir", vm.DefaultRunDir, "directory for VM disks and consoles")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *configFile, *listen, *runDir, log); err != nil {
		log.Error("sparrow failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configFile, listen, runDir string, log *slog.Logger) error {
	// Taps and iptables rules belong to the whole host. A second sparrow would
	// delete the first one's taps at startup and its rules at exit.
	lock, err := os.OpenFile("/run/sparrow.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another sparrow is already running")
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}

	n, err := network.Setup()
	if err != nil {
		return err
	}
	defer func() {
		if err := n.Close(); err != nil {
			log.Warn("removing iptables rules", "error", err)
		}
	}()
	log.Info("network ready", "subnet", network.Subnet, "egress", n.Egress())

	vms, err := vm.NewManager(cfg.Kernel, cfg.Rootfs, runDir, n, log)
	if err != nil {
		return err
	}
	defer vms.Close()

	srv := &http.Server{
		Addr:              listen,
		Handler:           api.New(vms, cfg.Rootfs, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("sparrow listening", "addr", listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down, stopping all vms")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
