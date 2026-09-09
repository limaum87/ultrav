package api

import (
	"log/slog"
	"testing"

	"github.com/ultrav/ultrav/backend/internal/iso"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testIsoStore(t *testing.T) *iso.Store {
	t.Helper()
	s, err := iso.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
