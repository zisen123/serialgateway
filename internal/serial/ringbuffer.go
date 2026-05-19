package serial

import (
	"sync"
	"time"
)

type Entry struct {
	TS    time.Time `json:"ts"`
	Seq   int       `json:"seq"`
	Line  string    `json:"line"`
	Bytes int       `json:"bytes"`
}

type RingBuffer struct {
	mu         sync.Mutex
	entries    []Entry
	maxLines   int
	maxBytes   int
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