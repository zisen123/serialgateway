package recorder

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// discardLogger returns a logger that discards all output, safe for tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestNewFileRecorder_CreatesDirAndFile(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_new")
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRecorder("COM1", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}
	defer r.Stop()

	filePath := filepath.Join(tmpDir, "COM1")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", filePath)
	}
}

func TestFileRecorder_Write(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_write")
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRecorder("COM2", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}

	r.Write([]byte("hello"))
	r.Write([]byte(" world"))
	r.Stop()

	// 等待 writeLoop 处理完
	time.Sleep(50 * time.Millisecond)

	data, err := os.ReadFile(filepath.Join(tmpDir, "COM2"))
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", string(data))
	}
}

func TestFileRecorder_Append(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_append")
	defer os.RemoveAll(tmpDir)

	r1, err := NewFileRecorder("COM3", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("first NewFileRecorder failed: %v", err)
	}
	r1.Write([]byte("first "))
	r1.Stop()
	time.Sleep(50 * time.Millisecond)

	r2, err := NewFileRecorder("COM3", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("second NewFileRecorder failed: %v", err)
	}
	r2.Write([]byte("second"))
	r2.Stop()
	time.Sleep(50 * time.Millisecond)

	data, err := os.ReadFile(filepath.Join(tmpDir, "COM3"))
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(data) != "first second" {
		t.Fatalf("expected 'first second', got '%s'", string(data))
	}
}

func TestFileRecorder_StopIdempotent(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_stop")
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRecorder("COM4", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}
	r.Stop()
	r.Stop() // 不应 panic
}

func TestFileRecorder_NonBlockingWrite(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_nonblock")
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRecorder("COM5", tmpDir, 1, 1, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}
	defer r.Stop()

	// 快速写入大量数据，超出 channel buffer (256)
	// 文件 I/O 慢于 channel send，buffer 必然溢出
	data := []byte("x")
	for i := 0; i < 500; i++ {
		r.Write(data)
	}

	// 不应死锁，不应 panic
	r.Stop()
	time.Sleep(50 * time.Millisecond)
}

func TestFileRecorder_Trim(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_trim")
	defer os.RemoveAll(tmpDir)

	r, err := NewFileRecorder("COM6", tmpDir, 0, 0, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}
	// 手动设置小值以加速测试
	r.maxSize = 1024  // 1KB
	r.trimSize = 512  // 512B

	// 写入 1KB+ 数据 (序号 0-10, 每个 100 字节)
	chunk := bytes.Repeat([]byte("0123456789"), 10) // 100 bytes
	for i := 0; i < 11; i++ {
		r.Write(chunk)
	}
	r.Stop()
	time.Sleep(100 * time.Millisecond)

	data, err := os.ReadFile(filepath.Join(tmpDir, "COM6"))
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}

	// 总共写入 11*100=1100 字节
	// 写满 1024 字节后 trim 掉最旧 512 字节，剩余在 588 左右
	if len(data) > 1024 {
		t.Errorf("expected file size <= 1024, got %d", len(data))
	}
	if len(data) < 400 {
		t.Errorf("expected file size >= 400, got %d (trim too aggressive?)", len(data))
	}
}
