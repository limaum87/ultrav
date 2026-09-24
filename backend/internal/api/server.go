// Package api contains the HTTP layer: routing, handlers and middlewares.
// Handlers only translate HTTP <-> domain/provider calls; they never talk to
// libvirt directly.
package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ultrav/ultrav/backend/internal/api/types"
	"github.com/ultrav/ultrav/backend/internal/auth"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
	"github.com/ultrav/ultrav/backend/internal/iso"
	"github.com/ultrav/ultrav/backend/internal/scheduler"
	"github.com/ultrav/ultrav/backend/internal/settings"
	"github.com/ultrav/ultrav/backend/internal/tasks"
)

//go:embed openapi.json
var openapiJSON embed.FS

// Server wires the HTTP API to a hypervisor.Provider.
type Server struct {
	provider hypervisor.Provider
	isos     *iso.Store
	tasks    *tasks.Manager
	authn    *auth.Service      // nil disables authentication (tests)
	settings *settings.Store    // nil disables persistence of runtime settings
	sched    *scheduler.Service // nil disables backup schedule endpoints
	log      *slog.Logger
	router   *http.ServeMux
}

// NewServer builds the API server. Pass a non-nil auth service to require
// bearer tokens on protected endpoints and a non-nil settings store to
// persist runtime settings (e.g. the ISO library directory override).
func NewServer(provider hypervisor.Provider, isos *iso.Store, authn *auth.Service, st *settings.Store, sched *scheduler.Service, log *slog.Logger) *Server {
	s := &Server{provider: provider, isos: isos, tasks: tasks.NewManager(log), authn: authn, settings: st, sched: sched, log: log, router: http.NewServeMux()}
	s.routes()
	return s
}

