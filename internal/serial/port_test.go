package serial

import (
	"testing"
)

func TestListPorts(t *testing.T) {
	ports, err := ListPorts()
	if err != nil {
		t.Fatalf("ListPorts failed: %v", err)
	}
	t.Logf("found %d ports", len(ports))
	for _, p := range ports {
		t.Logf("  %s: %s (%s)", p.Device, p.Description, p.HWID)
	}
}

func TestPortInfoFields(t *testing.T) {
	pi := PortInfo{
		Device:      "COM3",
		Description: "USB Serial Device",
		HWID:        "USB VID:PID",
	}
	if pi.Device != "COM3" {
		t.Errorf("expected COM3, got %s", pi.Device)
	}
}