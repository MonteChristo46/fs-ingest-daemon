package device

import (
	"errors"
	"net"
	"strings"
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

		mac := iface.HardwareAddr.String()
		return strings.ReplaceAll(mac, ":", "-"), nil
	}

	return "", errors.New("no valid network interface found")
}
