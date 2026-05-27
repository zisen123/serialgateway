package ssh

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	gliderssh "github.com/gliderlabs/ssh"

	"github.com/zisen123/serialgateway/internal/serial"
)

func (s *SSHServer) handleSession(sess gliderssh.Session) {
	logf := func(level slog.Level, msg string, args ...any) {
		allArgs := append([]any{"remote_addr", sess.RemoteAddr().String()}, args...)
		s.logger.Log(nil, level, msg, allArgs...)
	}
	logf(slog.LevelInfo, "connected")

	if !s.session.IsConnected() {
		err := s.session.Open()
		if err != nil {
			fmt.Fprintf(sess, "[serial open failed: %v]\n", err)
			sess.Close()
			logf(slog.LevelError, "serial open failed", "error", err)
			return
		}
	}

	sub := s.session.Subscribe()
	defer s.session.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		for msg := range sub {
			msg = strings.ReplaceAll(msg, "\r\n", "\n")
			msg = strings.ReplaceAll(msg, "\n", "\r\n")
			fmt.Fprintf(sess, "%s", msg)
		}
		close(done)
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := sess.Read(buf)
			if err != nil {
				if err != io.EOF {
					logf(slog.LevelWarn, "read error", "error", err)
				}
				return
			}
			req := serial.WriteRequest{
				Data: buf[:n],
				Done: make(chan error, 1),
			}
			s.session.WriteChannel() <- req
			select {
			case writeErr := <-req.Done:
				if writeErr != nil {
					fmt.Fprintf(sess, "[write error: %v]\n", writeErr)
				}
			case <-time.After(s.cfg.SerialDefaults.WriteTimeout):
				fmt.Fprintf(sess, "[write timeout]\n")
			}
		}
	}()

	<-done
	logf(slog.LevelInfo, "disconnected")
}