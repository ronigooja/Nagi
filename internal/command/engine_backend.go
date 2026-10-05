package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/engine"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type lifecycleBackend interface {
	Start(context.Context) (engine.Status, error)
	Stop(context.Context) (engine.Status, error)
	Status(context.Context) (engine.Status, error)
}

const (
	userBackend       = "user"
	privilegedBackend = "privileged"
)

func backendPath(paths nagiruntime.Paths) string {
	return filepath.Join(paths.ConfigDir, "engine-backend")
}

func readBackend(paths nagiruntime.Paths) (string, error) {
	data, err := os.ReadFile(backendPath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return userBackend, nil
	}
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(string(data))
	if mode != userBackend && mode != privilegedBackend {
		return "", fmt.Errorf("invalid engine backend in %s", backendPath(paths))
	}
	return mode, nil
}

func writeBackend(paths nagiruntime.Paths, mode string) error {
	if mode != userBackend && mode != privilegedBackend {
		return errors.New("unknown engine backend")
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(paths.ConfigDir, ".engine-backend-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(mode + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), backendPath(paths))
}
