package serial

import (
	"go.bug.st/serial/enumerator"
)

type PortInfo struct {
	Device      string `json:"device"`
	Description string `json:"description"`
	HWID        string `json:"hwid"`
}

func ListPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	result := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		hwid := p.VID + ":" + p.PID
		if hwid == ":" {
			hwid = ""
		}
		result = append(result, PortInfo{
			Device:      p.Name,
			Description: p.Product,
			HWID:        hwid,
		})
	}
	return result, nil
}