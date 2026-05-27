package core

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/ssh"
)

type PortManager struct {
	cfg     *config.Config
	logger  *slog.Logger
	servers map[string]*ssh.SSHServer
	mu      sync.RWMutex
}

func NewPortManager(cfg *config.Config, logger *slog.Logger) *PortManager {
	return &PortManager{
		cfg:     cfg,
		logger:  logger,
		servers: make(map[string]*ssh.SSHServer),
	}
}

func (pm *PortManager) AutoStart() error {
	for _, portCfg := range pm.cfg.Ports {
		if _, exists := pm.servers[portCfg.Device]; exists {
			continue
		}
		srv, err := ssh.NewSSHServer(portCfg.Device, pm.cfg, pm.logger.With("device", portCfg.Device))
		if err != nil {
			pm.logger.Warn("skipping port", "device", portCfg.Device, "error", err)
			continue
		}
		if err := srv.Start(); err != nil {
			pm.logger.Error("SSH server start failed", "device", portCfg.Device, "error", err)
			continue
		}
		pm.mu.Lock()
		pm.servers[portCfg.Device] = srv
		pm.mu.Unlock()
		pm.logger.Info("started mapping", "device", portCfg.Device, "ssh_port", srv.Port())
	}
	return nil
}

func (pm *PortManager) Mappings() []map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]map[string]interface{}, 0, len(pm.servers))
	for _, srv := range pm.servers {
		result = append(result, map[string]interface{}{
			"serial_port": srv.Device(),
			"ssh_port":    srv.Port(),
			"connected":   srv.Session().IsConnected(),
		})
	}
	return result
}

func (pm *PortManager) GetServer(device string) *ssh.SSHServer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.servers[device]
}

func (pm *PortManager) AddMapping(device string) (*ssh.SSHServer, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.servers[device]; exists {
		return nil, fmt.Errorf("mapping already exists for %s", device)
	}
	srv, err := ssh.NewSSHServer(device, pm.cfg, pm.logger.With("device", device))
	if err != nil {
		return nil, err
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	pm.servers[device] = srv
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
	return nil
}

func (pm *PortManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, srv := range pm.servers {
		srv.Stop()
	}
	pm.servers = make(map[string]*ssh.SSHServer)
}