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
