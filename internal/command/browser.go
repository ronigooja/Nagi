package command

import (
	"context"
	"github.com/ronigooja/Nagi/internal/browserprefs"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func browserCommand(ctx context.Context, args []string) (any, error) {
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, fail("browser_preference_error", err)
	}
	manager := browserprefs.New(paths.StateDir)
	var state browserprefs.State
	switch args[2] {
	case "status":
		state, err = manager.Status(ctx)
	case "enable":
		state, err = manager.Enable(ctx)
	case "disable":
		state, err = manager.Disable(ctx)
	case "restore":
		state, err = manager.Restore(ctx)
	}
	if err != nil {
		return nil, fail("browser_preference_error", err)
	}
	return state, nil
}
