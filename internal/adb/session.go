package adb

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/serial"
)

var _ serial.Session = (*ADBSession)(nil)

type ADBSession struct {
	serialNum string
	adbPath   string
	username  string
	password  string
	cfg       *config.Config

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	connected bool

	writeCh     chan serial.WriteRequest
	subscribers map[chan string]struct{}
	subMu       sync.RWMutex

	ringBuffer *serial.RingBuffer
	seqCounter int

	stopCh chan struct{}
}

func NewADBSession(serialNum string, cfg *config.Config) *ADBSession {
	username := ""
	password := ""
	for _, p := range cfg.Ports {
		if p.Device == serialNum {
			username = p.AdbUsername
			password = p.AdbPassword
			break
		}
	}
	return &ADBSession{
		serialNum:   serialNum,
		adbPath:     cfg.ADB.AdbPath,
		username:    username,
		password:    password,
		cfg:         cfg,
		writeCh:     make(chan serial.WriteRequest, 64),
		subscribers: make(map[chan string]struct{}),
		ringBuffer:  serial.NewRingBuffer(cfg.RingBuffer.MaxLines, cfg.RingBuffer.MaxBytes),
		stopCh:      make(chan struct{}),
	}
}

func (s *ADBSession) Device() string                      { return s.serialNum }
func (s *ADBSession) Baudrate() int                       { return 0 }
func (s *ADBSession) WriteChannel() chan serial.WriteRequest { return s.writeCh }
func (s *ADBSession) RingBuffer() *serial.RingBuffer      { return s.ringBuffer }

func (s *ADBSession) Open() error {
	s.mu.Lock()
	if s.connected {
		s.mu.Unlock()
		return nil
	}
	cmd := exec.Command(s.adbPath, "-s", s.serialNum, "shell")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout
	s.connected = true
	s.mu.Unlock()

	if s.username != "" || s.password != "" {
		s.doLoginHandshake()
	}

	go s.readLoop()
	go s.writeLoop()
	return nil
}

// doLoginHandshake waits for login:/Password: prompts and sends credentials.
// Runs synchronously before starting the read/write goroutines.
func (s *ADBSession) doLoginHandshake() {
	deadline := time.Now().Add(3 * time.Second)
	buf := make([]byte, 256)
	accumulated := ""

	sentUser := false
	sentPass := false
	for time.Now().Before(deadline) {
		s.mu.Lock()
		r := s.stdout
		s.mu.Unlock()
		if r == nil {
			break
		}
		ch := make(chan string, 1)
		go func() {
			n, _ := r.Read(buf)
			if n > 0 {
				ch <- string(buf[:n])
			} else {
				ch <- ""
			}
		}()
		var chunk string
		select {
		case chunk = <-ch:
		case <-time.After(time.Until(deadline)):
			return
		}
		if chunk == "" {
			break
		}
		accumulated += chunk
		lower := strings.ToLower(accumulated)

		if !sentUser && strings.Contains(lower, "login:") {
			s.stdin.Write([]byte(s.username + "\n"))
			sentUser = true
			accumulated = ""
		} else if sentUser && !sentPass && strings.Contains(lower, "password:") {
			s.stdin.Write([]byte(s.password + "\n"))
			sentPass = true
			return
		}
	}
}

func (s *ADBSession) Close() {
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	s.cmd = nil
	s.stdin = nil
	s.stdout = nil
	s.connected = false
	s.mu.Unlock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *ADBSession) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *ADBSession) Subscribe() chan string {
	ch := make(chan string, 64)
	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()
	return ch
}

func (s *ADBSession) Unsubscribe(ch chan string) {
	s.subMu.Lock()
	delete(s.subscribers, ch)
	s.subMu.Unlock()
	close(ch)
}

func (s *ADBSession) readLoop() {
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
		r := s.stdout
		s.mu.Unlock()
		if r == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		n, err := r.Read(buf)
		if err != nil {
			log.Printf("adb read error on %s: %v", s.serialNum, err)
			s.broadcast("[adb disconnected - waiting for reconnect...]\n")
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
			line = serial.AnsiEscape.ReplaceAllString(line, "")
			s.mu.Lock()
			s.seqCounter++
			seq := s.seqCounter
			s.mu.Unlock()
			s.ringBuffer.Append(serial.Entry{
				TS:    time.Now(),
				Seq:   seq,
				Line:  line,
				Bytes: len(line),
			})
		}
	}
}

func (s *ADBSession) writeLoop() {
	stopCh := s.stopCh
	for {
		select {
		case <-stopCh:
			return
		case req := <-s.writeCh:
			s.mu.Lock()
			w := s.stdin
			s.mu.Unlock()
			if w == nil {
				if s.cfg.Reconnect.DiscardInputOnDisconnect {
					req.Done <- fmt.Errorf("adb disconnected, input discarded")
				}
				continue
			}
			_, err := w.Write(req.Data)
			req.Done <- err
		}
	}
}

func (s *ADBSession) broadcast(msg string) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	for ch := range s.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *ADBSession) handleDisconnect() {
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	s.cmd = nil
	s.stdin = nil
	s.stdout = nil
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

func (s *ADBSession) reconnectLoop() {
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
			s.broadcast("[adb reconnected]\n")
			return
		}
		log.Printf("adb reconnect attempt for %s failed: %v", s.serialNum, err)
		if interval < s.cfg.Reconnect.MaxInterval {
			interval *= 2
			if interval > s.cfg.Reconnect.MaxInterval {
				interval = s.cfg.Reconnect.MaxInterval
			}
		}
	}
}
