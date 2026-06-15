// Package notify provides Windows toast notification support.
package notify

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

const appID = "SerialGateway.Notifier"

// Valid toast durations.
const (
	DurationShort = "short"
	DurationLong  = "long"
)

// Show is an exported function variable so tests can replace it with a mock.
// The default implementation pops a Windows toast notification.
var Show = showImpl

// xmlEscapeText converts non-ASCII characters to XML numeric character references
// (&#xxxxx;) and escapes XML special characters. This ensures the generated script
// contains only ASCII, avoiding encoding issues across Go/PowerShell/XML boundaries.
func xmlEscapeText(s string) string {
	var buf strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			buf.WriteString("&amp;")
		case r == '<':
			buf.WriteString("&lt;")
		case r == '>':
			buf.WriteString("&gt;")
		case r == '"':
			buf.WriteString("&quot;")
		case r == '\'':
			buf.WriteString("&apos;")
		case r > 127:
			fmt.Fprintf(&buf, "&#%d;", r)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// toastXML generates the XML template for a Windows toast notification.
func toastXML(title, content, duration string) string {
	return fmt.Sprintf(`<toast activationType="protocol" duration="%s">
    <visual>
        <binding template="ToastGeneric">
            <text id="1">%s</text>
            <text id="2">%s</text>
        </binding>
    </visual>
    <audio src="ms-winsoundevent:Notification.Default" loop="false"/>
</toast>`, duration, xmlEscapeText(title), xmlEscapeText(content))
}

// powershellScript generates the PowerShell script to show a toast.
// Uses single-quoted here-string (verbatim) to avoid $ expansion issues.
func powershellScript(xml string) string {
	return fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.UI.Notifications.ToastNotification, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null

$template = @'
%s
'@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)
$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show($toast)
`, xml, appID)
}

// encodeUTF16LEBase64 converts a string to UTF-16LE bytes then base64 encodes.
// This is the format PowerShell -EncodedCommand expects.
func encodeUTF16LEBase64(s string) string {
	buf := new(bytes.Buffer)
	for _, r := range s {
		binary.Write(buf, binary.LittleEndian, uint16(r))
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func showImpl(title, content, duration string) error {
	xml := toastXML(title, content, duration)
	script := powershellScript(xml)
	encoded := encodeUTF16LEBase64(script)

		cmd := exec.Command("PowerShell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("toast push failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}
