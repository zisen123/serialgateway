# SerialGateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go-based serial port gateway (Windows platform) that maps physical COM ports to SSH listening ports and provides an HTTP REST API for management and history queries.

**Architecture:** SerialGateway is a single Go binary with three internal layers: a PortManager core that coordinates serial port sessions and SSH server mappings; per-port SerialSession instances that handle shared-read/queued-write with ring buffers; and two external interfaces (SSH port mapping for transparent terminal interaction, HTTP REST API for structured management and history retrieval by AI agents). Developed and tested on Windows; com0com virtual serial ports used for integration testing.

**Tech Stack:** Go 1.22+ (Windows), github.com/gliderlabs/ssh (SSH server), go.bug.st/serial (serial port, Windows COM port support), gopkg.in/yaml.v3 (config), golang.org/x/crypto/ssh (SSH client for tests), standard library net/http (HTTP API).

---

## File Structure

```
SerialGateway/
  cmd/
    serial-gateway/
      main.go                    # CLI entry point (serve command)
  internal/
    config/
      config.go                  # Config struct, YAML loading, validation
    serial/
      port.go                    # Port discovery (enumerate COM ports)
      session.go                 # SerialSession: open/close, read loop, write queue, broadcast
      ringbuffer.go              # RingBuffer: in-memory circular buffer for history
    ssh/
      server.go                  # SSHServer: per-port SSH listener lifecycle
      handler.go                 # SSH session handler: stdin→serial, serial→stdout bridge
    http/
      server.go                  # HTTP server setup, router registration
      handlers.go                # API route handlers (ports, mappings, config, tail, log)
  tests/
    vserial/
      driver.go                  # Mock serial device driver for com0com virtual ports
      driver_test.go             # Driver tests
    integration_test.go          # End-to-end integration tests (SSH + HTTP + virtual ports)
  go.mod
  go.sum
  serial-gateway.yaml            # Default config file
```

---

### Task 1: Project Scaffold and Config Module

**Files:**
- Create: `go.mod`
- Create: `cmd/serial-gateway/main.go`
- Create: `internal/config/config.go`
- Create: `serial-gateway.yaml`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Create go.mod**

```
module github.com/yicongwu/serialgateway

go 1.22
```

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go mod init github.com/yicongwu/serialgateway`

- [ ] **Step 2: Write config struct and YAML loading code**

```go
// internal/config/config.go
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type GatewayConfig struct {
	HTTPPort int `yaml:"http_port"`
}

