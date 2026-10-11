package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This is a test-only identity and ciphertext generated with the pinned
// mihomo component/age package. No external subscription credential is used.
func TestNativeAgeRefreshAndKeyLifecycle(t *testing.T) {
	binary := "/root/mihomo/bin/mihomo"
	if _, err := os.Stat(binary); err != nil {
		t.Skip("pinned native mihomo binary unavailable")
	}
	identity, err := os.ReadFile("testdata/identity.txt")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile("testdata/encrypted.txt")
	if err != nil {
		t.Fatal(err)
	}
	body := encrypted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "config"), filepath.Join(root, "cache"), server.Client())
	s.Binary = binary
	if err := s.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(context.Background(), "work"); err == nil || !strings.Contains(err.Error(), "needs an identity") {
		t.Fatalf("missing key error: %v", err)
	}
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, identity, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(s.keyDir()); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private dir: %v %v", info, err)
	}
	if info, err := os.Stat(s.keyPath("work")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private key: %v %v", info, err)
	}
	result, err := s.Refresh(context.Background(), "work")
	if err != nil || result.Bytes == 0 {
		t.Fatalf("native decrypt: %+v %v", result, err)
	}
	cache, err := s.Cached("work")
	if err != nil || !strings.Contains(string(cache), "fixture-node") || strings.Contains(string(cache), "AGE ENCRYPTED") {
		t.Fatalf("decrypted cache invalid: %v", err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || !list[0].KeyConfigured {
		t.Fatalf("key public state: %+v %v", list, err)
	}
	updatedAt := list[0].UpdatedAt
	wrong := filepath.Join(root, "wrong.txt")
	wrongIdentity, err := os.ReadFile("testdata/wrong-identity.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrong, wrongIdentity, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", wrong); err != nil {
		t.Fatal(err)
	}
	_, err = s.Refresh(context.Background(), "work")
	if err == nil || strings.Contains(err.Error(), string(wrongIdentity)) || strings.Contains(err.Error(), string(identity)) {
		t.Fatalf("unsafe or missing decrypt failure: %v", err)
	}
	after, _ := s.Cached("work")
	if string(after) != string(cache) {
		t.Fatal("decryption failure changed cache")
	}
	list, _ = s.List()
	if !list[0].UpdatedAt.Equal(updatedAt) {
		t.Fatal("decryption failure changed refresh metadata")
	}
	if err := s.ClearKey("work"); err != nil {
		t.Fatal(err)
	}
	list, _ = s.List()
	if list[0].KeyConfigured {
		t.Fatal("clear retained key")
	}
	body = []byte("proxies:\n  - name: plaintext-node\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n")
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatalf("plain refresh after clear: %v", err)
	}
	plain, _ := s.Cached("work")
	if !strings.Contains(string(plain), "plaintext-node") {
		t.Fatal("plain cache absent")
	}
	if err := os.Symlink(source, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", filepath.Join(root, "link")); err == nil {
		t.Fatal("accepted symlink identity")
	}
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", fifo); err == nil {
		t.Fatal("accepted FIFO identity")
	}
}
