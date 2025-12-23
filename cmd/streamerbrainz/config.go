package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level YAML configuration for the streamerbrainz daemon.
//
// This is intentionally user-facing and stable-ish. Keep defaults and validation
// centralized so the rest of the code can assume a well-formed config.
//
// Design goals:
// - Make config file the primary configuration surface.
// - Keep flags for small overrides and for environments where a file is awkward.
// - Preserve current defaults for existing users where practical.
type Config struct {
	// IR input configuration
	IR IRConfig `yaml:"ir"`

	// CamillaDSP control configuration
	CamillaDSP CamillaDSPConfig `yaml:"camilladsp"`

	// Velocity engine configuration
	Velocity VelocityFileConfig `yaml:"velocity"`

	// IPC configuration (used by librespot hook integration)
	IPC IPCConfig `yaml:"ipc"`

	// Webhooks server configuration
	Webhooks WebhooksConfig `yaml:"webhooks"`

	// Plex integration
	Plex PlexConfig `yaml:"plex"`

	// Logging
	Logging LoggingConfig `yaml:"logging"`
}

type IRConfig struct {
	Device  string   `yaml:"device,omitempty"`  // Deprecated: use Devices instead
	Devices []string `yaml:"devices,omitempty"` // List of input devices to monitor
}

type CamillaDSPConfig struct {
	WsURL      string  `yaml:"ws_url"`
	TimeoutMS  int     `yaml:"timeout_ms"`
	MinDB      float64 `yaml:"min_db"`
	MaxDB      float64 `yaml:"max_db"`
	UpdateHz   int     `yaml:"update_hz"`
	RampUpMS   int     `yaml:"ramp_up_ms,omitempty"`   // optional: if you want to document it alongside config
	RampDownMS int     `yaml:"ramp_down_ms,omitempty"` // optional
}

type IPCConfig struct {
	SocketPath string `yaml:"socket_path"`
}

type WebhooksConfig struct {
	Port int `yaml:"port"`
}