type SerialDefaults struct {
	Baudrate  int           `yaml:"baudrate"`
	Timeout   time.Duration `yaml:"timeout"`
	ByteSize  int           `yaml:"bytesize"`
	Parity    string        `yaml:"parity"`
	StopBits  float64       `yaml:"stopbits"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type SSHAuth struct {
	Type       string   `yaml:"type"`
	Password   string   `yaml:"password"`
	PublicKeys []string `yaml:"public_keys"`
}

type SSHConfig struct {
	BasePort int     `yaml:"base_port"`
	Auth     SSHAuth `yaml:"auth"`
}

type ReconnectConfig struct {
	InitialInterval       time.Duration `yaml:"initial_interval"`
	MaxInterval           time.Duration `yaml:"max_interval"`
	DiscardInputOnDisconnect bool        `yaml:"discard_input_on_disconnect"`
}

type RingBufferConfig struct {
	MaxLines int `yaml:"max_lines"`
	MaxBytes int `yaml:"max_bytes"`
}

type PortConfig struct {
	Device   string `yaml:"device"`
	Baudrate int    `yaml:"baudrate"`
}

type Config struct {
	Gateway        GatewayConfig   `yaml:"gateway"`
	SerialDefaults SerialDefaults  `yaml:"serial_defaults"`
	RingBuffer     RingBufferConfig `yaml:"ring_buffer"`
	SSH            SSHConfig       `yaml:"ssh"`
	Reconnect      ReconnectConfig `yaml:"reconnect"`
	Ports          []PortConfig    `yaml:"ports"`
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
		cfg.RingBuffer.MaxLines = 500
	}
	if cfg.RingBuffer.MaxBytes == 0 {
		cfg.RingBuffer.MaxBytes = 65536
	}
	if cfg.SSH.BasePort == 0 {
		cfg.SSH.BasePort = 2200
	}
	if cfg.SSH.Auth.Type == "" {
		cfg.SSH.Auth.Type = "password"
	}
	if cfg.SSH.Auth.Password == "" {
		cfg.SSH.Auth.Password = "serial"
	}
	if cfg.Reconnect.InitialInterval == 0 {
		cfg.Reconnect.InitialInterval = 1 * time.Second
	}
	if cfg.Reconnect.MaxInterval == 0 {
		cfg.Reconnect.MaxInterval = 30 * time.Second
	}
}
```

- [ ] **Step 3: Write config test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("nonexistent.yaml")
	if err != nil {
		t.Fatalf("Load should not fail for nonexistent file: %v", err)
	}
	if cfg.Gateway.HTTPPort != 8080 {
		t.Errorf("expected default HTTPPort 8080, got %d", cfg.Gateway.HTTPPort)
	}
	if cfg.SerialDefaults.Baudrate != 115200 {
		t.Errorf("expected default baudrate 115200, got %d", cfg.SerialDefaults.Baudrate)
	}
	if cfg.SSH.BasePort != 2200 {
		t.Errorf("expected default SSH base port 2200, got %d", cfg.SSH.BasePort)
	}
	if cfg.RingBuffer.MaxLines != 500 {
		t.Errorf("expected default ring buffer max lines 500, got %d", cfg.RingBuffer.MaxLines)
	}
	if cfg.Reconnect.MaxInterval != 30*time.Second {
		t.Errorf("expected default reconnect max interval 30s, got %v", cfg.Reconnect.MaxInterval)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
gateway:
  http_port: 9000
serial_defaults:
  baudrate: 9600
ssh:
  base_port: 3000
  auth:
    type: "password"
    password: "testpw"
ring_buffer:
  max_lines: 100
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(content)
	tmp.Close()

	cfg, err := Load(tmp.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Gateway.HTTPPort != 9000 {
		t.Errorf("expected HTTPPort 9000, got %d", cfg.Gateway.HTTPPort)
	}
	if cfg.SerialDefaults.Baudrate != 9600 {
		t.Errorf("expected baudrate 9600, got %d", cfg.SerialDefaults.Baudrate)
	}
	if cfg.SSH.Auth.Password != "testpw" {
		t.Errorf("expected password testpw, got %s", cfg.SSH.Auth.Password)
	}
	if cfg.RingBuffer.MaxLines != 100 {
		t.Errorf("expected max lines 100, got %d", cfg.RingBuffer.MaxLines)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Create default config file**

```yaml
# serial-gateway.yaml
gateway:
  http_port: 8080

serial_defaults:
  baudrate: 115200
  timeout: 5s
  bytesize: 8
  parity: "N"
  stopbits: 1
  write_timeout: 10s

ring_buffer:
  max_lines: 500
  max_bytes: 65536

ssh:
  base_port: 2200
  auth:
    type: "password"
    password: "serial"

reconnect:
  initial_interval: 1s
  max_interval: 30s
  discard_input_on_disconnect: true

ports:
  - device: "COM3"
    baudrate: 115200
```

- [ ] **Step 6: Write main.go entry point (minimal)**

```go
// cmd/serial-gateway/main.go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/yicongwu/serialgateway/internal/config"
)

func main() {
	configPath := flag.String("config", "serial-gateway.yaml", "config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("config loaded: http_port=%d, ssh_base=%d\n", cfg.Gateway.HTTPPort, cfg.SSH.BasePort)
}
```

- [ ] **Step 7: Add yaml dependency and run**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go get gopkg.in/yaml.v3 && go build ./cmd/serial-gateway/ && ./serial-gateway.exe`
Expected: Prints config loaded message

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd/ internal/config/ serial-gateway.yaml
git commit -m "feat: project scaffold with config module"
```

---

### Task 2: Ring Buffer Module

**Files:**
- Create: `internal/serial/ringbuffer.go`
- Test: `internal/serial/ringbuffer_test.go`

- [ ] **Step 1: Write ring buffer tests**

```go
// internal/serial/ringbuffer_test.go
package serial

import (
	"fmt"
	"testing"
	"time"
)

func TestRingBufferAppendAndTail(t *testing.T) {
	rb := NewRingBuffer(100, 65536)
	for i := 0; i < 50; i++ {
		rb.Append(Entry{
			TS:   time.Now(),
			Seq:  i + 1,
			Line: fmt.Sprintf("line %d", i),
			Bytes: 7,
		})
	}
	entries := rb.Tail(10)
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
	if entries[0].Seq != 41 {
		t.Errorf("expected first entry seq=41, got %d", entries[0].Seq)
	}
	if entries[9].Seq != 50 {
		t.Errorf("expected last entry seq=50, got %d", entries[9].Seq)
	}
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(5, 65536)
	for i := 0; i < 10; i++ {
		rb.Append(Entry{Seq: i + 1, Line: "x", TS: time.Now(), Bytes: 1})
	}
	entries := rb.Tail(100)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries after overflow, got %d", len(entries))
	}
	if entries[0].Seq != 6 {
		t.Errorf("expected oldest kept seq=6, got %d", entries[0].Seq)
	}
}

func TestRingBufferAllEntries(t *testing.T) {
	rb := NewRingBuffer(100, 65536)
	for i := 0; i < 5; i++ {
		rb.Append(Entry{Seq: i + 1, Line: "x", TS: time.Now(), Bytes: 1})
	}
	all := rb.All()
	if len(all) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(all))
	}
}

func TestRingBufferByteLimit(t *testing.T) {
	rb := NewRingBuffer(1000, 20)
	for i := 0; i < 10; i++ {
		rb.Append(Entry{Seq: i + 1, Line: "1234567", TS: time.Now(), Bytes: 7})
	}
	entries := rb.Tail(1000)
	// Each entry is 7 bytes, limit is 20, so only ~2-3 recent entries survive
	if len(entries) > 3 {
		t.Fatalf("expected at most 3 entries with byte limit 20, got %d", len(entries))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/serial/ -v -run RingBuffer`
Expected: FAIL — `NewRingBuffer`, `Entry`, `Append`, `Tail`, `All` not defined

- [ ] **Step 3: Implement ring buffer**

```go
// internal/serial/ringbuffer.go
package serial

import (
	"sync"
	"time"
)

type Entry struct {
	TS   time.Time `json:"ts"`
	Seq  int       `json:"seq"`
	Line string    `json:"line"`
	Bytes int      `json:"bytes"`
}

type RingBuffer struct {
	mu       sync.Mutex
	entries  []Entry
	maxLines int
	maxBytes int
	totalBytes int
}

func NewRingBuffer(maxLines, maxBytes int) *RingBuffer {
	return &RingBuffer{
		entries:  make([]Entry, 0, maxLines),
		maxLines: maxLines,
		maxBytes: maxBytes,
	}
}

func (rb *RingBuffer) Append(e Entry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.entries = append(rb.entries, e)
	rb.totalBytes += e.Bytes
	for len(rb.entries) > rb.maxLines || rb.totalBytes > rb.maxBytes {
		removed := rb.entries[0]
		rb.entries = rb.entries[1:]
		rb.totalBytes -= removed.Bytes
	}
}

func (rb *RingBuffer) Tail(n int) []Entry {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if n > len(rb.entries) {
		n = len(rb.entries)
	}
	result := make([]Entry, n)
	copy(result, rb.entries[len(rb.entries)-n:])
	return result
}

func (rb *RingBuffer) All() []Entry {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	result := make([]Entry, len(rb.entries))
	copy(result, rb.entries)
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/serial/ -v -run RingBuffer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/serial/ringbuffer.go internal/serial/ringbuffer_test.go
git commit -m "feat: ring buffer module for serial port history"
```

---

### Task 3: Serial Port Discovery and Enumeration

**Files:**
- Create: `internal/serial/port.go`
- Test: `internal/serial/port_test.go`

- [ ] **Step 1: Write port discovery tests**

```go
// internal/serial/port_test.go
package serial

import (
	"testing"
)

func TestListPorts(t *testing.T) {
	ports, err := ListPorts()
	if err != nil {
		t.Fatalf("ListPorts failed: %v", err)
	}
	// On a machine without serial ports, this returns an empty list — that's fine
	t.Logf("found %d ports", len(ports))
	for _, p := range ports {
		t.Logf("  %s: %s (%s)", p.Device, p.Description, p.HWID)
	}
}

func TestPortInfoFields(t *testing.T) {
	pi := PortInfo{
		Device:      "COM3",
		Description: "USB Serial Device",
		HWID:        "USB VID:PID",
	}
	if pi.Device != "COM3" {
		t.Errorf("expected COM3, got %s", pi.Device)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/serial/ -v -run Port`
Expected: FAIL — `ListPorts`, `PortInfo` not defined

- [ ] **Step 3: Implement port discovery**

```go
// internal/serial/port.go
package serial

import (
	"go.bug.st/serial/enumerator"
)

type PortInfo struct {
	Device      string `json:"device"`
	Description string `json:"description"`
	HWID        string `json:"hwid"`
}

func ListPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	result := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		result = append(result, PortInfo{
			Device:      p.Name,
			Description: p.Description,
			HWID:        p.HardwareID,
		})
	}
	return result, nil
}
```

- [ ] **Step 4: Add serial dependency and run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go get go.bug.st/serial && go test ./internal/serial/ -v -run Port`
Expected: PASS (may show 0 ports on a machine without serial hardware)

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/serial/port.go internal/serial/port_test.go
git commit -m "feat: serial port discovery and enumeration"
```

---

### Task 4: SerialSession — Open, Close, Write Queue, Read Broadcast

**Files:**
- Create: `internal/serial/session.go`
- Test: `internal/serial/session_test.go`

- [ ] **Step 1: Write session tests**

```go
// internal/serial/session_test.go
package serial

import (
	"testing"
	"time"

	"github.com/yicongwu/serialgateway/internal/config"
)

func TestSessionOpenClose(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := NewSerialSession("COM_TEST", cfg)
	err := sess.Open()
	// COM_TEST doesn't exist, expect error
	if err == nil {
		sess.Close()
	}
	// Test passes regardless — we validate the struct works
	if sess.Device() != "COM_TEST" {
		t.Errorf("expected device COM_TEST, got %s", sess.Device())
	}
}

func TestSessionWriteQueue(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := NewSerialSession("COM_FAKE", cfg)
	// Without a real port, we test queue mechanics
	ch := sess.WriteChannel()
	if ch == nil {
		t.Fatal("write channel should not be nil")
	}
}

func TestSessionBroadcastSubscriber(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := NewSerialSession("COM_FAKE", cfg)
	sub := sess.Subscribe()
	defer sess.Unsubscribe(sub)
	if sub == nil {
		t.Fatal("subscriber channel should not be nil")
	}
}

func TestRingBufferIntegration(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := NewSerialSession("COM_FAKE", cfg)
	rb := sess.RingBuffer()
	if rb == nil {
		t.Fatal("ring buffer should not be nil")
	}
	rb.Append(Entry{Seq: 1, Line: "hello", TS: time.Now(), Bytes: 5})
	entries := rb.Tail(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Line != "hello" {
		t.Errorf("expected line 'hello', got '%s'", entries[0].Line)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/serial/ -v -run Session`
Expected: FAIL — types and methods not defined

- [ ] **Step 3: Implement SerialSession**

```go
// internal/serial/session.go
package serial

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/yicongwu/serialgateway/internal/config"
)

type WriteRequest struct {
	Data   []byte
	Done   chan error
}

type SerialSession struct {
	device     string
	baudrate   int
	cfg        *config.Config

	mu         sync.Mutex
	port       serial.Port
	connected  bool

	writeCh    chan WriteRequest
	subscribers map[chan string]struct{}
	subMu      sync.RWMutex

	ringBuffer *RingBuffer
	seqCounter int

	stopCh     chan struct{}
}

func NewSerialSession(device string, cfg *config.Config) *SerialSession {
	baudrate := cfg.SerialDefaults.Baudrate
	for _, p := range cfg.Ports {
		if p.Device == device && p.Baudrate != 0 {
			baudrate = p.Baudrate
		}
	}
	return &SerialSession{
		device:      device,
		baudrate:    baudrate,
		cfg:         cfg,
		writeCh:     make(chan WriteRequest, 64),
		subscribers: make(map[chan string]struct{}),
		ringBuffer:  NewRingBuffer(cfg.RingBuffer.MaxLines, cfg.RingBuffer.MaxBytes),
		stopCh:      make(chan struct{}),
	}
}

func (s *SerialSession) Device() string { return s.device }
func (s *SerialSession) WriteChannel() chan WriteRequest { return s.writeCh }
func (s *SerialSession) RingBuffer() *RingBuffer { return s.ringBuffer }

func (s *SerialSession) Baudrate() int { return s.baudrate }

func (s *SerialSession) Open() error {
	s.mu.Lock()
	if s.connected {
		s.mu.Unlock()
		return nil
	}
	mode := &serial.Mode{
		BaudRate: s.baudrate,
		DataBits: s.cfg.SerialDefaults.ByteSize,
		Parity:   serial.NoParity,
		StopBits: serial.StopBits1,
	}
	switch s.cfg.SerialDefaults.Parity {
	case "E": mode.Parity = serial.EvenParity
	case "O": mode.Parity = serial.OddParity
	case "M": mode.Parity = serial.MarkParity
	case "S": mode.Parity = serial.SpaceParity
	}
	switch s.cfg.SerialDefaults.StopBits {
	case 1.5: mode.StopBits = serial.StopBits1_5
	case 2: mode.StopBits = serial.StopBits2
	}
	p, err := serial.Open(s.device, mode)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("open %s: %w", s.device, err)
	}
	s.port = p
	s.connected = true
	s.mu.Unlock()
	go s.readLoop()
	go s.writeLoop()
	return nil
}

func (s *SerialSession) Close() {
	s.mu.Lock()
	if s.port != nil {
		s.port.Close()
	}
	s.connected = false
	s.mu.Unlock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *SerialSession) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *SerialSession) Subscribe() chan string {
	ch := make(chan string, 64)
	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()
	return ch
}

func (s *SerialSession) Unsubscribe(ch chan string) {
	s.subMu.Lock()
	delete(s.subscribers, ch)
	s.subMu.Unlock()
	close(ch)
}

func (s *SerialSession) readLoop() {
	buf := make([]byte, 1024)
	lineBuf := []byte{}
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		s.mu.Lock()
		p := s.port
		s.mu.Unlock()
		if p == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		n, err := p.Read(buf)
		if err != nil {
			if err == io.EOF {
				s.broadcast("[serial disconnected - waiting for reconnect...]")
				s.handleDisconnect()
				continue
			}
			log.Printf("serial read error on %s: %v", s.device, err)
			s.broadcast(fmt.Sprintf("[serial error: %v]", err))
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if n == 0 {
			continue
		}
		lineBuf = append(lineBuf, buf[:n]...)
		for {
			idx := bytes.IndexByte(lineBuf, '\n')
			if idx < 0 {
				break
			}
			line := string(lineBuf[:idx])
			lineBuf = lineBuf[idx+1:]
			line = strings.TrimRight(line, "\r")
			s.mu.Lock()
			s.seqCounter++
			seq := s.seqCounter
			s.mu.Unlock()
			entry := Entry{
				TS:   time.Now(),
				Seq:  seq,
				Line: line,
				Bytes: len(line),
			}
			s.ringBuffer.Append(entry)
			s.broadcast(line)
		}
	}
}

func (s *SerialSession) writeLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case req := <-s.writeCh:
			s.mu.Lock()
			p := s.port
			s.mu.Unlock()
			if p == nil {
				if s.cfg.Reconnect.DiscardInputOnDisconnect {
					req.Done <- fmt.Errorf("serial disconnected, input discarded")
				}
				continue
			}
			_, err := p.Write(req.Data)
			req.Done <- err
		}
	}
}

func (s *SerialSession) broadcast(msg string) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	for ch := range s.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *SerialSession) handleDisconnect() {
	s.mu.Lock()
	s.port.Close()
	s.port = nil
	s.connected = false
	s.mu.Unlock()
	go s.reconnectLoop()
}

func (s *SerialSession) reconnectLoop() {
	interval := s.cfg.Reconnect.InitialInterval
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		time.Sleep(interval)
		err := s.Open()
		if err == nil {
			s.broadcast("[serial reconnected]")
			return
		}
		log.Printf("reconnect attempt for %s failed: %v", s.device, err)
		interval *= 2
		if interval > s.cfg.Reconnect.MaxInterval {
			interval = s.cfg.Reconnect.MaxInterval
		}
	}
}

- [ ] **Step 4: Run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/serial/ -v -run Session`
Expected: PASS (COM_TEST/COM_FAKE open fails gracefully, struct accessors work)

- [ ] **Step 5: Commit**

```bash
git add internal/serial/session.go internal/serial/session_test.go
git commit -m "feat: serial session with write queue, read broadcast, ring buffer"
```

---

### Task 5: SSH Server Module

**Files:**
- Create: `internal/ssh/server.go`
- Create: `internal/ssh/handler.go`
- Test: `internal/ssh/server_test.go`

- [ ] **Step 1: Write SSH server tests**

```go
// internal/ssh/server_test.go
package ssh

import (
	"testing"

	"github.com/yicongwu/serialgateway/internal/config"
)

func TestSSHPortMapping(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	mapping := PortMapping("COM3", cfg)
	if mapping != 2203 {
		t.Errorf("expected COM3→2203, got %d", mapping)
	}
	mapping8 := PortMapping("COM8", cfg)
	if mapping8 != 2208 {
		t.Errorf("expected COM8→2208, got %d", mapping8)
	}
}

func TestNewSSHServer(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	srv, err := NewSSHServer("COM3", cfg)
	if err != nil {
		t.Fatalf("NewSSHServer failed: %v", err)
	}
	if srv.Port() != 2203 {
		t.Errorf("expected port 2203, got %d", srv.Port())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/ssh/ -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement PortMapping and SSHServer**

```go
// internal/ssh/server.go
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
	device   string
	sshPort  int
	cfg      *config.Config
	session  *serial.SerialSession
	server   *gliderssh.Server
	mu       sync.Mutex
	running  bool
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

func (s *SSHServer) Port() int { return s.sshPort }
func (s *SSHServer) Device() string { return s.device }
func (s *SSHServer) Session() *serial.SerialSession { return s.session }
func (s *SSHServer) IsRunning() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.running }

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
	// Note: serial port is opened lazily on first SSH connection (see handler.go),
	// not eagerly on AutoStart. This is by design — unused ports don't hold resources.
	// gliderlabs/ssh auto-generates an ephemeral host key if none is provided.
	// This is fine for a local gateway — clients will see a new key each restart.
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
```

- [ ] **Step 4: Write SSH handler**

```go
// internal/ssh/handler.go
package ssh

import (
	"fmt"
	"io"
	"log"
	"time"

	gliderssh "github.com/gliderlabs/ssh"

	"github.com/yicongwu/serialgateway/internal/serial"
)

func (s *SSHServer) handleSession(sess gliderssh.Session) {
	logf := func(format string, args ...interface{}) {
		log.Printf("ssh:%s %s", s.device, fmt.Sprintf(format, args...))
	}
	logf("connected: %s", sess.RemoteAddr())

	if !s.session.IsConnected() {
		err := s.session.Open()
		if err != nil {
			fmt.Fprintf(sess, "[serial open failed: %v]\n", err)
			sess.Close()
			logf("serial open failed: %v", err)
			return
		}
	}

	sub := s.session.Subscribe()
	defer s.session.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		for msg := range sub {
			fmt.Fprintf(sess, "%s\n", msg)
		}
		close(done)
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := sess.Read(buf)
			if err != nil {
				if err != io.EOF {
					logf("read error: %v", err)
				}
				return
			}
			req := serial.WriteRequest{
				Data: buf[:n],
				Done: make(chan error, 1),
			}
			s.session.WriteChannel() <- req
			select {
			case writeErr := <-req.Done:
				if writeErr != nil {
					fmt.Fprintf(sess, "[write error: %v]\n", writeErr)
				}
			case <-time.After(s.cfg.SerialDefaults.WriteTimeout):
				fmt.Fprintf(sess, "[write timeout]\n")
			}
		}
	}()

	<-done
	logf("disconnected: %s", sess.RemoteAddr())
}
```

- [ ] **Step 5: Add gliderlabs/ssh dependency and run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go get github.com/gliderlabs/ssh && go test ./internal/ssh/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ssh/
git commit -m "feat: SSH server module with port mapping and session handler"
```

---

### Task 6: HTTP API Server

**Files:**
- Create: `internal/http/server.go`
- Create: `internal/http/handlers.go`
- Test: `internal/http/handlers_test.go`

- [ ] **Step 1: Write HTTP handler tests**

```go
// internal/http/handlers_test.go
package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
)

func TestGetPorts(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/ports", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	ports, ok := resp["ports"]
	if !ok {
		t.Fatal("response missing 'ports' key")
	}
	t.Logf("ports: %v", ports)
}

func TestGetMappings(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/mappings", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	mappings, ok := resp["mappings"]
	if !ok {
		t.Fatal("response missing 'mappings' key")
	}
	t.Logf("mappings: %v", mappings)
}

func TestGetConfig(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["gateway"] == nil {
		t.Fatal("response missing 'gateway' key")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/http/ -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement Gateway and HTTP server (with PortManager)**

```go
// internal/http/server.go
package http

import (
	"fmt"
	"log"
	"net/http"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
)

type Gateway struct {
	cfg *config.Config
	pm  *core.PortManager
}

func NewGatewayWithManager(cfg *config.Config, pm *core.PortManager) *Gateway {
	return &Gateway{
		cfg: cfg,
		pm:  pm,
	}
}

func (gw *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ports", gw.handleGetPorts)
	mux.HandleFunc("/api/mappings", gw.handleMappings)
	mux.HandleFunc("/api/mappings/", gw.handleMappingDetail)
	mux.HandleFunc("/api/config", gw.handleConfig)
	return mux
}

func (gw *Gateway) StartHTTP() {
	addr := fmt.Sprintf(":%d", gw.cfg.Gateway.HTTPPort)
	log.Printf("HTTP API listening on %s", addr)
	http.ListenAndServe(addr, gw.Handler())
}
```

- [ ] **Step 4: Implement handlers (complete, with tail/log)**

```go
// internal/http/handlers.go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/serial"
	"github.com/yicongwu/serialgateway/internal/ssh"
)

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func (gw *Gateway) handleGetPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	ports, err := serial.ListPorts()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	result := make([]map[string]interface{}, 0, len(ports))
	for _, p := range ports {
		baudrate := gw.cfg.SerialDefaults.Baudrate
		for _, cp := range gw.cfg.Ports {
			if cp.Device == p.Device && cp.Baudrate != 0 {
				baudrate = cp.Baudrate
			}
		}
		sshPort := ssh.PortMapping(p.Device, gw.cfg)
		status := "inactive"
		srv := gw.pm.GetServer(p.Device)
		if srv != nil && srv.IsRunning() {
			status = "active"
		}
		result = append(result, map[string]interface{}{
			"device":      p.Device,
			"description": p.Description,
			"hwid":        p.HWID,
			"baudrate":    baudrate,
			"ssh_port":    sshPort,
			"status":      status,
		})
	}
	writeJSON(w, 200, map[string]interface{}{"ports": result})
}

