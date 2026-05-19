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
	if err == nil {
		sess.Close()
	}
	if sess.Device() != "COM_TEST" {
		t.Errorf("expected device COM_TEST, got %s", sess.Device())
	}
}

func TestSessionWriteQueue(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := NewSerialSession("COM_FAKE", cfg)
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