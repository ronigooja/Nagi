//go:build darwin

package configpolicy

import (
	"os/exec"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// macOS ACLs can grant access beyond the POSIX mode. Reject any ACL on a
// snapshot directory or ancestor rather than trying to interpret its entries.
func rejectDarwinACL(path string) error {
	out, err := exec.Command("/bin/ls", "-lde", path).Output()
	if err != nil {
		return err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || len(fields[0]) < 10 {
		return i18n.Errorf("system.privileged.cannot.inspect.directory.acl", "cannot inspect directory ACL")
	}
	if strings.Contains(fields[0], "+") {
		return i18n.Errorf("system.privileged.snapshot.directory.or.ancestor.has.a.macos.acl", "snapshot directory or ancestor has a macOS ACL")
	}
	return nil
}
