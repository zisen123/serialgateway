package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
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

func (s *SSHServer) Port() int                       { return s.sshPort }
func (s *SSHServer) Device() string                   { return s.device }
func (s *SSHServer) Session() *serial.SerialSession   { return s.session }
func (s *SSHServer) IsRunning() bool                  { s.mu.Lock(); defer s.mu.Unlock(); return s.running }

func loadOrGenerateHostKeyPEM(keyPath string) ([]byte, error) {
	keyData, err := os.ReadFile(keyPath)
	if err == nil {
		log.Printf("Loaded host key from %s", keyPath)
		return keyData, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read host key: %w", err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}

	marshaled, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal host key: %w", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: marshaled,
	})
	if err := os.WriteFile(keyPath, pemData, 0600); err != nil {
		return nil, fmt.Errorf("save host key: %w", err)
	}
	log.Printf("Generated new host key and saved to %s", keyPath)
	return pemData, nil
}

func (s *SSHServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	pemData, err := loadOrGenerateHostKeyPEM("host_key")
	if err != nil {
		return fmt.Errorf("host key: %w", err)
	}

	sshServer := &gliderssh.Server{
		Addr: fmt.Sprintf(":%d", s.sshPort),
	}
	sshServer.SetOption(gliderssh.HostKeyPEM(pemData))
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