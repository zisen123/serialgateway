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

	mu        sync.Mutex
	outputLog []string
	writeLog  []string
	stopCh    chan struct{}
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

func (d *MockDevice) Write(data []byte) (int, error) {
	return d.port.Write(data)
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