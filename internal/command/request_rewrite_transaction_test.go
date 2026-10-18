package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteTransactionExcludesOnlyCAAndRestoresProfileDocuments(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "request-rewrite")
	ca := filepath.Join(base, "ca")
	if e := os.MkdirAll(ca, 0700); e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(ca, "root.pem")
	document := filepath.Join(base, "work.json")
	caProfile := filepath.Join(base, "ca.json")
	for path, contents := range map[string]string{key: "old fictitious private key", document: "old document", caProfile: "profile named ca"} {
		if e := os.WriteFile(path, []byte(contents), 0600); e != nil {
			t.Fatal(e)
		}
	}
	images, e := snapshotConfig(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(images) != 2 || string(images[document].data) != "old document" || string(images[caProfile].data) != "profile named ca" {
		t.Fatal("CA entered snapshot or profile excluded")
	}
	if e = os.WriteFile(key, []byte("new fictitious private key"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(document, []byte("new document"), 0600); e != nil {
		t.Fatal(e)
	}
	extra := filepath.Join(ca, "new-state")
	os.WriteFile(extra, []byte("retained"), 0600)
	introduced := filepath.Join(base, "new-profile.json")
	os.WriteFile(introduced, []byte("rolled back"), 0600)
	if e = restoreConfig(root, images); e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{key: "new fictitious private key", extra: "retained", document: "old document", caProfile: "profile named ca"} {
		got, e := os.ReadFile(path)
		if e != nil || string(got) != want {
			t.Fatal("rollback crossed CA boundary", path, e)
		}
	}
	if _, e = os.Stat(introduced); !os.IsNotExist(e) {
		t.Fatal("profile document not rolled back", e)
	}
	// A private identity subtree with content that cannot be copied as a regular
	// config file is skipped entirely, proving snapshot does not inspect it.
	if e = os.Symlink(filepath.Join(root, "nonexistent"), filepath.Join(ca, "not-config")); e != nil {
		t.Fatal(e)
	}
	if _, e = snapshotConfig(root); e != nil {
		t.Fatal("snapshot traversed CA subtree", e)
	}
}
func TestRewriteTransactionRejectsSymlinkAtCABoundary(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "request-rewrite")
	os.MkdirAll(base, 0700)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "root.pem"), []byte("fictitious key"), 0600)
	if e := os.Symlink(outside, filepath.Join(base, "ca")); e != nil {
		t.Fatal(e)
	}
	if _, e := snapshotConfig(root); e == nil {
		t.Fatal("symlink CA accepted")
	}
	if e := restoreConfig(root, map[string]fileImage{}); e == nil {
		t.Fatal("restore accepted symlink CA")
	}
	got, e := os.ReadFile(filepath.Join(outside, "root.pem"))
	if e != nil || string(got) != "fictitious key" {
		t.Fatal("outside modified", e)
	}
}