// vmIDPattern is an allowlist protecting against path traversal and injection.
var vmIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (s *Server) routes() {
	mux := s.router

	// Auth (login is public; /auth/me is protected)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v1/auth/me", s.handleGetCurrentUser)
	mux.HandleFunc("PUT /api/v1/auth/password", s.handleChangeOwnPassword)
	// User management (admin only)
	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("PUT /api/v1/users/{id}/password", s.requireAdmin(s.handleResetUserPassword))
	// Health
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/ready", s.handleReadiness)

	// Host
	mux.HandleFunc("GET /api/v1/host", s.handleGetHost)
	mux.HandleFunc("GET /api/v1/capabilities", s.handleGetCapabilities)

	// Virtual machines
	mux.HandleFunc("GET /api/v1/vms", s.handleListVMs)
	mux.HandleFunc("POST /api/v1/vms", s.handleCreateVM)
	mux.HandleFunc("GET /api/v1/vms/{id}", s.requireValidVMID(s.handleGetVM))
	mux.HandleFunc("PATCH /api/v1/vms/{id}", s.requireValidVMID(s.handleUpdateVM))
	mux.HandleFunc("DELETE /api/v1/vms/{id}", s.requireValidVMID(s.handleDeleteVM))
	mux.HandleFunc("POST /api/v1/vms/{id}/start", s.requireValidVMID(s.handleStartVM))
	mux.HandleFunc("POST /api/v1/vms/{id}/shutdown", s.requireValidVMID(s.handleShutdownVM))
	mux.HandleFunc("POST /api/v1/vms/{id}/reboot", s.requireValidVMID(s.handleRebootVM))
	mux.HandleFunc("POST /api/v1/vms/{id}/stop", s.requireValidVMID(s.handleForceStopVM))
	// WebSocket (noVNC) — proxied to the VM's graphical console.
	mux.HandleFunc("GET /api/v1/vms/{id}/console", s.requireValidVMID(s.handleVMConsole))

	// Backup
	mux.HandleFunc("GET /api/v1/vms/{id}/backups", s.requireValidVMID(s.handleListVMBackups))
	mux.HandleFunc("POST /api/v1/vms/{id}/backups", s.requireValidVMID(s.handleCreateVMBackup))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", s.requireValidBackupID(s.handleDeleteBackup))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", s.requireValidBackupID(s.handleRestoreBackup))
	mux.HandleFunc("GET /api/v1/vms/{id}/config-export", s.requireValidVMID(s.handleExportVMConfig))

	// Backup schedules
	mux.HandleFunc("GET /api/v1/backup-schedules", s.handleListBackupSchedules)
	mux.HandleFunc("POST /api/v1/backup-schedules", s.handleCreateBackupSchedule)
	mux.HandleFunc("PUT /api/v1/backup-schedules/{id}", s.requireValidScheduleID(s.handleUpdateBackupSchedule))
	mux.HandleFunc("DELETE /api/v1/backup-schedules/{id}", s.requireValidScheduleID(s.handleDeleteBackupSchedule))

	// Storage pools
	mux.HandleFunc("GET /api/v1/storage/pools", s.handleListStoragePools)
	mux.HandleFunc("POST /api/v1/storage/pools", s.handleCreateStoragePool)
	mux.HandleFunc("GET /api/v1/storage/pools/{id}", s.requireValidVMID(s.handleGetStoragePool))
	mux.HandleFunc("DELETE /api/v1/storage/pools/{id}", s.requireValidVMID(s.handleDeleteStoragePool))
	mux.HandleFunc("POST /api/v1/storage/pools/{id}/refresh", s.requireValidVMID(s.handleRefreshStoragePool))

	// ISO library
	mux.HandleFunc("GET /api/v1/storage/isos", s.handleListIsos)
	mux.HandleFunc("POST /api/v1/storage/isos", s.handleUploadIso)
	mux.HandleFunc("DELETE /api/v1/storage/isos/{id}", s.requireValidVMID(s.handleDeleteIso))

	// Networks
	mux.HandleFunc("GET /api/v1/networks", s.handleListNetworks)
	mux.HandleFunc("POST /api/v1/networks", s.handleCreateNetwork)
	mux.HandleFunc("GET /api/v1/networks/{id}", s.requireValidVMID(s.handleGetNetwork))
	mux.HandleFunc("POST /api/v1/networks/{id}/start", s.requireValidVMID(s.handleStartNetwork))
	mux.HandleFunc("POST /api/v1/networks/{id}/stop", s.requireValidVMID(s.handleStopNetwork))
	mux.HandleFunc("PUT /api/v1/networks/{id}", s.requireValidVMID(s.handleUpdateNetwork))
	mux.HandleFunc("DELETE /api/v1/networks/{id}", s.requireValidVMID(s.handleDeleteNetwork))
	mux.HandleFunc("GET /api/v1/host/bridges", s.handleListHostBridges)

	// Tasks (async job system)
	mux.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.requireValidTaskID(s.handleGetTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.requireValidTaskID(s.handleCancelTask))

	// Contract & docs
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	mux.HandleFunc("GET /docs", s.handleDocs)
	mux.HandleFunc("GET /", s.handleNotFound)
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	return withSecurityHeaders(withRecovery(s.log, withRequestID(withLogging(s.log, s.withAuth(s.router)))))
}

// --- health ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, types.Health{Status: types.Ok})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if err := s.provider.Ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, types.Readiness{
			Status:     types.ReadinessStatusNotReady,
			Hypervisor: types.ReadinessHypervisorUnavailable,
		})
		return
	}
	writeJSON(w, http.StatusOK, types.Readiness{
		Status:     types.ReadinessStatusReady,
		Hypervisor: types.ReadinessHypervisorReady,
	})
}

// --- host ---

func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	host, err := s.provider.GetHost(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, host)
}

func (s *Server) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	caps, err := s.provider.GetCapabilities(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, caps)
}

// --- virtual machines ---

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	vms, err := s.provider.ListVirtualMachines(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, types.VirtualMachineList{Items: vms, Total: len(vms)})
}

