package recorder

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// Write 异步写入数据，不阻塞调用方。channel 满时丢弃数据。
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

// Stop 停止 FileRecorder，关闭文件句柄。幂等，多次调用安全。
func (r *FileRecorder) Stop() {
	select {
	case <-r.stopCh:
		// 已停止
	default:
		close(r.stopCh)
	}
}

func (r *FileRecorder) writeLoop() {
	for {
		select {
		case <-r.stopCh:
			// 排空 channel 中剩余数据再关闭文件
			r.drainAndClose()
			return
		case data := <-r.writeCh:
			r.processWrite(data)
		}
	}
}

// drainAndClose 排空 channel 中剩余数据后关闭文件。
func (r *FileRecorder) drainAndClose() {
	for {
		select {
		case data := <-r.writeCh:
			r.processWrite(data)
		default:
			r.mu.Lock()
			if r.file != nil {
				r.file.Close()
				r.file = nil
			}
			r.mu.Unlock()
			return
		}
	}
}

// processWrite 处理单条写入数据，含前后 trim 检查。
func (r *FileRecorder) processWrite(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	if r.fileSize >= r.maxSize && r.file != nil {
		r.trim()
	}
}

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
