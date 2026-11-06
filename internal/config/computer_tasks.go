package config

// ComputerTasksConfig bounds the existing operation JSON evidence history.
// Zero uses 32 observations; lowering the limit never deletes saved evidence.
type ComputerTasksConfig struct {
	NativeObservationLimit int `toml:"native_observation_limit"`
}