func (gw *Gateway) handleMappings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		gw.handleGetMappings(w, r)
	case "POST":
		gw.handleCreateMapping(w, r)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (gw *Gateway) handleGetMappings(w http.ResponseWriter, r *http.Request) {
	mappings := gw.pm.Mappings()
	result := make([]map[string]interface{}, 0, len(mappings))
	for _, m := range mappings {
		srv := gw.pm.GetServer(m["serial_port"].(string))
		result = append(result, map[string]interface{}{
			"serial_port": m["serial_port"],
			"ssh_port":    m["ssh_port"],
			"connections": 0,
			"baudrate":    srv.Session().Baudrate(),
			"connected":   srv.Session().IsConnected(),
		})
	}
	writeJSON(w, 200, map[string]interface{}{"mappings": result})
}

func (gw *Gateway) handleCreateMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Device == "" {
		writeJSON(w, 400, map[string]string{"error": "device is required"})
		return
	}
	srv, err := gw.pm.AddMapping(req.Device)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, 201, map[string]interface{}{
		"serial_port": req.Device,
		"ssh_port":    srv.Port(),
		"status":      "active",
	})
}

func (gw *Gateway) handleMappingDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/api/mappings/"), "/")
	device := parts[0]

	if r.Method == "DELETE" && len(parts) == 1 {
		err := gw.pm.RemoveMapping(device)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"device": device,
			"status": "removed",
		})
		return
	}

	srv := gw.pm.GetServer(device)
	if srv == nil {
		writeJSON(w, 404, map[string]string{"error": "mapping not found for " + device})
		return
	}
	if len(parts) >= 2 {
		switch parts[1] {
		case "tail":
			gw.handleTail(w, r, srv)
		case "log":
			gw.handleLog(w, r, srv)
		default:
			writeJSON(w, 404, map[string]string{"error": "unknown sub-path"})
		}
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"serial_port": srv.Device(),
		"ssh_port":    srv.Port(),
		"connected":   srv.Session().IsConnected(),
	})
}

