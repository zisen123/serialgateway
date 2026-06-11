# Serial Port File Recorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将串口输出按端口名保存到文件，单文件最大 1GB，满时从头部剪裁 100MB 后继续追加。

**Architecture:** 新建 `internal/recorder` 包，FileRecorder 通过 buffered channel 异步接收原始字节，独立 goroutine 写入文件。通过 `SerialSession.SetRecorder()` 注入，`readLoop` 中非阻塞推数据。PortManager 统一管理 Recorder 生命周期。

**Tech Stack:** Go 1.26.3, 标准库 `os` / `sync` / `testing`

---

### Task 1: 新增 FileRecordingConfig 配置

**Files:**
- Modify: `internal/config/config.go:74-84` (Config struct), `internal/config/config.go:107-211` (applyDefaults)

- [ ] **Step 1: 添加 FileRecordingConfig 结构体**

在 `ReverseTunnelConfig` 和 `LoggingConfig` 之间插入（约第 62 行后）：

```go
type FileRecordingConfig struct {
	OutputDir  string `yaml:"output_dir"  json:"output_dir"`
	MaxSizeMB  int    `yaml:"max_size_mb" json:"max_size_mb"`
	TrimSizeMB int    `yaml:"trim_size_mb" json:"trim_size_mb"`
}
```

- [ ] **Step 2: 在 Config 结构体中添加字段**

在 `Config` 结构体的 `Logging` 字段前插入（约第 81 行）：

```go
FileRecording FileRecordingConfig `yaml:"file_recording" json:"file_recording"`
```

- [ ] **Step 3: 在 applyDefaults 中添加默认值**

在 `applyDefaults` 函数末尾（`}` 之前）添加：

```go
if cfg.FileRecording.OutputDir == "" {
	cfg.FileRecording.OutputDir = "./serial_logs"
}
if cfg.FileRecording.MaxSizeMB == 0 {
	cfg.FileRecording.MaxSizeMB = 1024
}
if cfg.FileRecording.TrimSizeMB == 0 {
	cfg.FileRecording.TrimSizeMB = 100
}
```

- [ ] **Step 4: 运行现有测试确认无回归**

```powershell
go test ./internal/config/...
```

期望：PASS

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go
git commit -m "feat: add FileRecordingConfig with defaults (1GB max, 100MB trim)"
```

---

### Task 2: 创建 FileRecorder 核心实现

**Files:**
- Create: `internal/recorder/recorder.go`

- [ ] **Step 1: 写出骨架和构造函数**

```go
package recorder

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// FileRecorder 异步将原始字节写入文件，支持文件大小上限和头部剪裁。
type FileRecorder struct {
	device   string
	dir      string
	maxSize  int64
	trimSize int64

	writeCh chan []byte
	stopCh  chan struct{}

	mu       sync.Mutex
	file     *os.File
	fileSize int64

	logger *slog.Logger
}

// NewFileRecorder 创建 FileRecorder，自动创建目录并以追加模式打开文件。
func NewFileRecorder(device, dir string, maxSizeMB, trimSizeMB int, logger *slog.Logger) (*FileRecorder, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir %s: %w", dir, err)
	}

	filePath := filepath.Join(dir, device)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", filePath, err)
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat file %s: %w", filePath, err)
	}

	r := &FileRecorder{
		device:   device,
		dir:      dir,
		maxSize:  int64(maxSizeMB) * 1024 * 1024,
		trimSize: int64(trimSizeMB) * 1024 * 1024,
		writeCh:  make(chan []byte, 256),
		stopCh:   make(chan struct{}),
		file:     f,
		fileSize: fi.Size(),
		logger:   logger,
	}

	go r.writeLoop()
	return r, nil
}
```

注意：需要导入 `path/filepath`。

- [ ] **Step 2: 确认编译通过**

```powershell
go build ./internal/recorder/
```

期望：编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/recorder/recorder.go
git commit -m "feat: add FileRecorder skeleton with constructor"
```

---

