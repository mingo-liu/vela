package macos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type NetworkTraffic struct {
	Upload    int64
	Download  int64
	Interface string
}

// TrafficTotals reads the byte counters of the interface used by the default route.
func TrafficTotals() (NetworkTraffic, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	route, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return NetworkTraffic{}, err
	}
	iface, err := parseDefaultInterface(string(route))
	if err != nil {
		return NetworkTraffic{}, err
	}
	stats, err := exec.CommandContext(ctx, "/usr/sbin/netstat", "-ibn", "-I", iface).Output()
	if err != nil {
		return NetworkTraffic{}, err
	}
	return parseInterfaceTraffic(string(stats), iface)
}

func parseDefaultInterface(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && key == "interface" && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", errors.New("无法确定当前默认网络接口")
}

func parseInterfaceTraffic(output, iface string) (NetworkTraffic, error) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return NetworkTraffic{}, errors.New("网络接口统计为空")
	}
	header := strings.Fields(lines[0])
	inIndex, outIndex := -1, -1
	for index, name := range header {
		switch name {
		case "Ibytes":
			inIndex = index
		case "Obytes":
			outIndex = index
		}
	}
	if inIndex < 0 || outIndex < 0 {
		return NetworkTraffic{}, errors.New("网络接口统计缺少字节列")
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) <= outIndex || fields[0] != iface {
			continue
		}
		if !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		download, inErr := strconv.ParseInt(fields[inIndex], 10, 64)
		upload, outErr := strconv.ParseInt(fields[outIndex], 10, 64)
		if inErr != nil || outErr != nil {
			return NetworkTraffic{}, fmt.Errorf("无法读取网络接口 %s 的流量", iface)
		}
		return NetworkTraffic{Upload: upload, Download: download, Interface: iface}, nil
	}
	return NetworkTraffic{}, fmt.Errorf("未找到网络接口 %s 的流量", iface)
}
