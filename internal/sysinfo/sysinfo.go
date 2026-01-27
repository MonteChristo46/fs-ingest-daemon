package sysinfo

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// Collect gathers system information and returns it as a map.
func Collect() (map[string]interface{}, error) {
	data := make(map[string]interface{})

	// 1. Host Info (Hostname, OS, Platform, Kernel)
	hInfo, err := host.Info()
	if err == nil {
		data["Hostname"] = hInfo.Hostname
		data["OS"] = hInfo.OS
		data["Platform"] = hInfo.Platform
		data["PlatformVersion"] = hInfo.PlatformVersion
		data["KernelVersion"] = hInfo.KernelVersion
		data["Arch"] = hInfo.KernelArch
	}

	// 2. CPU Info (Model)
	cInfos, err := cpu.Info()
	if err == nil && len(cInfos) > 0 {
		// Use the first CPU's model name
		data["CPU Model"] = cInfos[0].ModelName
		data["CPU Cores"] = len(cInfos)
	}

	// 3. Memory Info (Total RAM)
	mInfo, err := mem.VirtualMemory()
	if err == nil {
		data["Total RAM"] = fmt.Sprintf("%d MB", mInfo.Total/1024/1024)
	}

	// 4. Network Info (MAC, IP)
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			// Check for loopback in Flags (slice of strings)
			isLoopback := false
			for _, f := range iface.Flags {
				if f == "loopback" {
					isLoopback = true
					break
				}
			}

			if isLoopback || iface.HardwareAddr == "" {
				continue
			}

			// Add the first suitable interface found
			data["MAC Address"] = iface.HardwareAddr

			// Get IP addresses for this interface
			// gopsutil's InterfaceStat has Addrs []InterfaceAddr
			for _, addr := range iface.Addrs {
				// Check if it's an IPv4 address
				if strings.Contains(addr.Addr, ".") {
					data["IP Address"] = addr.Addr
					break
				}
			}

			// If we found an IP, we are likely done.
			// If not, we might want to keep looking for an interface with an IP.
			if val, ok := data["IP Address"]; ok && val != "" {
				break
			}
		}
	}

	// 5. Go Runtime Info
	data["Go Version"] = runtime.Version()

	return data, nil
}
