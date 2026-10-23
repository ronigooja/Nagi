package scripts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlineInstallerDownloadFallback(t *testing.T) {
	for _, mode := range []string{"direct", "network", "checksum-network", "both", "corrupt", "proxy-corrupt", "custom"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			archivePath := filepath.Join(root, "archive.tar.gz")
			archive, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(archive)
			tw := tar.NewWriter(gz)
			for _, name := range []string{"nagi", "mihomo", "installer.sh"} {
				body := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$FIXTURE/installed\"\n"
				if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0700, Size: int64(len(body))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(root, "sums"), fmt.Sprintf("%x  nagi-linux-amd64.tar.gz\n", sha256.Sum256(data)))
			write(filepath.Join(bin, "uname"), "#!/bin/sh\ncase $1 in -s) echo Linux;; -m) echo x86_64;; esac\n")
			write(filepath.Join(bin, "curl"), `#!/bin/sh
while [ "$#" -gt 0 ]; do
    case "$1" in
        --output) destination=$2; shift 2 ;;
        *) url=$1; shift ;;
    esac
done
printf '%s\n' "$url" >> "$FIXTURE/requests"
proxy=no
case "$url" in https://gh-proxy.com/*) proxy=yes;; esac
if [ "$MODE" = both ] || [ "$MODE" = custom ]; then exit 22; fi
if [ "$proxy" = no ]; then
    case "$MODE" in
        network|proxy-corrupt) printf partial > "$destination"; exit 28 ;;
        checksum-network) case "$url" in */SHA256SUMS) exit 22;; esac ;;
    esac
fi
case "$url" in
    */SHA256SUMS)
        if [ "$MODE" = corrupt ] || [ "$MODE" = proxy-corrupt ]; then
            printf '%064d  nagi-linux-amd64.tar.gz\n' 0 > "$destination"
        else
            cp "$FIXTURE/sums" "$destination"
        fi ;;
    *) cp "$FIXTURE/archive.tar.gz" "$destination" ;;
esac
`)
			cmd := exec.Command("bash", "install.sh", "install", "--version", "v1.0.0", "--prefix", filepath.Join(root, "prefix"))
			base := ""
			if mode == "custom" {
				base = "https://releases.example.invalid/v1.0.0"
			}
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "MODE="+mode, "FIXTURE="+root,
				"TMPDIR="+root, "NAGI_RELEASE_BASE="+base, "NAGI_REPOSITORY=ronigooja/Nagi")
			out, err := cmd.CombinedOutput()
			wantErr := mode == "both" || mode == "corrupt" || mode == "proxy-corrupt" || mode == "custom"
			if (err != nil) != wantErr {
				t.Fatalf("error = %v, output: %s", err, out)
			}
			requests, readErr := os.ReadFile(filepath.Join(root, "requests"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			wantProxy := mode == "network" || mode == "checksum-network" || mode == "both" || mode == "proxy-corrupt"
			if strings.Contains(string(requests), "https://gh-proxy.com/https://github.com/ronigooja/Nagi/releases/download/v1.0.0/") != wantProxy {
				t.Fatalf("unexpected requests: %s", requests)
			}
			_, installErr := os.Stat(filepath.Join(root, "installed"))
			if (installErr == nil) == wantErr {
				t.Fatalf("installer execution after error=%t: %v", wantErr, installErr)
			}
		})
	}
}