func (gw *Gateway) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, 200, gw.cfg)
	case "PUT":
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid config"})
			return
		}
		config.ApplyDefaults(&newCfg)
		gw.cfg = &newCfg
		writeJSON(w, 200, gw.cfg)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}
```

- [ ] **Step 5: Add tail and log endpoint handlers**

```go
// Add to internal/http/handlers.go

func (gw *Gateway) handleTail(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	lines := 200
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			lines = n
		}
	}
	entries := srv.Session().RingBuffer().Tail(lines)
	writeJSON(w, 200, map[string]interface{}{
		"device":         srv.Device(),
		"lines":          lines,
		"count":          len(entries),
		"items":          entries,
	})
}

func (gw *Gateway) handleLog(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "text"
	}
	entries := srv.Session().RingBuffer().All()
	if format == "jsonl" {
		var lines []string
		for _, e := range entries {
			b, _ := json.Marshal(e)
			lines = append(lines, string(b))
		}
		content := strings.Join(lines, "\n")
		writeJSON(w, 200, map[string]interface{}{
			"device":  srv.Device(),
			"format":  "jsonl",
			"content": content,
			"bytes":   len(content),
		})
		return
	}
	var textLines []string
	for _, e := range entries {
		textLines = append(textLines, fmt.Sprintf("%s [%s] %s", e.TS.Format(time.RFC3339), srv.Device(), e.Line))
	}
	content := strings.Join(textLines, "\n")
	writeJSON(w, 200, map[string]interface{}{
		"device":  srv.Device(),
		"format":  "text",
		"content": content,
		"bytes":   len(content),
	})
}
```

- [ ] **Step 6: Run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/http/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/http/ internal/serial/session.go internal/config/config.go
git commit -m "feat: HTTP API with ports, mappings, config, tail, and log endpoints"
```

