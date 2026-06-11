# Serial Port File Recorder — Design Spec

**Date:** 2026-06-11  
**Base branch:** master (commit `60501cd`)  
**Feature branch:** `feat-file-recorder`

## Overview

串口输出自动保存到文件，按串口号命名，单文件最大 1GB，满时从头部删最旧 100MB 后继续尾部追加。

## Requirements Summary

| 维度 | 决定 |
|------|------|
| 触发方式 | 自动录制，网关启动即录 |
| 录制范围 | 仅 YAML 配置中静态列出的端口 |
| 文件格式 | 原始字节流，不做任何处理 |
| 文件命名 | 以串口号命名（如 `COM3`） |
| 存储路径 | YAML 配置 `file_recording.output_dir`，默认 `./serial_logs/` |
| 大小上限 | 1GB（可配置） |
| 满时策略 | 删掉文件头部最旧 100MB（可配置），继续尾部追加 |
| 重启行为 | 追加写入，保留已有数据 |
| 动态端口 | `POST /api/mappings` 动态添加的端口不自动录制 |

## Architecture

### 新增组件：FileRecorder

新建 `internal/recorder/` 包，独立于串口通信逻辑。

```go
type FileRecorder struct {
    device   string        // 串口号
    dir      string        // 输出目录
    maxSize  int64         // 文件最大字节数
    trimSize int64         // 满时从头部删除的字节数
    writeCh  chan []byte   // 接收原始字节数据
    stopCh   chan struct{} // 停止信号（closed）
    file     *os.File      // 当前文件句柄
    fileSize int64         // 当前文件大小（内存跟踪）
    mu       sync.Mutex    // 保护 file 和 fileSize
    logger   *slog.Logger
}
```

### 数据流

```
SerialSession.readLoop()
    │
    ├── s.broadcast(buf[:n])     // 现有：推送给订阅者（SSH/SSE）
    ├── ringBuffer.Append(...)   // 现有：环形缓冲区
    └── recorder.Write(buf[:n])  // 新增：非阻塞发到 channel
                    │
                    ▼
        FileRecorder.writeLoop()  // 独立 goroutine
                    │
                    ├── fileSize >= maxSize → trim()
                    └── file.Write(data)
```

### 与现有代码的关系

- `SerialSession.readLoop()` 增加一行 `recorder.Write()` 调用
- `PortManager` 增加 `map[string]*recorder.FileRecorder`，管理 Recorder 生命周期
- `config.Config` 增加 `FileRecordingConfig` 结构体
- 其他文件不改动

## Configuration

```yaml
# serial-gateway.yaml 新增
file_recording:
  output_dir: "./serial_logs"   # 默认值
  max_size_mb: 1024             # 默认 1024 (1GB)
  trim_size_mb: 100             # 默认 100MB
```

Go 配置结构体：

```go
type FileRecordingConfig struct {
    OutputDir  string `yaml:"output_dir"  json:"output_dir"`
    MaxSizeMB  int    `yaml:"max_size_mb" json:"max_size_mb"`
    TrimSizeMB int    `yaml:"trim_size_mb" json:"trim_size_mb"`
}
```

默认值在 `applyDefaults()` 中设置：
- `output_dir`: `"./serial_logs"`
- `max_size_mb`: `1024`
- `trim_size_mb`: `100`

## Core Logic

### NewFileRecorder

```go
func NewFileRecorder(device, dir string, maxSizeMB, trimSizeMB int, logger *slog.Logger) (*FileRecorder, error)
```

1. `os.MkdirAll(dir, 0755)` 确保目录存在
2. 以追加模式打开文件 `dir/device`
3. `stat` 获取当前文件大小赋给 `fileSize`
4. 启动 `go r.writeLoop()`

### writeLoop

```go
func (r *FileRecorder) writeLoop() {
    for {
        select {
        case <-r.stopCh:
            // 关闭文件，置 nil
            return
        case data := <-r.writeCh:
            r.mu.Lock()
            if r.fileSize >= r.maxSize {
                r.trim()
            }
            n, err := r.file.Write(data)
            if err != nil {
                r.logger.Error(..., "error", err)
            } else {
                r.fileSize += int64(n)
            }
            r.mu.Unlock()
        }
    }
}
```

### trim

```go
func (r *FileRecorder) trim()
```

1. 关闭当前文件
2. 以只读打开，Seek 到 `trimSize` 位置
3. 读取剩余内容到内存（最多 1GB，一次性读完）
4. 以写模式重新打开（覆盖），写回剩余内容
5. 更新 `fileSize` = 剩余字节数
6. 操作失败时记录 error 日志，回退到空文件从头开始写

### Write（供外部调用）

```go
func (r *FileRecorder) Write(data []byte)
```

