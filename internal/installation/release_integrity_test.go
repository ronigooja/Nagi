package installation

import (
	"context"
	"testing"
)

func TestFetchRejectsEmptyRequiredExecutables(t *testing.T) {
	for _, empty := range []string{"nagi", "mihomo", "installer.sh"} {
		t.Run(empty, func(t *testing.T) {
			files := map[string]string{
				"nagi":         "#!/bin/sh\nexit 0\n",
				"mihomo":       "#!/bin/sh\nexit 0\n",
				"installer.sh": "#!/bin/sh\nexit 0\n",
			}
			files[empty] = ""
			server := releaseServer(t, archiveBytes(t, files, ""), false, nil)
			_, cleanup, err := Fetch(context.Background(), server.URL, "linux-amd64", "v1.0.0")
			if cleanup != nil {
				cleanup()
			}
			if err == nil {
				t.Fatalf("accepted a checksummed release containing empty %s", empty)
			}
		})
	}
}