---

### Task 7: PortManager — Orchestration Layer

**Files:**
- Create: `internal/core/portmanager.go`
- Test: `internal/core/portmanager_test.go`

- [ ] **Step 1: Write PortManager tests**

```go
// internal/core/portmanager_test.go
package core

import (
	"testing"

	"github.com/yicongwu/serialgateway/internal/config"
)

func TestNewPortManager(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	if pm == nil {
		t.Fatal("PortManager should not be nil")
	}
}

func TestStartupConfiguredPorts(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{Device: "COM3", Baudrate: 115200},
			{Device: "COM4", Baudrate: 9600},
		},
	}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	mappings := pm.Mappings()
	// COM3 and COM4 may not be physically available, so they may be inactive
	t.Logf("mappings: %v", mappings)
}

func TestAutoStart(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	err := pm.AutoStart()
	if err != nil {
		t.Logf("AutoStart returned error (expected if no ports available): %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/core/ -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement PortManager**

```go
// internal/core/portmanager.go
package core

import (
	"fmt"
	"log"
	"sync"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/ssh"
)

type PortManager struct {
	cfg     *config.Config
	servers map[string]*ssh.SSHServer
	mu      sync.RWMutex
}

func NewPortManager(cfg *config.Config) *PortManager {
	return &PortManager{
		cfg:     cfg,
		servers: make(map[string]*ssh.SSHServer),
	}
}