### Task 3: 实现 FileRecorder 完整功能（Write + writeLoop + trim + Stop）

**Files:**
- Modify: `internal/recorder/recorder.go`

- [ ] **Step 1: 实现 Write 方法（拷贝数据，非阻塞发送）**

```go
// Write 异步写入数据，不会阻塞调用方。channel 满时丢弃数据。
func (r *FileRecorder) Write(data []byte) {
	if len(data) == 0 {
		return
	}
	copied := make([]byte, len(data))
	copy(copied, data)
	select {
	case r.writeCh <- copied:
	default:
		r.logger.Warn("recorder channel full, dropping data", "device", r.device, "bytes", len(data))
	}
}
```

- [ ] **Step 2: 实现 writeLoop**

```go
func (r *FileRecorder) writeLoop() {
	for {
		select {
		case <-r.stopCh:
			r.mu.Lock()
			if r.file != nil {
				r.file.Close()
				r.file = nil
			}
			r.mu.Unlock()
			return
		case data := <-r.writeCh:
			r.mu.Lock()
			if r.fileSize >= r.maxSize {
				r.trim()
			}
			if r.file != nil {
				n, err := r.file.Write(data)
				if err != nil {
					r.logger.Error("recorder write error", "device", r.device, "error", err)
				} else {
					r.fileSize += int64(n)
				}
			}
			r.mu.Unlock()
		}
	}
}
```

- [ ] **Step 3: 实现 trim 方法**

```go
// trim 关闭当前文件，读取除头部 trimSize 字节外的剩余内容，
// 然后覆盖写回文件。必须在持有 r.mu 锁的情况下调用。
func (r *FileRecorder) trim() {
	if r.file == nil {
		return
	}

	filePath := filepath.Join(r.dir, r.device)

	// 关闭写入句柄
	if err := r.file.Close(); err != nil {
		r.logger.Error("recorder close for trim failed", "device", r.device, "error", err)
		r.file = nil
		return
	}
	r.file = nil

	// 以只读打开，跳过头部 trimSize 字节
	rf, err := os.Open(filePath)
	if err != nil {
		r.logger.Error("recorder open for trim read failed", "device", r.device, "error", err)
		return
	}

	if _, err := rf.Seek(r.trimSize, 0); err != nil {
		rf.Close()
		r.logger.Error("recorder seek for trim failed", "device", r.device, "error", err)
		return
	}

	remaining, err := io.ReadAll(rf)
	rf.Close()
	if err != nil {
		r.logger.Error("recorder read for trim failed", "device", r.device, "error", err)
		return
	}

	// 覆盖写回
	wf, err := os.Create(filePath)
	if err != nil {
		r.logger.Error("recorder create for trim write failed", "device", r.device, "error", err)
		return
	}

	n, err := wf.Write(remaining)
	if err != nil {
		wf.Close()
		r.logger.Error("recorder write after trim failed", "device", r.device, "error", err)
		return
	}
	wf.Close()

	r.fileSize = int64(n)
	r.logger.Info("recorder trimmed", "device", r.device, "new_size", r.fileSize)

	// 重新以追加模式打开
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		r.logger.Error("recorder reopen after trim failed", "device", r.device, "error", err)
		return
	}
	r.file = f
}
```

- [ ] **Step 4: 实现 Stop 方法**

```go
// Stop 停止 FileRecorder，关闭文件句柄。幂等，多次调用安全。
func (r *FileRecorder) Stop() {
	select {
	case <-r.stopCh:
		// 已停止
	default:
		close(r.stopCh)
	}
}
```

- [ ] **Step 5: 确认编译通过**

```powershell
go build ./internal/recorder/
```

期望：编译成功

- [ ] **Step 6: 提交**

```bash
git add internal/recorder/recorder.go
git commit -m "feat: implement FileRecorder write loop, trim, and stop"
```

---

### Task 4: 编写 FileRecorder 单元测试

**Files:**
- Create: `internal/recorder/recorder_test.go`

