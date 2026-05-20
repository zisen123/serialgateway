package tests

import (
	"bytes"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestSSHEchoImmediate(t *testing.T) {
	sshConfig := &gossh.ClientConfig{
		User: "serial",
		Auth: []gossh.AuthMethod{
			gossh.Password("serial"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	conn, err := gossh.Dial("tcp", "localhost:2203", sshConfig)
	if err != nil {
		t.Skipf("Gateway not running: %v", err)
	}
	defer conn.Close()
	t.Log("SSH connected")

	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("Session failed: %v", err)
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

	time.Sleep(500 * time.Millisecond)
	stdoutBuf.Reset()

	// type 'l' 's' character by character with delay
	stdin.Write([]byte("l"))
	time.Sleep(200 * time.Millisecond)
	stdin.Write([]byte("s"))
	time.Sleep(200 * time.Millisecond)
	stdin.Write([]byte("\n"))
	time.Sleep(500 * time.Millisecond)

	output := stdoutBuf.String()
	t.Logf("Char-by-char output:\n%s", output)

	if len(output) == 0 {
		t.Error("No output — echo might not be working")
	} else {
		t.Log("Echo is working — characters received")
	}
}