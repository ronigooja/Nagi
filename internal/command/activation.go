package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
)

var errActivationUnconfirmed = errors.New("mihomo configuration activation is unconfirmed")

// activateConfig uses mihomo's activation marker as evidence that the
// requested configuration actually became active. A timed-out PUT may still
// complete, so the independent recovery context checks before deciding.
func activateConfig(ctx context.Context, client *control.Client, path string) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(nonce[:])
	err := client.Put(ctx, "/configs?force=true", map[string]any{"path": path, "activation_id": id}, nil)
	var apiErr *control.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		return err
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var current struct {
			ActivationID string `json:"activation_id"`
		}
		getErr := client.Get(checkCtx, "/configs", &current)
		if getErr == nil && current.ActivationID == id {
			return nil
		}
		select {
		case <-checkCtx.Done():
			if err != nil {
				return fmt.Errorf("%w after PUT error: %v; inspect `nagi status` after mihomo settles", errActivationUnconfirmed, err)
			}
			return fmt.Errorf("%w: current marker %s", errActivationUnconfirmed, current.ActivationID)
		case <-time.After(200 * time.Millisecond):
		}
	}
}
