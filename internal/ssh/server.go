package ssh

import (
	"fmt"
	"log"
	"strconv"
	"sync"

	gliderssh "github.com/gliderlabs/ssh"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/serial"
)

func PortMapping(device string, cfg *config.Config) int {
	numStr := device
	if len(numStr) > 3 && numStr[:3] == "COM" {
		numStr = numStr[3:]
	}
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	return cfg.SSH.BasePort + num
}

type SSHServer struct {
	device  string
	sshPort int
	cfg     *config.Config
	session *serial.SerialSession
	server  *gliderssh.Server
	mu      sync.Mutex
	running bool
}

func NewSSHServer(device string, cfg *config.Config) (*SSHServer, error) {
	sshPort := PortMapping(device, cfg)
	if sshPort == 0 {
		return nil, fmt.Errorf("cannot determine SSH port for device %s", device)
	}
	sess := serial.NewSerialSession(device, cfg)
	srv := &SSHServer{
		device:  device,
		sshPort: sshPort,
		cfg:     cfg,
		session: sess,
	}
	return srv, nil
}

func (s *SSHServer) Port() int                    { return s.sshPort }
func (s *SSHServer) Device() string                { return s.device }
func (s *SSHServer) Session() *serial.SerialSession { return s.session }
func (s *SSHServer) IsRunning() bool               { s.mu.Lock(); defer s.mu.Unlock(); return s.running }

func (s *SSHServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	sshServer := &gliderssh.Server{
		Addr: fmt.Sprintf(":%d", s.sshPort),
	}
	if s.cfg.SSH.Auth.Type == "password" {
		sshServer.PasswordHandler = func(ctx gliderssh.Context, password string) bool {
			return password == s.cfg.SSH.Auth.Password
		}
	}
	sshServer.Handler = s.handleSession
	s.server = sshServer
	s.running = true
	go func() {
		log.Printf("SSH server for %s listening on :%d", s.device, s.sshPort)
		if err := s.server.ListenAndServe(); err != nil {
			log.Printf("SSH server for %s stopped: %v", s.device, err)
		}
	}()
	return nil
}

func (s *SSHServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	if s.server != nil {
		s.server.Close()
	}
	s.session.Close()
	return nil
}