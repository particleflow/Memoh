package media

import "testing"

func TestTransferNetworkPolicySnapshotAndValidation(t *testing.T) {
	s := NewService(nil, nil)
	input := &TransferNetworkPolicy{Version: 1, LANAddresses: []string{"::ffff:192.0.2.1", "2001:0DB8::1"}}
	if err := s.SetTransferNetworkPolicy(input); err != nil {
		t.Fatal(err)
	}
	input.LANAddresses[0] = "changed"
	got := s.TransferNetworkPolicy()
	if got.LANAddresses[0] != "192.0.2.1" || got.LANAddresses[1] != "2001:db8::1" {
		t.Fatal(got)
	}
	got.LANAddresses[0] = "changed"
	if s.TransferNetworkPolicy().LANAddresses[0] != "192.0.2.1" {
		t.Fatal("shared mutable policy")
	}
	for _, bad := range []*TransferNetworkPolicy{
		{Version: 2, LANAddresses: []string{"192.0.2.1"}},
		{Version: 1},
		{Version: 1, LANAddresses: []string{"192.0.2.1", "::ffff:192.0.2.1"}},
		{Version: 1, LANAddresses: []string{"0.0.0.0"}},
		{Version: 1, LANAddresses: []string{"ff02::1"}},
		{Version: 1, LANAddresses: []string{"https://example.invalid"}},
	} {
		if err := s.SetTransferNetworkPolicy(bad); err == nil {
			t.Fatal("accepted invalid policy")
		}
	}
	if s.TransferNetworkPolicy().LANAddresses[0] != "192.0.2.1" {
		t.Fatal("invalid policy changed snapshot")
	}
	if err := s.SetTransferNetworkPolicy(nil); err != nil || s.TransferNetworkPolicy() != nil {
		t.Fatal("omission did not clear startup policy")
	}
}
