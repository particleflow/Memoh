package userruntime

import (
	"time"

	"github.com/felinics/memoh/internal/agenvo"
)

// Runtime is one registered client. Machine details only exist while its
// reverse-RPC connection is online; they are not persisted.
type Runtime struct {
	Provider      string               `json:"provider,omitempty"`
	ID            string               `json:"id"`
	TeamID        string               `json:"team_id"`
	Name          string               `json:"name"`
	Key           string               `json:"key"`
	CreatedAt     time.Time            `json:"created_at"`
	Online        bool                 `json:"online"`
	Availability  string               `json:"availability,omitempty"`
	WorkspaceBase string               `json:"workspace_base,omitempty"`
	Hostname      string               `json:"hostname,omitempty"`
	OS            string               `json:"os,omitempty"`
	Arch          string               `json:"arch,omitempty"`
	ClientVersion string               `json:"client_version,omitempty"`
	Capabilities  []string             `json:"capabilities,omitempty"`
	NativeHerdr   *NativeHerdrIdentity `json:"native_herdr,omitempty"`
}

// NativeHerdrIdentity is the current server-resolved binding, not a selectable
// target or a credential. Bot requests still use their existing mount target_id.
type NativeHerdrIdentity struct {
	ConnectionID      string `json:"connection_id"`
	DeviceID          string `json:"device_id"`
	InstanceID        string `json:"instance_id"`
	Session           string `json:"session"`
	TargetFingerprint string `json:"target_fingerprint"`
	ConfigSHA256      string `json:"config_sha256"`
}

type CreateRuntimeRequest struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider,omitempty" enums:"memoh"`
	Agenvo   *agenvo.Target `json:"agenvo,omitempty" swaggerignore:"true"`
}

type HandshakeInfo struct {
	Version       int      `json:"version"`
	Hostname      string   `json:"hostname"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	ClientVersion string   `json:"client_version"`
	WorkspaceBase string   `json:"workspace_base"`
	Capabilities  []string `json:"capabilities"`
}