func (pm *PortManager) AutoStart() error {
	for _, portCfg := range pm.cfg.Ports {
		if _, exists := pm.servers[portCfg.Device]; exists {
			continue
		}
		srv, err := ssh.NewSSHServer(portCfg.Device, pm.cfg)
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
		pm.mu.Unlock()
		log.Printf("started mapping: %s → :%d", portCfg.Device, srv.Port())
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
	srv, err := ssh.NewSSHServer(device, pm.cfg)
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
```

- [ ] **Step 4: Run tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/core/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat: PortManager orchestration layer"
```

---

### Task 8: Main Entry Point — Wire Everything Together

**Files:**
- Modify: `cmd/serial-gateway/main.go`

- [ ] **Step 1: Rewrite main.go to start all components**

```go
// cmd/serial-gateway/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
	"github.com/yicongwu/serialgateway/internal/http"
)

func main() {
	configPath := flag.String("config", "serial-gateway.yaml", "config file path")
	httpPort := flag.Int("http-port", 0, "override HTTP port (0 = use config)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if *httpPort != 0 {
		cfg.Gateway.HTTPPort = *httpPort
	}

	pm := core.NewPortManager(cfg)
	if err := pm.AutoStart(); err != nil {
		log.Printf("auto-start warning: %v", err)
	}

	gw := http.NewGatewayWithManager(cfg, pm)
	go gw.StartHTTP()

	log.Printf("SerialGateway started — HTTP :%d, SSH base port %d", cfg.Gateway.HTTPPort, cfg.SSH.BasePort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	log.Println("shutting down...")
	pm.Shutdown()
}
```

Need to update `http.NewGateway` to accept PortManager instead of maintaining its own server map:

```go
// Update internal/http/server.go
type Gateway struct {
	cfg *config.Config
	pm  *core.PortManager
}

func NewGatewayWithManager(cfg *config.Config, pm *core.PortManager) *Gateway {
	return &Gateway{
		cfg: cfg,
		pm:  pm,
	}
}
```

- [ ] **Step 2: Build and verify**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go build ./cmd/serial-gateway/ && ./serial-gateway.exe --config serial-gateway.yaml`
Expected: Starts, logs config, HTTP port, SSH base port. May warn about COM ports not found.

- [ ] **Step 3: Test HTTP API**

Run: `curl http://localhost:8080/api/ports` (after starting gateway in background)
Expected: JSON response with ports list

- [ ] **Step 4: Commit**

```bash
git add cmd/serial-gateway/main.go internal/http/
git commit -m "feat: main entry point wiring all components together"
```

---

### Task 9: com0com Virtual Serial Port Setup + Mock Device Driver

**Prerequisite:** Install com0com (https://com0com.sourceforge.net/) on the test machine.

**Files:**
- Create: `tests/vserial/driver.go`
- Create: `tests/vserial/driver_test.go`

com0com creates virtual COM port pairs (e.g., COM10↔COM11). The driver connects to one end of the pair (COM11) and simulates device behavior: continuous log output, responding to commands. The gateway connects to the other end (COM10) via SSH mapping.

- [ ] **Step 1: Install com0com and create virtual port pairs**

Download and install com0com from https://com0com.sourceforge.net/

After installation, create virtual port pairs:

```
install PortName=COM10 PortName=COM11
install PortName=COM20 PortName=COM21
```

Verify: `mode COM10` and `mode COM11` should show the ports exist (or use `list` in com0com setup tool).

- [ ] **Step 2: Write the mock device driver**

```go
// tests/vserial/driver.go
package vserial

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

type MockDevice struct {
	port       serial.Port
	deviceName string

	mu         sync.Mutex
	outputLog  []string
	writeLog   []string
	stopCh     chan struct{}
}

func NewMockDevice(portName string) (*MockDevice, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
	}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", portName, err)
	}
	d := &MockDevice{
		port:       p,
		deviceName: portName,
		stopCh:     make(chan struct{}),
	}
	return d, nil
}

func (d *MockDevice) StartContinuousOutput(interval time.Duration) {
	go func() {
		counter := 0
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-d.stopCh:
				return
			case <-ticker.C:
				line := fmt.Sprintf("[device-%s] log line %d — heartbeat at %s", d.deviceName, counter, time.Now().Format(time.RFC3339))
				d.mu.Lock()
				d.outputLog = append(d.outputLog, line)
				d.mu.Unlock()
				d.port.Write([]byte(line + "\n"))
				counter++
			}
		}
	}()
}

func (d *MockDevice) StartCommandResponder() {
	buf := make([]byte, 256)
	lineBuf := []byte{}
	for {
		select {
		case <-d.stopCh:
			return
		default:
		}
		n, err := d.port.Read(buf)
		if err != nil {
			log.Printf("mock device %s read error: %v", d.deviceName, err)
			continue
		}
		lineBuf = append(lineBuf, buf[:n]...)
		for {
			idx := bytes.IndexByte(lineBuf, '\n')
			if idx < 0 {
				break
			}
			cmd := string(lineBuf[:idx])
			lineBuf = lineBuf[idx+1:]
			cmd = strings.TrimRight(cmd, "\r")
			d.mu.Lock()
			d.writeLog = append(d.writeLog, cmd)
			d.mu.Unlock()
			response := d.handleCommand(cmd)
			d.port.Write([]byte(response + "\n"))
		}
	}
}

func (d *MockDevice) handleCommand(cmd string) string {
	switch cmd {
	case "help":
		return "Available commands: help, status, reboot, echo <msg>"
	case "status":
		return fmt.Sprintf("device %s running, uptime %s", d.deviceName, time.Now().Format(time.RFC3339))
	case "reboot":
		return "rebooting..."
	default:
		if strings.HasPrefix(cmd, "echo ") {
			return strings.TrimPrefix(cmd, "echo ")
		}
		return fmt.Sprintf("unknown command: %s", cmd)
	}
}

func (d *MockDevice) Close() {
	close(d.stopCh)
	d.port.Close()
}

func (d *MockDevice) OutputLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.outputLog
}

func (d *MockDevice) WriteLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeLog
}
```

- [ ] **Step 3: Write driver test**

```go
// tests/vserial/driver_test.go
package vserial

import (
	"strings"
	"testing"
	"time"
)

func TestMockDeviceCreation(t *testing.T) {
	d, err := NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available (com0com not installed or port pair not created): %v", err)
	}
	defer d.Close()
	d.StartContinuousOutput(100 * time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	log := d.OutputLog()
	if len(log) < 3 {
		t.Fatalf("expected at least 3 log lines after 500ms, got %d", len(log))
	}
	t.Logf("device produced %d log lines", len(log))
}

func TestMockDeviceCommandResponse(t *testing.T) {
	d, err := NewMockDevice("COM21")
	if err != nil {
		t.Skipf("COM21 not available: %v", err)
	}
	defer d.Close()
	go d.StartCommandResponder()

	// Write a command from the "other side" — need a second connection
	other, err := NewMockDevice("COM20")
	if err != nil {
		t.Skipf("COM20 not available: %v", err)
	}
	defer other.Close()

	other.port.Write([]byte("help\n"))
	time.Sleep(200 * time.Millisecond)

	writes := d.WriteLog()
	if len(writes) == 0 {
		t.Fatal("expected at least 1 command received")
	}
	if !strings.Contains(writes[0], "help") {
		t.Errorf("expected 'help' in write log, got '%s'", writes[0])
	}
}
```

- [ ] **Step 4: Run driver tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./tests/vserial/ -v`
Expected: PASS if com0com installed and port pairs created; SKIP otherwise

- [ ] **Step 5: Commit**

```bash
git add tests/vserial/
git commit -m "feat: mock serial device driver for com0com virtual port testing"
```

---

### Task 10: Integration Test — SSH + HTTP End-to-End with Virtual Ports

**Files:**
- Create: `tests/integration_test.go`
- Modify: `internal/config/config.go` (add `ApplyDefaults` public function)
- Modify: `internal/serial/session.go` (add `Baudrate()` accessor)
- Modify: `internal/http/server.go` (add `NewGatewayWithManager`)

**Prerequisite:** com0com installed with COM10↔COM11 pair created (Task 9).

- [ ] **Step 1: Write full integration test**

```go
// tests/integration_test.go
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
	sgwhttp "github.com/yicongwu/serialgateway/internal/http"
	"github.com/yicongwu/serialgateway/tests/vserial"
)

func setupGateway(t *testing.T, httpPort, sshBase int) (*core.PortManager, *sgwhttp.Gateway) {
	t.Helper()
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{Device: "COM10", Baudrate: 115200},
		},
	}
	config.ApplyDefaults(cfg)
	cfg.Gateway.HTTPPort = httpPort
	cfg.SSH.BasePort = sshBase

	pm := core.NewPortManager(cfg)
	pm.AutoStart()
	gw := sgwhttp.NewGatewayWithManager(cfg, pm)
	return pm, gw
}

