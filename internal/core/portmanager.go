package core

import (
	"fmt"
	"log"
	"sync"

	"github.com/zisen123/serialgateway/internal/adb"
	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/serial"
	"github.com/zisen123/serialgateway/internal/ssh"
)

type PortManager struct {
	cfg         *config.Config
	servers     map[string]*ssh.SSHServer
	deviceTypes map[string]string // device -> "serial" or "adb"
	mu          sync.RWMutex
}

func NewPortManager(cfg *config.Config) *PortManager {
	return &PortManager{
		cfg:         cfg,
		servers:     make(map[string]*ssh.SSHServer),
		deviceTypes: make(map[string]string),
	}
}

func (pm *PortManager) AutoStart() error {
	for _, portCfg := range pm.cfg.Ports {
		if _, exists := pm.servers[portCfg.Device]; exists {
			continue
		}
		deviceType := portCfg.Type
		if deviceType == "" {
			deviceType = "serial"
		}
		srv, err := pm.newServer(portCfg.Device, deviceType)
		if err != nil {
			log.Printf("skip %s: %v", portCfg.Device, err)
			continue
		}
		if err := srv.Start(); err != nil {
			log.Printf("SSH server start failed for %s: %v", portCfg.Device, err)
			continue
		}
		pm.mu.Lock()
		pm.servers[portCfg.Device] = srv
		pm.deviceTypes[portCfg.Device] = deviceType
		pm.mu.Unlock()
		log.Printf("started mapping: %s (%s) -> :%d", portCfg.Device, deviceType, srv.Port())
	}
	return nil
}

func (pm *PortManager) Mappings() []map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]map[string]interface{}, 0, len(pm.servers))
	for device, srv := range pm.servers {
		deviceType := pm.deviceTypes[device]
		if deviceType == "" {
			deviceType = "serial"
		}
		result = append(result, map[string]interface{}{
			"device":      device,
			"serial_port": device,
			"ssh_port":    srv.Port(),
			"connected":   srv.Session().IsConnected(),
			"type":        deviceType,
		})
	}
	return result
}

func (pm *PortManager) GetServer(device string) *ssh.SSHServer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.servers[device]
}

func (pm *PortManager) GetDeviceType(device string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if t, ok := pm.deviceTypes[device]; ok {
		return t
	}
	return "serial"
}

func (pm *PortManager) AddMapping(device string) (*ssh.SSHServer, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.servers[device]; exists {
		return nil, fmt.Errorf("mapping already exists for %s", device)
	}
	deviceType := pm.detectDeviceType(device)
	srv, err := pm.newServer(device, deviceType)
	if err != nil {
		return nil, err
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	pm.servers[device] = srv
	pm.deviceTypes[device] = deviceType
	return srv, nil
}

func (pm *PortManager) RemoveMapping(device string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	srv, exists := pm.servers[device]
	if !exists {
		return fmt.Errorf("no mapping for %s", device)
	}
	srv.Stop()
	delete(pm.servers, device)
	delete(pm.deviceTypes, device)
	return nil
}

func (pm *PortManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, srv := range pm.servers {
		srv.Stop()
	}
	pm.servers = make(map[string]*ssh.SSHServer)
	pm.deviceTypes = make(map[string]string)
}

// detectDeviceType determines whether device is "serial" or "adb".
// Priority: config PortConfig.Type > serial port list > ADB device list > default "serial".
// Must be called with pm.mu held or before servers are active.
func (pm *PortManager) detectDeviceType(device string) string {
	for _, p := range pm.cfg.Ports {
		if p.Device == device && p.Type != "" {
			return p.Type
		}
	}
	if ports, err := serial.ListPorts(); err == nil {
		for _, p := range ports {
			if p.Device == device {
				return "serial"
			}
		}
	}
	if pm.cfg.ADB != nil {
		if devices, err := adb.ListDevices(pm.cfg.ADB.AdbPath); err == nil {
			for _, d := range devices {
				if d.Serial == device {
					return "adb"
				}
			}
		}
	}
	return "serial"
}

// newServer creates an SSHServer with the appropriate session type.
func (pm *PortManager) newServer(device, deviceType string) (*ssh.SSHServer, error) {
	var sess serial.Session
	var sshPort int

	switch deviceType {
	case "adb":
		if pm.cfg.ADB == nil {
			return nil, fmt.Errorf("ADB not configured")
		}
		sess = adb.NewADBSession(device, pm.cfg)
		sshPort = ssh.ADBPortMapping(device, pm.cfg)
	default:
		sess = serial.NewSerialSession(device, pm.cfg)
		sshPort = ssh.PortMapping(device, pm.cfg)
		if sshPort == 0 {
			return nil, fmt.Errorf("cannot determine SSH port for device %s", device)
		}
	}

	return ssh.NewSSHServer(device, sshPort, pm.cfg, sess), nil
}
