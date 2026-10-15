package api

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"slices"

	"github.com/utibeabasi6/sparrow/internal/network"
	"github.com/utibeabasi6/sparrow/internal/vm"
)

//go:embed ui
var ui embed.FS

type Server struct {
	vms    *vm.Manager
	rootfs []string
	log    *slog.Logger
}

// New serves the JSON API under /api and the UI from /.
func New(vms *vm.Manager, rootfs map[string]string, log *slog.Logger) *Server {
	return &Server{
		vms:    vms,
		rootfs: slices.Sorted(maps.Keys(rootfs)),
		log:    log,
	}
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(ui, "ui")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rootfs", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, http.StatusOK, s.rootfs)
	})
	mux.HandleFunc("GET /api/vms", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, http.StatusOK, s.vms.List())
	})
	mux.HandleFunc("POST /api/vms", s.createVM)
	mux.HandleFunc("GET /api/vms/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.vms.Get(r.PathValue("id"))
		if err != nil {
			s.error(w, err)
			return
		}
		s.json(w, http.StatusOK, v)
	})
	mux.HandleFunc("GET /api/vms/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.vms.Console(r.PathValue("id"))
		if err != nil {
			s.error(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(out)
	})
	mux.HandleFunc("DELETE /api/vms/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.vms.Delete(r.PathValue("id")); err != nil {
			s.error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("GET /", http.FileServerFS(static))
	return mux
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	req := struct {
		Rootfs    string `json:"rootfs"`
		VCPUs     int64  `json:"vcpus"`
		MemoryMiB int64  `json:"memory_mib"`
	}{VCPUs: 1, MemoryMiB: 512}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.json(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	v, err := s.vms.Create(req.Rootfs, req.VCPUs, req.MemoryMiB)
	if err != nil {
		s.error(w, err)
		return
	}
	s.json(w, http.StatusCreated, v)
}

func (s *Server) error(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, vm.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, vm.ErrUnknownRootfs), errors.Is(err, vm.ErrInvalidSize):
		status = http.StatusBadRequest
	case errors.Is(err, network.ErrPoolExhausted):
		status = http.StatusServiceUnavailable
	default:
		s.log.Error("request failed", "error", err)
	}
	s.json(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) json(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Warn("writing response", "error", err)
	}
}
