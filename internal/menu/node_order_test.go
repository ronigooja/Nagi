package menu

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestNodeManagementSourceOrderAndProviderNavigation(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{
		"proxy node list": {Data: menuNodeList{Profile: "work", Nodes: []menuProxyNode{
			{Name: "Z-sub", Type: "vmess", Subscription: "feed"},
			{Name: "A-sub", Type: "socks5", Subscription: "feed"},
			{Name: "Local", Type: "socks5"},
		}, ProviderCandidates: []string{"Z-provider", "A-provider"}}},
		"proxy provider show Z-provider": {Data: menuProvider{Name: "Z-provider", Nodes: []string{"Z-node", "A-node"}}},
	}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	page := localizednodeListPage(s.language)
	v, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, item := range v.Items {
		names = append(names, item.Name)
	}
	if !reflect.DeepEqual(names, []string{"Z-sub", "A-sub", "Z-provider", "A-provider", "Local"}) {
		t.Fatal(names)
	}
	provider, err := v.Items[2].Next().Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Items) != 2 || provider.Items[0].Name != "Z-node" || provider.Items[1].Name != "A-node" {
		t.Fatalf("provider nodes=%+v", provider.Items)
	}
	// Returning to the list must remove stale subscription/provider entries.
	r.results["proxy node list"] = Result{Data: menuNodeList{Profile: "work", Nodes: []menuProxyNode{{Name: "Local", Type: "socks5"}}}}
	v, err = page.Load(context.Background(), s)
	if err != nil || len(v.Items) != 1 || v.Items[0].Name != "Local" {
		t.Fatalf("view=%+v err=%v", v, err)
	}
}