**内部拷贝数据**（调用方的 buffer 会在下次 serial read 时被覆盖），然后非阻塞发送到 `writeCh`：

```go
copied := make([]byte, len(data))
copy(copied, data)
select {
case r.writeCh <- copied:
default:
    // 丢弃，打 warn 日志
}
```

### Stop

```go
func (r *FileRecorder) Stop()
```

关闭 `stopCh`（仅一次），`writeLoop` 收到信号后关闭文件句柄并退出。

## Lifecycle Integration

### 启动时

`PortManager.AutoStart()` 遍历 `cfg.Ports`，对每个端口创建 SSH server 后，同步创建 FileRecorder：

```go
// 在 AutoStart 循环中, srv 创建成功后
rec, err := recorder.NewFileRecorder(portCfg.Device, cfg.FileRecording.OutputDir,
    cfg.FileRecording.MaxSizeMB, cfg.FileRecording.TrimSizeMB, pm.logger.With("component", "recorder"))
if err != nil {
    pm.logger.Error("failed to create recorder", "device", portCfg.Device, "error", err)
    continue
}
pm.recorders[portCfg.Device] = rec
```

### SerialSession 写入

在 `readLoop()` 中，`s.broadcast()` 调用之后：

```go
// readLoop 中，buf[:n] 读取后
s.broadcast(string(buf[:n]))
if s.recorder != nil {
    s.recorder.Write(buf[:n])
}
```

`SerialSession` 增加 `recorder *recorder.FileRecorder` 字段和 `SetRecorder(*FileRecorder)` 方法。

**注入路径**（SerialSession 由 `ssh.NewSSHServer` 内部创建，不直接暴露给 PortManager）：

```go
// PortManager.AutoStart() 循环中：
srv, err := ssh.NewSSHServer(portCfg.Device, pm.cfg, ...)
// ... 错误处理 ...
srv.Start()
// 创建 Recorder 并注入到 session
rec, err := recorder.NewFileRecorder(portCfg.Device, cfg.FileRecording.OutputDir, ...)
if err != nil {
    pm.logger.Error("create recorder failed", "device", portCfg.Device, "error", err)
    continue
}
srv.Session().SetRecorder(rec)                   // ← 注入
pm.recorders[portCfg.Device] = rec
```

### 关闭时

`PortManager.Shutdown()` 遍历 `recorders`，逐个调用 `Stop()`，然后清空 map。

## Error Handling & Edge Cases

| 场景 | 处理 |
|------|------|
| 目录不存在 | `NewFileRecorder` 用 `os.MkdirAll` 自动创建 |
| 串口断开重连 | Recorder 不受影响，数据流恢复后继续追加 |
| 磁盘写满 | `file.Write` 失败打 error 日志，丢弃该批数据，不阻塞 |
| trim 失败 | 打 error 日志，file 重置为空文件从头写 |
| writeCh 满 | 非阻塞 select，丢弃该批数据，打 warn 日志 |
| 重启后已存在文件 | 追加模式打开，不丢旧数据 |
| 动态添加端口 | 不创建 Recorder，不自动录制 |
| 端口从 YAML 移除后重启 | 旧文件保留不删 |

### 原则

写文件失败不影响串口通信主链路。file I/O 错误只看不打回。

## Testing

### 单元测试 (`internal/recorder/recorder_test.go`)

| 用例 | 验证点 |
|------|--------|
| `TestNewFileRecorder` | 创建成功，目录和文件自动生成 |
| `TestWriteAndReopen` | 写入数据后 Close→重新 NewFileRecorder→验证追加而非覆盖 |
| `TestTrim` | 写满 maxSize 后继续写，验证旧 100MB 被截掉，新数据保留 |
| `TestWriteAfterTrim` | trim 后继续写入正常 |
| `TestNonBlockingWrite` | 小 channel buffer，快速写多批，验证不阻塞 |
| `TestStop` | Stop 后文件正确关闭，再 Write 无效果 |
| `TestDirNotExist` | 指定不存在的目录，验证自动创建 |
| `TestTrimKeepsLatest` | trim 后验证保留的是最新数据（通过内容校验） |

## Files Changed

| 文件 | 改动 |
|------|------|
| `internal/recorder/recorder.go` | **新增** FileRecorder 完整实现 |
| `internal/recorder/recorder_test.go` | **新增** 单元测试 |
| `internal/config/config.go` | 新增 `FileRecordingConfig` 结构体和默认值 |
| `internal/serial/session.go` | 新增 `recorder` 字段；`readLoop` 中新增一行 `recorder.Write()` |
| `internal/core/portmanager.go` | 新增 `recorders` map；`AutoStart` 中创建 Recorder；`Shutdown` 中关闭 |
