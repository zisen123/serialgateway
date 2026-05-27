package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type GatewayConfig struct {
	HTTPPort int `yaml:"http_port" json:"http_port"`
}

type SerialDefaults struct {
	Baudrate     int           `yaml:"baudrate" json:"baudrate"`
	Timeout      time.Duration `yaml:"timeout" json:"timeout"`
	ByteSize     int           `yaml:"bytesize" json:"bytesize"`
	Parity       string        `yaml:"parity" json:"parity"`
	StopBits     float64       `yaml:"stopbits" json:"stopbits"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout"`
}

type SSHAuth struct {
	Type       string   `yaml:"type" json:"type"`
	Password   string   `yaml:"password" json:"password"`
	PublicKeys []string `yaml:"public_keys" json:"public_keys"`
}

type SSHConfig struct {
	BasePort int     `yaml:"base_port" json:"base_port"`
	Auth     SSHAuth `yaml:"auth" json:"auth"`
}

type ReconnectConfig struct {
	InitialInterval          time.Duration `yaml:"initial_interval" json:"initial_interval"`
	MaxInterval              time.Duration `yaml:"max_interval" json:"max_interval"`
	DiscardInputOnDisconnect bool          `yaml:"discard_input_on_disconnect" json:"discard_input_on_disconnect"`
}

type RingBufferConfig struct {
	MaxLines int `yaml:"max_lines" json:"max_lines"`
	MaxBytes int `yaml:"max_bytes" json:"max_bytes"`
}

type PortConfig struct {
	Device      string `yaml:"device" json:"device"`
	Baudrate    int    `yaml:"baudrate" json:"baudrate"`
	Type        string `yaml:"type" json:"type"`
	AdbUsername string `yaml:"adb_username" json:"adb_username"`
	AdbPassword string `yaml:"adb_password" json:"adb_password"`
}

type AdbConfig struct {
	AdbPath      string        `yaml:"adb_path" json:"adb_path"`
	BasePort     int           `yaml:"base_port" json:"base_port"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout"`
}

type ReverseTunnelConfig struct {
	Host           string `yaml:"host"             json:"host"`
	Port           int    `yaml:"port"             json:"port"`
	User           string `yaml:"user"             json:"user"`
	Password       string `yaml:"password"         json:"password"`
	PrivateKeyFile string `yaml:"private_key_file" json:"private_key_file"`
	RemotePort     int    `yaml:"remote_port"      json:"remote_port"`
	LocalPort      int    `yaml:"local_port"       json:"local_port"`
}

type Config struct {
	Gateway        GatewayConfig        `yaml:"gateway" json:"gateway"`
	SerialDefaults SerialDefaults       `yaml:"serial_defaults" json:"serial_defaults"`
	RingBuffer     RingBufferConfig     `yaml:"ring_buffer" json:"ring_buffer"`
	SSH            SSHConfig            `yaml:"ssh" json:"ssh"`
	Reconnect      ReconnectConfig      `yaml:"reconnect" json:"reconnect"`
	ReverseTunnel  ReverseTunnelConfig  `yaml:"reverse_tunnel" json:"reverse_tunnel"`
	Ports          []PortConfig         `yaml:"ports" json:"ports"`
	ADB            *AdbConfig           `yaml:"adb" json:"adb"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyDefaults(cfg)
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	return cfg, nil
}

func ApplyDefaults(cfg *Config) {
	applyDefaults(cfg)
}

func applyDefaults(cfg *Config) {
	if cfg.Gateway.HTTPPort == 0 {
		cfg.Gateway.HTTPPort = 8080
	}
	if cfg.SerialDefaults.Baudrate == 0 {
		cfg.SerialDefaults.Baudrate = 115200
	}
	if cfg.SerialDefaults.Timeout == 0 {
		cfg.SerialDefaults.Timeout = 5 * time.Second
	}
	if cfg.SerialDefaults.ByteSize == 0 {
		cfg.SerialDefaults.ByteSize = 8
	}
	if cfg.SerialDefaults.Parity == "" {
		cfg.SerialDefaults.Parity = "N"
	}
	if cfg.SerialDefaults.StopBits == 0 {
		cfg.SerialDefaults.StopBits = 1
	}
	if cfg.SerialDefaults.WriteTimeout == 0 {
		cfg.SerialDefaults.WriteTimeout = 10 * time.Second
	}
	if cfg.RingBuffer.MaxLines == 0 {
		cfg.RingBuffer.MaxLines = 100000
	}
	if cfg.RingBuffer.MaxBytes == 0 {
		cfg.RingBuffer.MaxBytes = 10485760
	}
	if cfg.SSH.BasePort == 0 {
		cfg.SSH.BasePort = 2200
	}
	if cfg.SSH.Auth.Type == "" {
		cfg.SSH.Auth.Type = "password"
	}

	if cfg.Reconnect.InitialInterval == 0 {
		cfg.Reconnect.InitialInterval = 1 * time.Second
	}
	if cfg.Reconnect.MaxInterval == 0 {
		cfg.Reconnect.MaxInterval = 30 * time.Second
	}

	if cfg.ADB != nil {
		if cfg.ADB.AdbPath == "" {
			cfg.ADB.AdbPath = "adb"
		}
		if cfg.ADB.BasePort == 0 {
			cfg.ADB.BasePort = 2300
		}
		if cfg.ADB.WriteTimeout == 0 {
			cfg.ADB.WriteTimeout = 10 * time.Second
		}
	}

	if cfg.ReverseTunnel.Host != "" {
		if cfg.ReverseTunnel.Port == 0 {
			cfg.ReverseTunnel.Port = 22
		}
		if cfg.ReverseTunnel.User == "" {
			cfg.ReverseTunnel.User = "root"
		}
		if cfg.ReverseTunnel.RemotePort == 0 {
			cfg.ReverseTunnel.RemotePort = 18080
		}
		if cfg.ReverseTunnel.LocalPort == 0 {
			cfg.ReverseTunnel.LocalPort = cfg.Gateway.HTTPPort
		}
	}
}