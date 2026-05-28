package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
	sgwhttp "github.com/zisen123/serialgateway/internal/http"
	"github.com/zisen123/serialgateway/tests/vserial"
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

	pm := core.NewPortManager(cfg, slog.Default())
	pm.AutoStart()
	gw := sgwhttp.NewGatewayWithManager(cfg, pm, slog.Default())
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
		t.Error("expected 'log line' in output �?device heartbeat not received")
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

func TestHTTPExecEndpoint(t *testing.T) {
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available: %v", err)
	}
	defer device.Close()
	go device.StartCommandResponder()

	pm, gw := setupGateway(t, 18083, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:18083/api/mappings/COM10/exec?cmd=help"))
	if err != nil {
		t.Fatalf("HTTP exec request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}

	// Read SSE stream line by line
	var receivedLines []string
	doneReceived := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			receivedLines = append(receivedLines, data)
		}
		if strings.HasPrefix(line, "event: done") {
			doneReceived = true
			break
		}
	}

	if !doneReceived {
		t.Error("expected 'event: done' in SSE stream")
	}

	full := strings.Join(receivedLines, "\n")
	if !strings.Contains(full, "Available commands") {
		t.Errorf("expected 'Available commands' in output, got: %s", full)
	}

	t.Logf("exec SSE output:\n%s", full)
}

func TestHTTPExecSilenceTimeout(t *testing.T) {
	device, err := vserial.NewMockDevice("COM11")
	if err != nil {
		t.Skipf("COM11 not available: %v", err)
	}
	defer device.Close()
	go device.StartCommandResponder()

	cfg := &config.Config{
		Ports: []config.PortConfig{
			{Device: "COM10", Baudrate: 115200},
		},
	}
	config.ApplyDefaults(cfg)
	cfg.Gateway.HTTPPort = 18084
	cfg.SSH.BasePort = 32000
	cfg.SerialDefaults.ExecSilenceTimeout = 1 * time.Second

	pm := core.NewPortManager(cfg)
	pm.AutoStart()
	gw := sgwhttp.NewGatewayWithManager(cfg, pm)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(500 * time.Millisecond)

	start := time.Now()
	resp, err := http.Get(fmt.Sprintf("http://localhost:18084/api/mappings/COM10/exec?cmd=help"))
	if err != nil {
		t.Fatalf("HTTP exec request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the full stream
	scanner := bufio.NewScanner(resp.Body)
	doneReason := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "reason") {
			var doneData map[string]string
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &doneData)
			doneReason = doneData["reason"]
		}
	}

	elapsed := time.Since(start)
	if doneReason != "silence" {
		t.Errorf("expected done reason 'silence', got '%s'", doneReason)
	}

	// Should finish within silence timeout + some buffer (give some margin)
	if elapsed > 6*time.Second {
		t.Errorf("stream took too long to finish: %v", elapsed)
	}
	t.Logf("stream ended after %v with reason=%s", elapsed, doneReason)
}

func TestHTTPExecMissingCmd(t *testing.T) {
	pm, gw := setupGateway(t, 18085, 32000)
	go gw.StartHTTP()
	defer pm.Shutdown()
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:18085/api/mappings/COM10/exec"))
	if err != nil {
		t.Fatalf("HTTP exec request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["error"] == "" {
		t.Error("expected error message in response")
	}
	t.Logf("missing cmd error: %s", result["error"])
}