- [ ] **Step 1: 测试 NewFileRecorder 自动创建目录和文件**

```go
package recorder

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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
```

- [ ] **Step 2: 测试写入和验证内容**

```go
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
```

需要 import `time`。

- [ ] **Step 3: 测试追加写入（重启后数据保留）**

```go
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
```

- [ ] **Step 4: 测试 Stop 幂等性**

```go
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
```

- [ ] **Step 5: 测试 channel 满时不阻塞（非阻塞写入）**

```go
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
```

- [ ] **Step 6: 运行测试**

```powershell
go test ./internal/recorder/ -v -timeout 30s
```

期望：所有测试 PASS

- [ ] **Step 7: 测试 trim 功能**

```go
func TestFileRecorder_Trim(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "recorder_test_trim")
	defer os.RemoveAll(tmpDir)

	// maxSize=1KB, trimSize=512B
	r, err := NewFileRecorder("COM6", tmpDir, 0, 0, discardLogger())
	if err != nil {
		t.Fatalf("NewFileRecorder failed: %v", err)
	}
	// 手动设置小值以加速测试
	r.maxSize = 1024       // 1KB
	r.trimSize = 512        // 512B

	// 写入 1KB 数据 (序号 0-9, 每个 100 字节)
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
	// 写满 1024 字节后 trim 掉最旧 512 字节，剩余 ~588 字节
	// 文件大小应 < 1024+100 (truncated once), 且 > 512-100
	if len(data) > 1024 {
		t.Errorf("expected file size <= 1024, got %d", len(data))
	}
	if len(data) < 400 {
		t.Errorf("expected file size >= 400, got %d (trim too aggressive?)", len(data))
	}
}
```

需要 import `bytes`。

- [ ] **Step 8: 运行全部测试**

```powershell
go test ./internal/recorder/ -v -timeout 30s
```

期望：所有测试 PASS

- [ ] **Step 9: 提交**

```bash
git add internal/recorder/recorder_test.go
git commit -m "test: add FileRecorder unit tests"
```

---

### Task 5: 在 SerialSession 中集成 Recorder

**Files:**
- Modify: `internal/serial/session.go:24-42` (SerialSession struct), `internal/serial/session.go:44-61` (NewSerialSession), `internal/serial/session.go:145-199` (readLoop)

- [ ] **Step 1: 在 SerialSession 结构体中添加 recorder 字段**

在 `SerialSession` 结构体中（`ringBuffer *RingBuffer` 之后）添加：

```go
recorder interface {
	Write([]byte)
}
```

使用接口而非具体类型，避免 `internal/serial` 和 `internal/recorder` 之间的循环导入。

- [ ] **Step 2: 添加 SetRecorder 方法**

在 `Baudrate()` 方法之后添加：

```go
// SetRecorder 设置输出录制器（可选）。串口读取的原始数据将异步写入 recorder。
func (s *SerialSession) SetRecorder(r interface{ Write([]byte) }) {
	s.recorder = r
}
```

- [ ] **Step 3: 在 readLoop 中添加 recorder.Write 调用**

在 `readLoop` 中 `s.broadcast(string(buf[:n]))` 这一行之后添加：

```go
if s.recorder != nil {
	s.recorder.Write(buf[:n])
}
```

- [ ] **Step 4: 确认编译通过，运行现有测试**

```powershell
go build ./internal/serial/
go test ./internal/serial/ -v -timeout 30s
```

