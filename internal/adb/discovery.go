package adb

import (
	"fmt"
	"os/exec"
	"strings"
)

type DeviceInfo struct {
	Serial string `json:"serial"`
	Model  string `json:"model"`
	State  string `json:"state"`
}

// ListDevices runs `adb devices -l` and returns connected (state=="device") entries.
func ListDevices(adbPath string) ([]DeviceInfo, error) {
	out, err := exec.Command(adbPath, "devices", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}
	return parseDevicesOutput(string(out)), nil
}

// IsAvailable reports whether the adb binary is executable.
func IsAvailable(adbPath string) bool {
	err := exec.Command(adbPath, "version").Run()
	return err == nil
}

func parseDevicesOutput(output string) []DeviceInfo {
	var devices []DeviceInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		serial := fields[0]
		state := fields[1]
		if state != "device" {
			continue
		}
		model := ""
		for _, f := range fields[2:] {
			if strings.HasPrefix(f, "model:") {
				model = strings.TrimPrefix(f, "model:")
				model = strings.ReplaceAll(model, "_", " ")
				break
			}
		}
		devices = append(devices, DeviceInfo{
			Serial: serial,
			Model:  model,
			State:  state,
		})
	}
	return devices
}
