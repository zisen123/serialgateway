package tests

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestRealPortSSHConnection(t *testing.T) {
	device := os.Getenv("SG_TEST_PORT")
	if device == "" {
		device = "COM3"
	}
	sshPort := os.Getenv("SG_TEST_SSH_PORT")
	if sshPort == "" {
		sshPort = "2203"
	}

	sshConfig := &gossh.ClientConfig{
		User: "serial",
		Auth: []gossh.AuthMethod{
			gossh.Password("serial"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("localhost:%s", sshPort)
	t.Logf("Connecting SSH to %s (device: %s)...", addr, device)

	conn, err := gossh.Dial("tcp", addr, sshConfig)
	if err != nil {
		t.Fatalf("SSH connection failed: %v (is gateway running with %s?)", err, device)
	}
	defer conn.Close()
	t.Log("SSH connected successfully")

	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("SSH session failed: %v", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe failed: %v", err)
	}
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stdoutBuf

	if err := session.Shell(); err != nil {
		t.Fatalf("Shell failed: %v", err)
	}

	time.Sleep(2 * time.Second)
	initialOutput := stdoutBuf.String()
	t.Logf("Initial output (%d bytes):\n%s", len(initialOutput), initialOutput)

	stdin.Write([]byte("\n"))
	time.Sleep(1 * time.Second)

	stdin.Write([]byte("\n"))
	time.Sleep(1 * time.Second)

	stdin.Write([]byte("echo hello_serial_gateway\n"))
	time.Sleep(1 * time.Second)

	output := stdoutBuf.String()
	t.Logf("Full output (%d bytes):\n%s", len(output), output)

	if strings.Contains(output, "hello_serial_gateway") {
		t.Log("SUCCESS: Echo command received")
	} else if len(output) > len(initialOutput) {
		t.Log("Device responded (some output received)")
	} else {
		t.Log("No new output — device may be idle, but connection works")
	}
}