func (s *Server) handleGetVM(w http.ResponseWriter, r *http.Request) {
	vm, err := s.provider.GetVirtualMachine(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

// handleCreateVM validates the request and starts the creation as a
// background task (Job System v1): the caller polls GET /tasks/{id}.
func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	var req types.VirtualMachineCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if msg := validateCreateVM(&req); msg != "" {
		s.writeError(w, r, CodeValidationError, msg)
		return
	}
	task, err := s.tasks.Start(tasks.TypeVMCreate, req.Name, req.Name, func(ctx context.Context, rep *tasks.Reporter) error {
		rep.SetMessage("creating virtual machine " + req.Name)
		vm, err := s.provider.CreateVirtualMachine(ctx, req)
		if err != nil {
			return err
		}
		rep.SetResourceID(vm.Id)
		rep.SetProgress(100)
		rep.SetMessage("virtual machine " + req.Name + " created")
		if vm.Warnings != nil {
			for _, w := range *vm.Warnings {
				rep.AddWarning(w)
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("failed to start vm-create task", "err", err)
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

// validateCreateVM enforces the contract constraints not covered by the
// pattern-validated path parameters (body has no generated validation).
func validateCreateVM(req *types.VirtualMachineCreate) string {
	if !vmIDPattern.MatchString(req.Name) {
		return "Invalid virtual machine name"
	}
	if req.Vcpus < 1 || req.Vcpus > 64 {
		return "vcpus must be between 1 and 64"
	}
	if req.MemoryBytes < 16*1024*1024 {
		return "memoryBytes must be at least 16 MiB"
	}
	if req.Disk.PoolId == "" {
		return "disk.poolId is required"
	}
	if req.Disk.SizeBytes < 1024*1024 {
		return "disk.sizeBytes must be at least 1 MiB"
	}
	return ""
}

// handleUpdateVM applies a partial settings update (vcpus, memoryBytes,
// isoId) to a virtual machine.
func (s *Server) handleUpdateVM(w http.ResponseWriter, r *http.Request) {
	var req types.VirtualMachineUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if msg := validateUpdateVM(&req); msg != "" {
		s.writeError(w, r, CodeValidationError, msg)
		return
	}
	vm, err := s.provider.UpdateVirtualMachine(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

// handleDeleteVM removes a virtual machine. deleteDisks=true also deletes
// the disk volumes; otherwise only the domain definition is removed.
func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	deleteDisks := false
	switch v := r.URL.Query().Get("deleteDisks"); v {
	case "", "false":
	case "true":
		deleteDisks = true
	default:
		s.writeError(w, r, CodeValidationError, "deleteDisks must be 'true' or 'false'")
		return
	}
	if err := s.provider.DeleteVirtualMachine(r.Context(), r.PathValue("id"), deleteDisks); err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateUpdateVM enforces the contract constraints for PATCH /vms/{id}.
func validateUpdateVM(req *types.VirtualMachineUpdate) string {
	if req.Vcpus != nil && (*req.Vcpus < 1 || *req.Vcpus > 64) {
		return "vcpus must be between 1 and 64"
	}
	if req.MemoryBytes != nil && *req.MemoryBytes < 16*1024*1024 {
		return "memoryBytes must be at least 16 MiB"
	}
	if req.IsoId != nil && *req.IsoId != "" && !iso.ValidID.MatchString(*req.IsoId) {
		return "isoId must be a valid ISO filename"
	}
	return ""
}

// vmAction adapts a power operation to a handler.
func (s *Server) vmAction(
	fn func(ctx context.Context, id string) (types.VirtualMachine, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vm, err := fn(r.Context(), r.PathValue("id"))
		if err != nil {
			s.writeProviderError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	}
}

func (s *Server) requireValidVMID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !vmIDPattern.MatchString(id) {
			s.writeError(w, r, CodeValidationError, "Invalid virtual machine identifier")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleStartVM(w http.ResponseWriter, r *http.Request) {
	s.vmAction(s.provider.StartVirtualMachine)(w, r)
}
func (s *Server) handleShutdownVM(w http.ResponseWriter, r *http.Request) {
	s.vmAction(s.provider.ShutdownVirtualMachine)(w, r)
}
func (s *Server) handleRebootVM(w http.ResponseWriter, r *http.Request) {
	s.vmAction(s.provider.RebootVirtualMachine)(w, r)
}
func (s *Server) handleForceStopVM(w http.ResponseWriter, r *http.Request) {
	s.vmAction(s.provider.ForceStopVirtualMachine)(w, r)
}

// --- storage pools ---

func (s *Server) handleListStoragePools(w http.ResponseWriter, r *http.Request) {
	pools, err := s.provider.ListStoragePools(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, types.StoragePoolList{Items: pools, Total: len(pools)})
}

// handleCreateStoragePool validates and creates a directory storage pool.
func (s *Server) handleCreateStoragePool(w http.ResponseWriter, r *http.Request) {
	var req types.StoragePoolCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if msg := validateCreateStoragePool(&req); msg != "" {
		s.writeError(w, r, CodeValidationError, msg)
		return
	}
	pool, err := s.provider.CreateStoragePool(r.Context(), req)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	// Optional: promote the pool's directory to be the ISO library, so an
	// existing folder full of .iso files can be reused without re-uploading.
	if req.IsoLibrary != nil && *req.IsoLibrary {
		if err := s.isos.SetDir(req.TargetPath); err != nil {
			s.writeError(w, r, CodeInternalError, "pool created, but failed to use its directory as the ISO library: "+err.Error())
			return
		}
		if s.settings != nil {
			if err := s.settings.SetIsoDir(req.TargetPath); err != nil {
				s.log.Warn("failed to persist ISO library directory override", "dir", req.TargetPath, "err", err)
			}
		}
		s.log.Info("ISO library directory changed", "dir", req.TargetPath)
	}
	writeJSON(w, http.StatusCreated, pool)
}

var absPathPattern = regexp.MustCompile(`^/[a-zA-Z0-9._/-]{0,254}$`)

func validateCreateStoragePool(req *types.StoragePoolCreate) string {
	if !vmIDPattern.MatchString(req.Name) {
		return "Invalid storage pool name"
	}
	if !absPathPattern.MatchString(req.TargetPath) || strings.Contains(req.TargetPath, "..") {
		return "targetPath must be an absolute path without '..'"
	}
	if req.Type != nil && *req.Type != "" && *req.Type != types.StoragePoolCreateTypeDir {
		return "only 'dir' storage pools are supported"
	}
	return ""
}

// handleDeleteStoragePool removes a pool from the listing (undefine only).
// Volumes, disk images and files on disk are never deleted.
func (s *Server) handleDeleteStoragePool(w http.ResponseWriter, r *http.Request) {
	if err := s.provider.DeleteStoragePool(r.Context(), r.PathValue("id")); err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetStoragePool(w http.ResponseWriter, r *http.Request) {
	pool, err := s.provider.GetStoragePool(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

func (s *Server) handleRefreshStoragePool(w http.ResponseWriter, r *http.Request) {
	pool, err := s.provider.RefreshStoragePool(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

// --- iso library ---

func (s *Server) handleListIsos(w http.ResponseWriter, r *http.Request) {
	isos, err := s.isos.List()
	if err != nil {
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	writeJSON(w, http.StatusOK, types.IsoList{Items: isos, Total: len(isos)})
}

// handleUploadIso streams a multipart upload into the ISO library.
func (s *Server) handleUploadIso(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, r, CodeValidationError, "Missing multipart field 'file'")
		return
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if !iso.ValidID.MatchString(name) {
		s.writeError(w, r, CodeValidationError, "Filename must end in .iso (letters, digits, dot, dash, underscore, space)")
		return
	}

	img, err := s.isos.Create(name, file)
	if err != nil {
		switch {
		case errors.Is(err, iso.ErrAlreadyExists):
			s.writeError(w, r, CodeIsoAlreadyExists, "An ISO image with this filename already exists")
		case errors.Is(err, os.ErrInvalid):
			s.writeError(w, r, CodeValidationError, "Invalid ISO filename")
		default:
			s.writeError(w, r, CodeInternalError, "")
		}
		return
	}
	writeJSON(w, http.StatusCreated, img)
}

func (s *Server) handleDeleteIso(w http.ResponseWriter, r *http.Request) {
	if err := s.isos.Delete(r.PathValue("id")); err != nil {
		switch {
		case errors.Is(err, iso.ErrNotFound):
			s.writeError(w, r, CodeIsoNotFound, "ISO image was not found")
		case errors.Is(err, os.ErrInvalid):
			s.writeError(w, r, CodeValidationError, "Invalid ISO filename")
		default:
			s.writeError(w, r, CodeInternalError, "")
		}
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// --- networks ---

func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	nets, err := s.provider.ListNetworks(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, types.NetworkList{Items: nets, Total: len(nets)})
}

// handleCreateNetwork validates and creates a virtual network (nat, bridge
// or isolated).
func (s *Server) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req types.NetworkCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if msg := validateCreateNetwork(&req); msg != "" {
		s.writeError(w, r, CodeValidationError, msg)
		return
	}
	netw, err := s.provider.CreateNetwork(r.Context(), req)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, netw)
}

var cidrPattern = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[12][0-9]|3[0-2])$`)

func validateCreateNetwork(req *types.NetworkCreate) string {
	if !vmIDPattern.MatchString(req.Name) {
		return "Invalid network name"
	}
	switch req.Mode {
	case types.NetworkCreateModeNat, types.NetworkCreateModeIsolated:
		if req.Cidr == nil || !cidrPattern.MatchString(*req.Cidr) {
			return "cidr is required for nat/isolated networks (e.g. 192.168.100.0/24)"
		}
		if _, _, err := net.ParseCIDR(*req.Cidr); err != nil {
			return "cidr is not a valid subnet"
		}
	case types.NetworkCreateModeBridge:
		if req.BridgeName == nil || !vmIDPattern.MatchString(*req.BridgeName) {
			return "bridgeName is required for bridge networks and must be a valid interface name"
		}
	default:
		return "mode must be one of: nat, bridge, isolated"
	}
	return ""
}

// handleListHostBridges lists host Linux bridges available for bridge-mode
// networks (host config; UltraV only detects them).
func (s *Server) handleListHostBridges(w http.ResponseWriter, r *http.Request) {
	bridges, err := s.provider.ListHostBridges(r.Context())
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, types.HostBridgeList{Items: bridges, Total: len(bridges)})
}

func (s *Server) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	net, err := s.provider.GetNetwork(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, net)
}

func (s *Server) handleStartNetwork(w http.ResponseWriter, r *http.Request) {
	net, err := s.provider.StartNetwork(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, net)
}

func (s *Server) handleStopNetwork(w http.ResponseWriter, r *http.Request) {
	net, err := s.provider.StopNetwork(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, net)
}

// handleUpdateNetwork applies a partial update to a virtual network.
func (s *Server) handleUpdateNetwork(w http.ResponseWriter, r *http.Request) {
	var req types.NetworkUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, CodeValidationError, "Invalid JSON body")
		return
	}
	if msg := validateUpdateNetwork(&req); msg != "" {
		s.writeError(w, r, CodeValidationError, msg)
		return
	}
	net, err := s.provider.UpdateNetwork(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, net)
}

func validateUpdateNetwork(req *types.NetworkUpdate) string {
	if req.Cidr != nil && (!cidrPattern.MatchString(*req.Cidr) || func() bool { _, _, err := net.ParseCIDR(*req.Cidr); return err != nil }()) {
		return "cidr is not a valid subnet (e.g. 192.168.100.0/24)"
	}
	if req.BridgeName != nil && !vmIDPattern.MatchString(*req.BridgeName) {
		return "bridgeName must be a valid interface name"
	}
	if req.Mode == nil {
		return ""
	}
	switch *req.Mode {
	case types.Nat, types.Isolated:
	case types.Bridge:
	default:
		return "mode must be one of: nat, bridge, isolated"
	}
	return ""
}

// handleDeleteNetwork undefines an inactive virtual network.
func (s *Server) handleDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := s.provider.DeleteNetwork(r.Context(), r.PathValue("id")); err != nil {
		s.writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- contract & docs ---

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	b, err := openapiJSON.ReadFile("openapi.json")
	if err != nil {
		s.log.Error("openapi.json missing", "err", err)
		s.writeError(w, r, CodeInternalError, "")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// handleDocs serves Swagger UI from the CDN pointed at our own openapi.json.
func (s *Server) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>UltraV API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = () => window.ui = SwaggerUIBundle({
      url: '/openapi.json',
      dom_id: '#swagger-ui',
      deepLinking: true,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, CodeNotFound, "Resource not found")
}
