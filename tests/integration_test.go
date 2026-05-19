package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
	sgwhttp "github.com/yicongwu/serialgateway/internal/http"
	"github.com/yicongwu/serialgateway/tests/vserial"
)

func setupGateway(t *testing.T, httpPort, sshBase int) (*core.PortManager, *sgwhttp.Gateway) {
	t.Helper()
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{Device: "COM10", Baudrate: 115200},
		},
	}
	config.ApplyDefaults(cfg)
	cfg.Gateway.HTTPPort = httpPort
	cfg.SSH.BasePort = sshBase

	pm := core.NewPortManager(cfg)
	pm.AutoStart()
	gw := sgwhttp.NewGatewayWithManager(cfg, pm)
	return pm, gw
}

func TestHTTPAPIPortsEndpoint(t *testing.T) {
	pm, gw := setupGateway(t, 18080, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:18080/api/ports"))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ports"] == nil {
		t.Fatal("response missing 'ports' key")
	}
	t.Logf("ports response: %v", result["ports"])
}

func TestSSHConnectionToVirtualPort(t *testing.T) {
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available (com0com not installed): %v", err)
	}
	defer device.Close()
	device.StartContinuousOutput(200 * time.Millisecond)
	go device.StartCommandResponder()

	pm, gw := setupGateway(t, 18081, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()

	time.Sleep(1 * time.Second)

	sshConfig := &gossh.ClientConfig{
		User: "serial",
		Auth: []gossh.AuthMethod{
			gossh.Password("serial"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	conn, err := gossh.Dial("tcp", "localhost:32010", sshConfig)
	if err != nil {
		t.Fatalf("SSH connection failed: %v", err)
	}
	defer conn.Close()

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

	if err := session.Shell(); err != nil {
		t.Fatalf("session.Shell() failed: %v", err)
	}

	time.Sleep(2 * time.Second)

	fmt.Fprintf(stdin, "help\n")
	time.Sleep(500 * time.Millisecond)

	output := stdoutBuf.String()
	if len(output) == 0 {
		t.Fatal("expected output from SSH session, got empty")
	}
	t.Logf("SSH output:\n%s", output)

	if !strings.Contains(output, "log line") {
		t.Error("expected 'log line' in output — device heartbeat not received")
	}

	if !strings.Contains(output, "Available commands") {
		t.Error("expected command response 'Available commands' in output")
	}
}

func TestHTTPHistoryTailEndpoint(t *testing.T) {
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available: %v", err)
	}
	defer device.Close()
	device.StartContinuousOutput(100 * time.Millisecond)

	pm, gw := setupGateway(t, 18082, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(2 * time.Second)

	resp, err := http.Get(fmt.Sprintf("http://localhost:18082/api/mappings/COM10/tail?lines=10"))
	if err != nil {
		t.Fatalf("HTTP tail request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	items, ok := result["items"]
	if !ok {
		t.Fatal("response missing 'items' key")
	}
	t.Logf("tail items: %v", items)
}