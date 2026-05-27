package serial

import (
	"bytes"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/zisen123/serialgateway/internal/config"
)

var AnsiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b[^[A-Za-z]?[A-Za-z]`)

// Session is the common interface for device I/O sessions (serial or ADB).
type Session interface {
	Device() string
	Baudrate() int
	Open() error
	Close()
	IsConnected() bool
	Subscribe() chan string
	Unsubscribe(chan string)
	WriteChannel() chan WriteRequest
	RingBuffer() *RingBuffer
}

var _ Session = (*SerialSession)(nil)

type WriteRequest struct {
	Data []byte
	Done chan error
}

type SerialSession struct {
	device   string
	baudrate int
	cfg      *config.Config

	mu        sync.Mutex
	port      serial.Port
	connected bool

	writeCh     chan WriteRequest
	subscribers map[chan string]struct{}
	subMu       sync.RWMutex

	ringBuffer *RingBuffer
	seqCounter int

	stopCh chan struct{}
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

func (s *SerialSession) Device() string                { return s.device }
func (s *SerialSession) WriteChannel() chan WriteRequest { return s.writeCh }
func (s *SerialSession) RingBuffer() *RingBuffer         { return s.ringBuffer }
func (s *SerialSession) Baudrate() int                   { return s.baudrate }

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
		StopBits: serial.OneStopBit,
	}
	switch s.cfg.SerialDefaults.Parity {
	case "E":
		mode.Parity = serial.EvenParity
	case "O":
		mode.Parity = serial.OddParity
	case "M":
		mode.Parity = serial.MarkParity
	case "S":
		mode.Parity = serial.SpaceParity
	}
	switch s.cfg.SerialDefaults.StopBits {
	case 1.5:
		mode.StopBits = serial.OnePointFiveStopBits
	case 2:
		mode.StopBits = serial.TwoStopBits
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
		s.port = nil
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
	stopCh := s.stopCh
	buf := make([]byte, 1024)
	lineBuf := []byte{}
	for {
		select {
		case <-stopCh:
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
			log.Printf("serial read error on %s: %v", s.device, err)
			s.broadcast("[serial disconnected - waiting for reconnect...]\n")
			s.handleDisconnect()
			return
		}
		if n == 0 {
			continue
		}
		s.broadcast(string(buf[:n]))
		lineBuf = append(lineBuf, buf[:n]...)
		for {
			idx := bytes.IndexByte(lineBuf, '\n')
			if idx < 0 {
				break
			}
			line := string(lineBuf[:idx])
			lineBuf = lineBuf[idx+1:]
			line = strings.TrimRight(line, "\r")
			if i := strings.LastIndex(line, "\r"); i >= 0 {
				line = line[i+1:]
			}
			line = AnsiEscape.ReplaceAllString(line, "")
			s.mu.Lock()
			s.seqCounter++
			seq := s.seqCounter
			s.mu.Unlock()
			entry := Entry{
				TS:    time.Now(),
				Seq:   seq,
				Line:  line,
				Bytes: len(line),
			}
			s.ringBuffer.Append(entry)
		}
	}
}

func (s *SerialSession) writeLoop() {
	stopCh := s.stopCh
	for {
		select {
		case <-stopCh:
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
	if s.port == nil {
		s.mu.Unlock()
		return
	}
	s.port.Close()
	s.port = nil
	s.connected = false
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.stopCh = make(chan struct{})
	s.mu.Unlock()
	go s.reconnectLoop()
}

func (s *SerialSession) reconnectLoop() {
	stopCh := s.stopCh
	interval := s.cfg.Reconnect.InitialInterval
	for {
		select {
		case <-stopCh:
			return
		default:
		}
		time.Sleep(interval)
		err := s.Open()
		if err == nil {
			s.broadcast("[serial reconnected]\n")
			return
		}
		log.Printf("reconnect attempt for %s failed: %v", s.device, err)
	}
}