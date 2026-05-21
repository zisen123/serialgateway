package ssh

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	gliderssh "github.com/gliderlabs/ssh"

	"github.com/zisen123/serialgateway/internal/serial"
)

func (s *SSHServer) handleSession(sess gliderssh.Session) {
	logf := func(format string, args ...interface{}) {
		log.Printf("ssh:%s %s", s.device, fmt.Sprintf(format, args...))
	}
	logf("connected: %s", sess.RemoteAddr())

	if !s.session.IsConnected() {
		err := s.session.Open()
		if err != nil {
			fmt.Fprintf(sess, "[serial open failed: %v]\n", err)
			sess.Close()
			logf("serial open failed: %v", err)
			return
		}
	}

	sub := s.session.Subscribe()
	defer s.session.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		for msg := range sub {
			msg = strings.ReplaceAll(msg, "\r\n", "\n")
			msg = strings.ReplaceAll(msg, "\r", "")
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
					logf("read error: %v", err)
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
	logf("disconnected: %s", sess.RemoteAddr())
}