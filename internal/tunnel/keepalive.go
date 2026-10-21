package tunnel

import (
	"fmt"
	"log/slog"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

const (
	keepAliveInterval = 30 * time.Second
	keepAliveMaxFails = 3
)

// keepAliveMonitor sends an SSH keepalive request every keepAliveInterval and
// logs each successful probe at DEBUG level. After keepAliveMaxFails
// consecutive failures it calls onDead and returns.
//
// We do NOT rely on client.Wait() because a silently-dropped TCP connection
// never produces a read error — both sides think the link is up. The periodic
// probe forces the client to write; on a half-dead link the write eventually
// errors and the tunnel tears down and reconnects.
func keepAliveMonitor(client *gossh.Client, stopCh <-chan struct{}, logger *slog.Logger, onDead func(error)) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	fails := 0
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			// keepalive@openssh.com is the conventional keepalive request.
			// It asks the server to reply, proving the connection is alive.
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				fails++
				if fails >= keepAliveMaxFails {
					onDead(fmt.Errorf("keepalive failed %d times: %w", fails, err))
					return
				}
				continue
			}
			if fails > 0 {
				logger.Info("ssh keepalive recovered", "after_failures", fails)
			}
			fails = 0
			logger.Debug("ssh keepalive ok")
		}
	}
}
