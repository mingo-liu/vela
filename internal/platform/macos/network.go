package macos

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const (
	NetworkWired    = "wired"
	NetworkWireless = "wireless"
)

// DefaultNetwork returns the default route's interface and whether it is wired or
// wireless. The kind is empty for virtual interfaces such as VPN utun devices.
func DefaultNetwork() (iface, kind string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	iface, err = defaultInterface(ctx)
	if err != nil {
		return "", "", err
	}
	ports, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", "-listallhardwareports").Output()
	if err != nil {
		return "", "", err
	}
	return iface, parseInterfaceKind(string(ports), iface), nil
}

func parseInterfaceKind(output, iface string) string {
	port := ""
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Hardware Port":
			port = value
		case "Device":
			if value != iface {
				continue
			}
			if port == "Wi-Fi" || port == "AirPort" {
				return NetworkWireless
			}
			return NetworkWired
		}
	}
	return ""
}
