package service

// Result describes the system-level privileged helper independently of the
// existing per-user login service.
type Result struct {
	Path       string `json:"path"`
	Manager    string `json:"manager"`
	Installed  bool   `json:"installed"`
	Active     bool   `json:"active"`
	Executable string `json:"executable"`
}