type PlexConfig struct {
	ServerURL  string `yaml:"server_url"`
	TokenFile  string `yaml:"token_file"`
	MachineID  string `yaml:"machine_id"`
	Enabled    bool   `yaml:"enabled"`
	BindLocal  bool   `yaml:"bind_local,omitempty"`  // optional hardening knob for future
	AllowCIDRs []any  `yaml:"allow_cidrs,omitempty"` // placeholder for future; keep as any to avoid committing to a format
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

// VelocityFileConfig is the user-facing velocity configuration as represented in YAML.
//
// It maps 1:1 to VelocityConfig used by the engine, but uses YAML-friendly types.
// (e.g., hold timeout is represented in milliseconds).
type VelocityFileConfig struct {
	Mode string `yaml:"mode"` // "accelerating" or "constant"

	// Shared:
	// - accelerating: max velocity (dB/s)
	// - constant: base hold rate (dB/s)
	MaxDBPerSec float64 `yaml:"max_db_per_sec"`

	// Accelerating-mode only:
	AccelTimeSec float64 `yaml:"accel_time_sec,omitempty"`
	DecayTauSec  float64 `yaml:"decay_tau_sec,omitempty"`

	// Constant-mode turbo:
	TurboMult  float64 `yaml:"turbo_mult,omitempty"`
	TurboDelay float64 `yaml:"turbo_delay_sec,omitempty"`

	// Hold behavior:
	HoldTimeoutMS int `yaml:"hold_timeout_ms"`

	// Danger zone (near max volume, ramp-up only):
	DangerZoneDB            float64 `yaml:"danger_zone_db"`
	DangerVelMaxDBPerSec    float64 `yaml:"danger_vel_max_db_per_sec"`
	DangerVelMinNear0DBPerS float64 `yaml:"danger_vel_min_near0_db_per_sec"`
}

// DefaultConfig returns a fully-populated Config with defaults.
// Keep this aligned with constants.go defaults and current CLI defaults.
func DefaultConfig() Config {
	return Config{
		IR: IRConfig{
			Devices: []string{"/dev/input/event6"},
		},
		CamillaDSP: CamillaDSPConfig{
			WsURL:     "ws://127.0.0.1:1234",
			TimeoutMS: defaultReadTimeoutMS,
			MinDB:     -65.0,
			MaxDB:     0.0,
			UpdateHz:  defaultUpdateHz,
		},
		Velocity: VelocityFileConfig{
			Mode:                    string(VelocityModeAccelerating),
			MaxDBPerSec:             defaultVelMaxDBPerS,
			AccelTimeSec:            defaultAccelTime,
			DecayTauSec:             defaultDecayTau,
			TurboMult:               1.0,
			TurboDelay:              0.0,
			HoldTimeoutMS:           600,
			DangerZoneDB:            dangerZoneDB,
			DangerVelMaxDBPerSec:    dangerVelMaxDBPerS,
			DangerVelMinNear0DBPerS: dangerVelMinNear0DBPerS,
		},
		IPC: IPCConfig{
			SocketPath: "/tmp/streamerbrainz.sock",
		},
		Webhooks: WebhooksConfig{
			Port: 3001,
		},
		Plex: PlexConfig{
			Enabled:   false,
			ServerURL: "",
			TokenFile: "",
			MachineID: "",
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// LoadConfigFile reads and parses a YAML config file.
//
// Notes:
//   - The file must be valid YAML.
//   - Unknown fields are rejected (helps catch typos) via KnownFields(true).
//   - Relative paths inside the config (like plex token file) are not rewritten here;
//     handle that in validation or in the call site as needed.
func LoadConfigFile(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is empty")
	}
	b, err := os.ReadFile(ExpandPath(path))
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	cfg := DefaultConfig()

	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)

	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config yaml: %w", err)
	}

	// Ensure there's no trailing garbage (only whitespace/comments are allowed after the document).
	if err := dec.Decode(&struct{}{}); err == nil {
		return Config{}, fmt.Errorf("decode config yaml: unexpected trailing document")
	}

	return cfg, nil
}

// ApplyFlagOverrides applies overrides from flags on top of a loaded config.
//
// This is designed so you can keep a config file as the primary configuration source,
// but still do ad-hoc overrides for debugging/systemd overrides.
//
// Flags should pass pointers; each override is only applied if "set" is true.
//
// NOTE: This file only defines the mechanism; main.go should decide what flags exist.
// Keeping the override mechanism separate makes it easy to evolve flags without
// proliferating conditionals all over the code.
type FlagOverrides struct {
	IRDevice *string

	CamillaWsURL     *string
	CamillaTimeoutMS *int
	CamillaMinDB     *float64
	CamillaMaxDB     *float64
	CamillaUpdateHz  *int

	VelMode         *string
	VelMaxDBPerSec  *float64
	VelAccelTimeSec *float64
	VelDecayTauSec  *float64
	VelTurboMult    *float64
	VelTurboDelay   *float64

	VelHoldTimeoutMS           *int
	VelDangerZoneDB            *float64
	VelDangerVelMaxDBPerSec    *float64
	VelDangerVelMinNear0DBPerS *float64

	IPCSocketPath *string
	WebhooksPort  *int

	PlexEnabled   *bool
	PlexServerURL *string
	PlexTokenFile *string
	PlexMachineID *string

	LogLevel *string
}

// Apply merges the overrides into cfg. If an override pointer is nil, it is ignored.
// If the pointer is non-nil, the value is applied (even if it is a “zero value”).
func (o FlagOverrides) Apply(cfg *Config) {
	if cfg == nil {
		return
	}
	if o.IRDevice != nil {
		// For backward compatibility, setting IRDevice flag sets both old and new fields
		cfg.IR.Device = *o.IRDevice
		cfg.IR.Devices = []string{*o.IRDevice}
	}

	if o.CamillaWsURL != nil {
		cfg.CamillaDSP.WsURL = *o.CamillaWsURL
	}
	if o.CamillaTimeoutMS != nil {
		cfg.CamillaDSP.TimeoutMS = *o.CamillaTimeoutMS
	}
	if o.CamillaMinDB != nil {
		cfg.CamillaDSP.MinDB = *o.CamillaMinDB
	}
	if o.CamillaMaxDB != nil {
		cfg.CamillaDSP.MaxDB = *o.CamillaMaxDB
	}
	if o.CamillaUpdateHz != nil {
		cfg.CamillaDSP.UpdateHz = *o.CamillaUpdateHz
	}

	if o.VelMode != nil {
		cfg.Velocity.Mode = *o.VelMode
	}
	if o.VelMaxDBPerSec != nil {
		cfg.Velocity.MaxDBPerSec = *o.VelMaxDBPerSec
	}
	if o.VelAccelTimeSec != nil {
		cfg.Velocity.AccelTimeSec = *o.VelAccelTimeSec
	}
	if o.VelDecayTauSec != nil {
		cfg.Velocity.DecayTauSec = *o.VelDecayTauSec
	}
	if o.VelTurboMult != nil {
		cfg.Velocity.TurboMult = *o.VelTurboMult
	}
	if o.VelTurboDelay != nil {
		cfg.Velocity.TurboDelay = *o.VelTurboDelay
	}

	if o.VelHoldTimeoutMS != nil {
		cfg.Velocity.HoldTimeoutMS = *o.VelHoldTimeoutMS
	}
	if o.VelDangerZoneDB != nil {
		cfg.Velocity.DangerZoneDB = *o.VelDangerZoneDB
	}
	if o.VelDangerVelMaxDBPerSec != nil {
		cfg.Velocity.DangerVelMaxDBPerSec = *o.VelDangerVelMaxDBPerSec
	}
	if o.VelDangerVelMinNear0DBPerS != nil {
		cfg.Velocity.DangerVelMinNear0DBPerS = *o.VelDangerVelMinNear0DBPerS
	}

	if o.IPCSocketPath != nil {
		cfg.IPC.SocketPath = *o.IPCSocketPath
	}
	if o.WebhooksPort != nil {
		cfg.Webhooks.Port = *o.WebhooksPort
	}

	if o.PlexEnabled != nil {
		cfg.Plex.Enabled = *o.PlexEnabled
	}
	if o.PlexServerURL != nil {
		cfg.Plex.ServerURL = *o.PlexServerURL
	}
	if o.PlexTokenFile != nil {
		cfg.Plex.TokenFile = *o.PlexTokenFile
	}
	if o.PlexMachineID != nil {
		cfg.Plex.MachineID = *o.PlexMachineID
	}

	if o.LogLevel != nil {
		cfg.Logging.Level = *o.LogLevel
	}
}

// Validate checks config invariants and returns a user-friendly error.
// This is intended to be called after defaults + file + overrides are applied.
func (c *Config) Validate() error {
	// IR - support both old single device and new multiple devices
	// Migrate old config format to new format if needed
	if c.IR.Device != "" && len(c.IR.Devices) == 0 {
		c.IR.Devices = []string{c.IR.Device}
	}
	if len(c.IR.Devices) == 0 {
		return errors.New("ir.devices must not be empty (or use deprecated ir.device)")
	}
	// Validate all device paths are non-empty
	for i, dev := range c.IR.Devices {
		if dev == "" {
			return fmt.Errorf("ir.devices[%d] is empty", i)
		}
	}

	// CamillaDSP
	if c.CamillaDSP.WsURL == "" {
		return errors.New("camilladsp.ws_url must not be empty")
	}
	if c.CamillaDSP.TimeoutMS <= 0 {
		return errors.New("camilladsp.timeout_ms must be > 0")
	}
	if c.CamillaDSP.MinDB > c.CamillaDSP.MaxDB {
		return errors.New("camilladsp.min_db must be <= camilladsp.max_db")
	}
	if c.CamillaDSP.UpdateHz <= 0 || c.CamillaDSP.UpdateHz > 1000 {
		return errors.New("camilladsp.update_hz must be between 1 and 1000")
	}

	// Velocity
	mode := c.Velocity.Mode
	if mode == "" {
		mode = string(VelocityModeAccelerating)
	}
	if mode != string(VelocityModeAccelerating) && mode != string(VelocityModeConstant) {
		return fmt.Errorf("velocity.mode must be %q or %q", VelocityModeAccelerating, VelocityModeConstant)
	}
	if c.Velocity.MaxDBPerSec < 0 {
		return errors.New("velocity.max_db_per_sec must be >= 0")
	}
	if c.Velocity.HoldTimeoutMS < 0 {
		return errors.New("velocity.hold_timeout_ms must be >= 0")
	}
	if c.Velocity.DangerZoneDB < 0 {
		return errors.New("velocity.danger_zone_db must be >= 0")
	}
	if c.Velocity.DangerVelMaxDBPerSec < 0 {
		return errors.New("velocity.danger_vel_max_db_per_sec must be >= 0")
	}
	if c.Velocity.DangerVelMinNear0DBPerS < 0 {
		return errors.New("velocity.danger_vel_min_near0_db_per_sec must be >= 0")
	}
	if c.Velocity.DangerVelMinNear0DBPerS > c.Velocity.DangerVelMaxDBPerSec {
		return errors.New("velocity.danger_vel_min_near0_db_per_sec must be <= velocity.danger_vel_max_db_per_sec")
	}

	// Plex
	if c.Plex.Enabled {
		if c.Plex.ServerURL == "" {
			return errors.New("plex.enabled is true but plex.server_url is empty")
		}
		if c.Plex.TokenFile == "" {
			return errors.New("plex.enabled is true but plex.token_file is empty")
		}
		if c.Plex.MachineID == "" {
			return errors.New("plex.enabled is true but plex.machine_id is empty")
		}
	}

	// Logging
	if c.Logging.Level == "" {
		return errors.New("logging.level must not be empty")
	}

	return nil
}

// ToVelocityConfig converts file config + CamillaDSP bounds into the internal engine config.
func (c *Config) ToVelocityConfig() VelocityConfig {
	cfg := VelocityConfig{
		Mode: VelocityMode(c.Velocity.Mode),

		VelMaxDBPerS: c.Velocity.MaxDBPerSec,

		MinDB: c.CamillaDSP.MinDB,
		MaxDB: c.CamillaDSP.MaxDB,

		HoldTimeout: time.Duration(c.Velocity.HoldTimeoutMS) * time.Millisecond,

		DangerZoneDB:            c.Velocity.DangerZoneDB,
		DangerVelMaxDBPerS:      c.Velocity.DangerVelMaxDBPerSec,
		DangerVelMinNear0DBPerS: c.Velocity.DangerVelMinNear0DBPerS,
	}

	// Mode-specific mapping
	switch cfg.Mode {
	case VelocityModeConstant:
		cfg.AccelTime = c.Velocity.TurboMult // turbo multiplier
		cfg.DecayTau = c.Velocity.TurboDelay // turbo delay (seconds)
	default:
		cfg.Mode = VelocityModeAccelerating
		cfg.AccelTime = c.Velocity.AccelTimeSec
		cfg.DecayTau = c.Velocity.DecayTauSec
	}

	return cfg
}

// ExpandPath expands a leading "~" in a path using $HOME.
// This is handy for config values like plex.token_file.
func ExpandPath(p string) string {
	if p == "" {
		return p
	}
	if p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) >= 2 && (p[1] == '/' || p[1] == '\\') {
		return filepath.Join(home, p[2:])
	}
	return p
}
