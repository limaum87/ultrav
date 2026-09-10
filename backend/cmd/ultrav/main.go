// Command ultrav is the backend entrypoint: config loading, provider
// selection and HTTP server bootstrap.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ultrav/ultrav/backend/internal/api"
	"github.com/ultrav/ultrav/backend/internal/auth"
	"github.com/ultrav/ultrav/backend/internal/config"
	"github.com/ultrav/ultrav/backend/internal/hypervisor"
	"github.com/ultrav/ultrav/backend/internal/hypervisor/libvirt"
	"github.com/ultrav/ultrav/backend/internal/hypervisor/mock"
	"github.com/ultrav/ultrav/backend/internal/iso"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	provider, err := newProvider(cfg.Provider, cfg)
	if err != nil {
		log.Error("failed to create hypervisor provider", "provider", cfg.Provider, "err", err)
		os.Exit(1)
	}
	log.Info("hypervisor provider selected", "provider", cfg.Provider, "uri", cfg.LibvirtURI)

	isos, err := iso.New(cfg.IsoDir)
	if err != nil {
		log.Error("failed to create ISO library", "dir", cfg.IsoDir, "err", err)
		os.Exit(1)
	}

	// Auth: SQLite user store + JWT bearer tokens (nil would disable auth).
	authn, err := auth.Open(cfg.DBPath, cfg.JWTSecret, log)
	if err != nil {
		log.Error("failed to open user database", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	defer authn.Close()
	if err := authn.BootstrapAdmin(cfg.AdminUser, cfg.AdminPassword, log); err != nil {
		log.Error("failed to bootstrap admin user", "err", err)
		os.Exit(1)
	}

	srv := api.NewServer(provider, isos, authn, log)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}


	// CORS is applied around the whole handler chain.
	handler := api.WithCORS(cfg.CORSOrigin, httpServer.Handler)
	httpServer.Handler = handler

	go func() {
		log.Info("ultrav listening", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

// newProvider instantiates the concrete HypervisorProvider. This is the only
// place in the codebase that knows the concrete implementations.
func newProvider(p config.Provider, cfg config.Config) (hypervisor.Provider, error) {
	switch p {
	case config.ProviderMock:
		return mock.New(), nil
	case config.ProviderLibvirt:
		return libvirt.New(cfg.LibvirtURI, cfg.IsoDir), nil
	}
	return nil, errors.New("unknown provider")
}
