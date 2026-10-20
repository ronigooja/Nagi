package profile

import "context"

// Compatibility entry points for callers transitioning to generic chain APIs.
// Legacy on-disk policies are always rejected by SubscriptionChain.
type SubscriptionTailscalePolicy struct {
	Subscription string
	Node         map[string]any
	SourceNode   map[string]any
}

func (s *Store) SubscriptionTailscale(name string) (*SubscriptionTailscalePolicy, error) {
	p, err := s.SubscriptionChain(name)
	if err != nil || p == nil {
		return nil, err
	}
	o := &SubscriptionTailscalePolicy{Subscription: p.Subscription, Node: p.Nodes[len(p.Nodes)-1]}
	if len(p.SourceNodes) > 0 {
		o.SourceNode = p.SourceNodes[0]
	}
	return o, nil
}
func (s *Store) writeSubscriptionTailscale(name string, p *SubscriptionTailscalePolicy) error {
	if p == nil {
		return s.ClearSubscriptionChain(name)
	}
	return s.writeSubscriptionChain(name, &SubscriptionChainPolicy{Version: subscriptionChainVersion, Subscription: p.Subscription, Via: []string{tsString(p.Node, "name")}, Nodes: []map[string]any{p.Node}, SourceNodes: []map[string]any{p.SourceNode}})
}
func (s *Store) ClearSubscriptionTailscale(name string) error { return s.ClearSubscriptionChain(name) }
func (s *Store) applySubscriptionTailscale(name string, data []byte) ([]byte, error) {
	return s.applySubscriptionChain(name, data)
}
func (s *Store) MutateSubscriptionTailscale(ctx context.Context, name, subscription, action, node string) error {
	via := []string{node}
	if action == "unbind" {
		via = nil
	}
	return s.mutateSubscriptionChain(ctx, name, subscription, via, action == "unbind")
}
