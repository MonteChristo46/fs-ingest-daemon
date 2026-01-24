package device

import (
	"errors"
	"net"
)

// GetMACAddress returns the MAC address of the first valid network interface (non-loopback).
func GetMACAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		// Skip loopback interfaces and those that are down
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip if no hardware address
		if len(iface.HardwareAddr) == 0 {
			continue
		}

		return iface.HardwareAddr.String(), nil
	}

	return "", errors.New("no valid network interface found")
}
