package adb

import (
	"strings"
	"testing"
)

func TestParseDevicesOutput(t *testing.T) {
	output := `List of devices attached
ce1617181a3b3b0d03       device usb:1-1 product:dreamlte model:SM_G955U device:dreamlte transport_id:1
192.168.1.100:5555       offline
emulator-5554            device product:sdk_gphone64_x86_64 model:sdk_gphone64_x86_64 device:generic_x86_64 transport_id:2
`
	devices := parseDevicesOutput(output)
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if devices[0].Serial != "ce1617181a3b3b0d03" {
		t.Errorf("expected serial ce1617181a3b3b0d03, got %s", devices[0].Serial)
	}
	if !strings.Contains(devices[0].Model, "SM G955U") {
		t.Errorf("expected model SM G955U, got %s", devices[0].Model)
	}
	if devices[1].Serial != "emulator-5554" {
		t.Errorf("expected serial emulator-5554, got %s", devices[1].Serial)
	}
}

func TestParseDevicesOutputEmpty(t *testing.T) {
	output := "List of devices attached\n"
	devices := parseDevicesOutput(output)
	if len(devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(devices))
	}
}

func TestParseDevicesOutputOfflineSkipped(t *testing.T) {
	output := `List of devices attached
abc123       offline
def456       device model:Pixel_6
`
	devices := parseDevicesOutput(output)
	if len(devices) != 1 || devices[0].Serial != "def456" {
		t.Errorf("expected only online device, got %+v", devices)
	}
}
