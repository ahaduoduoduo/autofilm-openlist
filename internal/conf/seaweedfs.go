package conf

// SeaweedFS is opt-in and independent of the Restic and ordinary download APIs.
type SeaweedFS struct {
	Enable                   bool             `json:"enable" env:"ENABLE"`
	RecoveryToken            string           `json:"recovery_token" env:"RECOVERY_TOKEN"`
	MaxObjectMiB             int64            `json:"max_object_mib" env:"MAX_OBJECT_MIB"`
	ReadIntervalMilliseconds int64            `json:"read_interval_ms" env:"READ_INTERVAL_MS"`
	CooldownSeconds          int64            `json:"cooldown_seconds" env:"COOLDOWN_SECONDS"`
	Spaces                   []SeaweedFSSpace `json:"spaces"`
}

type SeaweedFSSpace struct {
	Name      string `json:"name"`
	Token     string `json:"token"`
	MountPath string `json:"mount_path"`
	ParentID  string `json:"parent_id"`
}
