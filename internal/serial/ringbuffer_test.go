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
			TS:    time.Now(),
			Seq:   i + 1,
			Line:  fmt.Sprintf("line %d", i),
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
	if len(entries) > 3 {
		t.Fatalf("expected at most 3 entries with byte limit 20, got %d", len(entries))
	}
}