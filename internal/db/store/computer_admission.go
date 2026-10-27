package store

import "context"

// ComputerAdmission is an owner-controlled admission fence, not task state or
// a host stop permit. No credential or provider configuration is projected.
type ComputerAdmission struct {
	ConnectionID string `json:"connection_id"`
	DeviceID     string `json:"device_id"`
	InstanceID   string `json:"instance_id"`
	RequestID    string `json:"request_id"`
	Paused       bool   `json:"paused"`
	Revision     int64  `json:"revision"`
}

type ComputerAdmissionStore interface {
	ReadComputerAdmission(context.Context, string, string, string, string) (ComputerAdmission, error)
	ChangeComputerAdmission(context.Context, string, string, JSON, string, int64, bool) (ComputerAdmission, error)
}

// NativeAdmissionSnapshot is internal: only approved identity/digest fields may
// be projected to HTTP. The runtime registration and gate share one DB snapshot.
type NativeAdmissionSnapshot struct {
	RuntimeID      string             `json:"runtime_id"`
	Provider       string             `json:"provider"`
	ProviderConfig JSON               `json:"provider_config"`
	Gate           *ComputerAdmission `json:"gate"`
}

type NativeAdmissionSnapshotStore interface {
	ReadNativeAdmissionSnapshot(context.Context, string, string) (NativeAdmissionSnapshot, error)
}