func TestHTTPAPIPortsEndpoint(t *testing.T) {
	pm, gw := setupGateway(t, 18080, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:18080/api/ports"))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ports"] == nil {
		t.Fatal("response missing 'ports' key")
	}
	t.Logf("ports response: %v", result["ports"])
}

func TestSSHConnectionToVirtualPort(t *testing.T) {
	// Start mock device on COM11 (paired with COM10)
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available (com0com not installed): %v", err)
	}
	defer device.Close()
	device.StartContinuousOutput(200 * time.Millisecond)
	go device.StartCommandResponder()

	pm, gw := setupGateway(t, 18081, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()

	// Auto-start should create mapping for COM10
	time.Sleep(1 * time.Second)

	// Connect via SSH to COM10's mapped port (32000 + 10 = 32010)
	sshConfig := &gossh.ClientConfig{
		User: "serial",
		Auth: []gossh.AuthMethod{
			gossh.Password("serial"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	conn, err := gossh.Dial("tcp", "localhost:32010", sshConfig)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("SSH session failed: %v", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe failed: %v", err)
	}
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf

	if err := session.Shell(); err != nil {
		t.Fatalf("session.Shell() failed: %v", err)
	}

	// Wait for some output from the device
	time.Sleep(2 * time.Second)

	// Send "help" command
	fmt.Fprintf(stdin, "help\n")
	time.Sleep(500 * time.Millisecond)

	output := stdoutBuf.String()
	if len(output) == 0 {
		t.Fatal("expected output from SSH session, got empty")
	}
	t.Logf("SSH output:\n%s", output)

	// Verify we received device log lines
	if !strings.Contains(output, "log line") {
		t.Error("expected 'log line' in output — device heartbeat not received")
	}

	// Verify command response appeared
	if !strings.Contains(output, "Available commands") {
		t.Error("expected command response 'Available commands' in output")
	}
}

func TestHTTPHistoryTailEndpoint(t *testing.T) {
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available: %v", err)
	}
	defer device.Close()
	device.StartContinuousOutput(100 * time.Millisecond)

	pm, gw := setupGateway(t, 18082, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(2 * time.Second)

	// Query history via HTTP
	resp, err := http.Get(fmt.Sprintf("http://localhost:18082/api/mappings/COM10/tail?lines=10"))
	if err != nil {
		t.Fatalf("HTTP tail request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	items, ok := result["items"]
	if !ok {
		t.Fatal("response missing 'items' key")
	}
	t.Logf("tail items: %v", items)
}
```

- [ ] **Step 2: Add golang.org/x/crypto/ssh dependency**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go get golang.org/x/crypto/ssh`

- [ ] **Step 3: Run integration tests (requires com0com)**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./tests/ -v`
Expected: PASS (with com0com), SKIP (without com0com)

- [ ] **Step 4: Commit**

```bash
git add tests/integration_test.go go.mod go.sum
git commit -m "feat: integration tests with com0com virtual ports and mock device driver"
```

---

### Task 11: Final Assembly and End-to-End Verification

**Files:**
- All files reviewed for consistency

- [ ] **Step 1: Run all unit tests**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./internal/... -v`
Expected: All PASS

- [ ] **Step 2: Run integration tests (requires com0com)**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go test ./tests/ -v`
Expected: PASS with com0com, SKIP without

- [ ] **Step 3: Build the binary**

Run: `cd C:\Users\yicong.wu\Documents\SerialGateway && go build -o serial-gateway.exe ./cmd/serial-gateway/`
Expected: Binary built successfully

- [ ] **Step 4: Manual smoke test with virtual ports**

1. Ensure com0com has COM10↔COM11 pair
2. Start mock device: `go run ./tests/vserial/cmd/main.go -port COM11` (a small main that starts the driver)
3. Start gateway: `./serial-gateway.exe --config serial-gateway.yaml` (config includes COM10)
4. Query ports: `curl http://localhost:8080/api/ports`
5. Query history: `curl http://localhost:8080/api/mappings/COM10/tail?lines=50`
6. SSH connect: `ssh -p 2210 serial@localhost` (password: serial) — should see device heartbeat logs
7. Send command: type `help` in SSH session — should see "Available commands" response

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: SerialGateway complete — SSH port mapping + HTTP management API + com0com integration tests"
```