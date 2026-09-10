// Package config holds runtime configuration loaded from the environment.
package config

import (
	"fmt"
	"os"
)

// Provider identifies a HypervisorProvider implementation.
type Provider string

const (
	ProviderMock    Provider = "mock"
	ProviderLibvirt Provider = "libvirt" // implemented in a future milestone
)

// Config is the application runtime configuration.
type Config struct {
	// Port is the HTTP listen port.
	Port string
	// Provider selects the HypervisorProvider implementation.
	Provider Provider
	// CORSOrigin is the allowed browser origin ("" disables CORS handling).
	CORSOrigin string
	// LibvirtURI is the libvirt connection URI used by the libvirt provider
	// (e.g. qemu:///system, qemu+ssh://host/system).
	LibvirtURI string
	// IsoDir is the directory backing the ISO library.
	IsoDir string
	// DBPath is the SQLite database file for users.
	DBPath string
	// JWTSecret signs bearer tokens; empty = ephemeral per-process secret.
	JWTSecret string
	// AdminUser / AdminPassword bootstrap the first user (empty DB only).
	AdminUser     string
	AdminPassword string
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		Port:       envOr("ULTRAV_PORT", "8080"),
		Provider:   Provider(envOr("HYPERVISOR_PROVIDER", string(ProviderMock))),
		CORSOrigin: os.Getenv("ULTRAV_CORS_ORIGIN"),
		LibvirtURI: envOr("HYPERVISOR_LIBVIRT_URI", "qemu:///system"),
		IsoDir:     envOr("ULTRAV_ISO_DIR", "/var/lib/libvirt/images/isos"),
		DBPath:     envOr("ULTRAV_DB_PATH", "/var/lib/ultrav/ultrav.db"),
		JWTSecret:  os.Getenv("ULTRAV_JWT_SECRET"),
		AdminUser:     os.Getenv("ULTRAV_ADMIN_USER"),
		AdminPassword: os.Getenv("ULTRAV_ADMIN_PASSWORD"),
	}
	switch cfg.Provider {
	case ProviderMock, ProviderLibvirt:
	default:
		return cfg, fmt.Errorf("invalid HYPERVISOR_PROVIDER %q (supported: mock, libvirt)", cfg.Provider)
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