期望：编译成功，测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/serial/session.go
git commit -m "feat: integrate recorder into SerialSession readLoop"
```

---

### Task 6: 在 PortManager 中管理 Recorder 生命周期

**Files:**
- Modify: `internal/core/portmanager.go:12-17` (PortManager struct), `internal/core/portmanager.go:27-47` (AutoStart), `internal/core/portmanager.go:98-105` (Shutdown)

添加 import:
```go
"github.com/zisen123/serialgateway/internal/recorder"
```

- [ ] **Step 1: 在 PortManager 结构体中添加 recorders map**

```go
type PortManager struct {
	cfg       *config.Config
	logger    *slog.Logger
	servers   map[string]*ssh.SSHServer
	recorders map[string]*recorder.FileRecorder  // new
	mu        sync.RWMutex
}
```

- [ ] **Step 2: 在 NewPortManager 中初始化 recorders map**

```go
func NewPortManager(cfg *config.Config, logger *slog.Logger) *PortManager {
	return &PortManager{
		cfg:       cfg,
		logger:    logger,
		servers:   make(map[string]*ssh.SSHServer),
		recorders: make(map[string]*recorder.FileRecorder),  // new
	}
}
```

- [ ] **Step 3: 在 AutoStart 中创建 Recorder 并注入**

在 `AutoStart` 循环中，`srv.Start()` 成功后，`pm.servers` 赋值前，添加 Recorder 创建逻辑：

```go
// 在 srv.Start() 和 pm.servers[portCfg.Device] = srv 之间
rec, err := recorder.NewFileRecorder(
	portCfg.Device,
	pm.cfg.FileRecording.OutputDir,
	pm.cfg.FileRecording.MaxSizeMB,
	pm.cfg.FileRecording.TrimSizeMB,
	pm.logger.With("component", "recorder", "device", portCfg.Device),
)
if err != nil {
	pm.logger.Error("failed to create recorder", "device", portCfg.Device, "error", err)
} else {
	srv.Session().SetRecorder(rec)
	pm.recorders[portCfg.Device] = rec
	pm.logger.Info("file recorder started", "device", portCfg.Device, "dir", pm.cfg.FileRecording.OutputDir)
}
```

- [ ] **Step 4: 在 Shutdown 中关闭所有 Recorder**

在 `Shutdown` 方法中，关闭 SSH server 的循环之前或之后，添加 Recorder 关闭：

```go
func (pm *PortManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, rec := range pm.recorders {
		rec.Stop()
	}
	pm.recorders = make(map[string]*recorder.FileRecorder)
	for _, srv := range pm.servers {
		srv.Stop()
	}
	pm.servers = make(map[string]*ssh.SSHServer)
}
```

- [ ] **Step 5: 确认编译通过**

```powershell
go build ./internal/core/
go build ./...
```

期望：编译成功

- [ ] **Step 6: 运行现有测试确认无回归**

```powershell
go test ./internal/... -v -timeout 30s
```

期望：所有测试 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/core/portmanager.go
git commit -m "feat: manage FileRecorder lifecycle in PortManager"
```

---

### Task 7: 端到端验证

**Files:** 无新增

- [ ] **Step 1: 创建测试配置文件**

```powershell
# 使用虚拟串口进行快速验证
go test ./tests/... -v -timeout 30s -run TestRealPort
```

期望：现有测试 PASS（功能不影响现有行为）

- [ ] **Step 2: 手动验证（可选）**

如果环境中有实际串口，启动服务后检查：
1. `./serial_logs/` 目录自动创建
2. 每个配置的串口有对应的输出文件
3. 串口有数据输出时，文件大小增长

```powershell
go run ./cmd/serial-gateway/ -config serial-gateway.yaml
```

- [ ] **Step 3: 提交最终版本确认**

```bash
git status
```

期望：工作区干净，所有改动已提交

---

### Task 8: 更新 YAML 示例（可选）

**Files:**
- 检查: `serial-gateway.yaml`（如果存在）

- [ ] **Step 1: 在示例配置文件中添加 file_recording 注释**

如果存在 `serial-gateway.yaml` 示例文件，添加：

```yaml
# 串口输出文件录制（可选，默认启用）
# file_recording:
#   output_dir: "./serial_logs"   # 输出目录
#   max_size_mb: 1024             # 单文件最大 1GB
#   trim_size_mb: 100             # 满时从头部删除 100MB
```

- [ ] **Step 2: 提交**

```bash
git add serial-gateway.yaml
git commit -m "docs: add file_recording config example"
```
