// Package notify provides Windows toast notification support.
package notify

import (
	"fmt"

	"github.com/go-toast/toast"
)

const appID = "SerialGateway.Notifier"

// Show is an exported function variable so tests can replace it with a mock.
// The default implementation pops a Windows toast notification.
var Show = showImpl

func showImpl(title, content string) error {
	notification := toast.Notification{
		AppID:   appID,
		Title:   title,
		Message: content,
	}
	if err := notification.Push(); err != nil {
		return fmt.Errorf("toast push failed: %w", err)
	}
	return nil
}