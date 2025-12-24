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
	// Inputs configuration (generic input devices, e.g. keyboards, IR remotes, rotary encoders)
	Inputs []InputDevice `yaml:"inputs"`

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

	// Rotary encoder configuration
	Rotary RotaryConfig `yaml:"rotary"`

	// Logging
	Logging LoggingConfig `yaml:"logging"`
}

// InputDeviceType describes how to interpret events from an input device
type InputDeviceType string

const (
	InputDeviceTypeKey    InputDeviceType = "key"    // EV_KEY events (IR remotes, keyboards)
	InputDeviceTypeRotary InputDeviceType = "rotary" // EV_REL events (rotary encoders)
)

// InputDevice describes a single input device with its path and type
type InputDevice struct {
	Path string          `yaml:"path"` // Device path (e.g., /dev/input/event6)
	Type InputDeviceType `yaml:"type"` // Device type: "key" or "rotary"
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

// RotaryConfig contains rotary encoder-specific configuration
type RotaryConfig struct {
	DbPerStep          float64 `yaml:"db_per_step"`         // dB change per encoder step
	VelocityWindowMS   int     `yaml:"velocity_window_ms"`  // Time window for velocity detection (ms)
	VelocityMultiplier float64 `yaml:"velocity_multiplier"` // Multiplier for "fast spinning"
	VelocityThreshold  int     `yaml:"velocity_threshold"`  // Steps in window to trigger velocity mode
}

// DefaultConfig returns a fully-populated Config with defaults.
// Keep this aligned with constants.go defaults and current CLI defaults.
func DefaultConfig() Config {
	return Config{
		Inputs: []InputDevice{
			{Path: "/dev/input/event6", Type: InputDeviceTypeKey},
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
		Rotary: RotaryConfig{
			DbPerStep:          defaultRotaryDbPerStep,
			VelocityWindowMS:   defaultRotaryVelocityWindowMS,
			VelocityMultiplier: defaultRotaryVelocityMultiplier,
			VelocityThreshold:  defaultRotaryVelocityThreshold,
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

// Validate checks config invariants and returns a user-friendly error.
// This is intended to be called after defaults + file + overrides are applied.
func (c *Config) Validate() error {
	// Inputs
	if len(c.Inputs) == 0 {
		return errors.New("inputs must not be empty")
	}

	// Validate all input devices
	for i, dev := range c.Inputs {
		if dev.Path == "" {
			return fmt.Errorf("inputs[%d].path is empty", i)
		}
		if dev.Type == "" {
			return fmt.Errorf("inputs[%d].type is empty", i)
		}
		if dev.Type != InputDeviceTypeKey && dev.Type != InputDeviceTypeRotary {
			return fmt.Errorf("inputs[%d].type must be %q or %q", i, InputDeviceTypeKey, InputDeviceTypeRotary)
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

	// Rotary encoder
	if c.Rotary.DbPerStep < 0 {
		return errors.New("rotary.db_per_step must be >= 0")
	}
	if c.Rotary.VelocityWindowMS < 0 {
		return errors.New("rotary.velocity_window_ms must be >= 0")
	}
	if c.Rotary.VelocityMultiplier < 1 {
		return errors.New("rotary.velocity_multiplier must be >= 1")
	}
	if c.Rotary.VelocityThreshold < 1 {
		return errors.New("rotary.velocity_threshold must be >= 1")
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
