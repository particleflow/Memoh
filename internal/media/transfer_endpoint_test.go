package media

import "testing"

func TestTransferEndpointPolicyValidationAndSnapshot(t *testing.T) {
	policy := &TransferNetworkPolicy{Version: 2, Endpoint: &TransferEndpoint{LogicalOrigin: "https://nas.example.invalid", PhysicalOrigin: "http://127.0.0.1:18068"}}
	s := NewService(nil, nil)
	if err := s.SetTransferNetworkPolicy(policy); err != nil {
		t.Fatal(err)
	}
	policy.Endpoint.PhysicalOrigin = "http://changed.invalid"
	got := s.TransferNetworkPolicy()
	if got.Endpoint.PhysicalOrigin != "http://127.0.0.1:18068" {
		t.Fatal("input aliases snapshot")
	}
	got.Endpoint.LogicalOrigin = "https://changed.invalid"
	if s.TransferNetworkPolicy().Endpoint.LogicalOrigin != "https://nas.example.invalid" {
		t.Fatal("output aliases snapshot")
	}
	for _, value := range []string{"http://user:pass@host.invalid", "http://host.invalid/path", "http://host.invalid?", "http://host.invalid#", "http://host.invalid:0", "http://host.invalid:65536", "http://host.invalid:", "file:///tmp/data", "//host.invalid", "http://host.invalid/", "http://host.invalid%2fother", "http://host.invalid:80", "https://host.invalid:443", "http://HOST.invalid"} {
		p := s.TransferNetworkPolicy()
		p.Endpoint.PhysicalOrigin = value
		if _, err := p.Normalized(); err == nil {
			t.Errorf("accepted endpoint %q", value)
		}
	}
	p := s.TransferNetworkPolicy()
	p.Endpoint.LogicalOrigin = "file:///tmp/data"
	if _, err := p.Normalized(); err == nil {
		t.Fatal("non-HTTP logical identity accepted")
	}
	p = s.TransferNetworkPolicy()
	p.Version = 1
	p.LANAddresses = []string{"127.0.0.1"}
	if _, err := p.Normalized(); err == nil {
		t.Fatal("v1 silently changed semantics")
	}
}
