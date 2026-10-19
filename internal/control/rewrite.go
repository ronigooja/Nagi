package control

// RewriteProtocolVersion is the only supported request rewrite wire protocol.
const RewriteProtocolVersion = 2

// RewritePrepare carries session authorization separately for intercepted and
// reinjected destinations. It never contains the CA private key or rule bodies.
type RewritePrepare struct {
	Version             int      `json:"version"`
	Generation          string   `json:"generation"`
	PreviousGeneration  *string  `json:"previous_generation"`
	OwnerUID            uint32   `json:"owner_uid"`
	HandoffAddr         string   `json:"handoff_addr"`
	ChildTransportCert  string   `json:"child_transport_cert"`
	HandoffSecret       string   `json:"handoff_secret"`
	ReinjectionSecret   string   `json:"reinjection_secret"`
	InterceptDomains    []string `json:"intercept_domains"`
	TargetDomains       []string `json:"target_domains"`
	RuleVersion         string   `json:"rule_version"`
	BlockAuthorizedQUIC bool     `json:"block_authorized_quic"`
}

type RewriteCapabilities struct {
	Versions        []int `json:"versions"`
	TUNOnly         bool  `json:"tun_only"`
	OriginalContext bool  `json:"original_context"`
	Reinjection     bool  `json:"reinjection"`
	QUIC            bool  `json:"quic_domain_block"`
	MaxContexts     int   `json:"max_contexts"`
	MaxStreams      int   `json:"max_streams_per_context"`
	ContextIdle     int   `json:"context_idle_seconds"`
	ContextMax      int   `json:"context_max_seconds"`
}

func (c RewriteCapabilities) Supported() bool {
	version := false
	for _, v := range c.Versions {
		version = version || v == RewriteProtocolVersion
	}
	return version && c.TUNOnly && c.OriginalContext && c.Reinjection && c.QUIC && c.MaxContexts >= 1024 && c.MaxStreams >= 64 && c.ContextIdle >= 60 && c.ContextMax >= 3600
}
