package adb

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// PushReader pushes data from src to remotePath on the device.
// Tries streaming (adb push -) first; if the adb binary doesn't support it,
// falls back to writing a temp file and pushing that.
// The src is fully buffered in memory before attempting either path,
// so that a failed stream attempt doesn't consume the reader.
func PushReader(adbPath, serial, remotePath string, src io.Reader) error {
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	if err := pushStream(adbPath, serial, remotePath, bytes.NewReader(data)); err == nil {
		return nil
	}
	return pushTempFileBytes(adbPath, serial, remotePath, data)
}

func pushStream(adbPath, serial, remotePath string, src io.Reader) error {
	cmd := exec.Command(adbPath, "-s", serial, "push", "-", remotePath)
	cmd.Stdin = src
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push stream: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pushTempFileBytes(adbPath, serial, remotePath string, data []byte) error {
	tmp, err := os.CreateTemp("", "adb-push-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()
	out, err := exec.Command(adbPath, "-s", serial, "push", tmp.Name(), remotePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PullWriter pulls remotePath from the device and writes it to dst.
// Tries streaming (adb pull <path> -) first; falls back to temp file.
func PullWriter(adbPath, serial, remotePath string, dst io.Writer) error {
	if err := pullStream(adbPath, serial, remotePath, dst); err == nil {
		return nil
	}
	return pullTempFile(adbPath, serial, remotePath, dst)
}

func pullStream(adbPath, serial, remotePath string, dst io.Writer) error {
	cmd := exec.Command(adbPath, "-s", serial, "pull", remotePath, "-")
	cmd.Stdout = dst
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb pull stream: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return nil
}

func pullTempFile(adbPath, serial, remotePath string, dst io.Writer) error {
	tmp, err := os.CreateTemp("", "adb-pull-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	out, err := exec.Command(adbPath, "-s", serial, "pull", remotePath, tmpName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb pull: %w: %s", err, strings.TrimSpace(string(out)))
	}
	f, err := os.Open(tmpName)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer f.Close()
	_, err = io.Copy(dst, f)
	return err
}

// ExecResult holds the result of a non-interactive adb shell command.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Exec runs a single non-interactive command via `adb shell` and returns the result.
func Exec(adbPath, serial, command string) ExecResult {
	cmd := exec.Command(adbPath, "-s", serial, "shell", command)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// ExecStream runs a non-interactive command and streams stdout to dst.
// stderr is merged into the stream.
func ExecStream(adbPath, serial, command string, dst io.Writer) error {
	cmd := exec.Command(adbPath, "-s", serial, "shell", command)
	cmd.Stdout = dst
	cmd.Stderr = dst
	return cmd.Run()
}