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

	other, err := NewMockDevice("COM20")
	if err != nil {
		t.Skipf("COM20 not available: %v", err)
	}
	defer other.Close()

	other.Write([]byte("help\n"))
	time.Sleep(200 * time.Millisecond)

	writes := d.WriteLog()
	if len(writes) == 0 {
		t.Fatal("expected at least 1 command received")
	}
	if !strings.Contains(writes[0], "help") {
		t.Errorf("expected 'help' in write log, got '%s'", writes[0])
	}
}