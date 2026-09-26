package macos

import (
	"runtime"
	"testing"
)

func TestParseInterfaceTraffic(t *testing.T) {
	const route = "   route to: default\n  interface: en0\n"
	iface, err := parseDefaultInterface(route)
	if err != nil || iface != "en0" {
		t.Fatalf("default interface = %q, %v", iface, err)
	}
	const stats = "Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll\n" +
		"en0 1500 <Link#14> aa:bb:cc:dd:ee:ff 40 0 123456 20 0 7890 0\n" +
		"en0 1500 192.168.0 192.168.0.2 40 - 123456 20 - 7890 -\n"
	totals, err := parseInterfaceTraffic(stats, iface)
	if err != nil || totals != (NetworkTraffic{Upload: 7890, Download: 123456, Interface: "en0"}) {
		t.Fatalf("traffic totals = %+v, %v", totals, err)
	}
	if _, err := parseInterfaceTraffic(stats, "en1"); err == nil {
		t.Fatal("accepted missing interface")
	}
}

func TestTrafficTotalsReadsActiveInterface(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS network counters")
	}
	totals, err := TrafficTotals()
	if err != nil {
		t.Skipf("no active default network interface: %v", err)
	}
	if totals.Interface == "" || totals.Upload < 0 || totals.Download < 0 {
		t.Fatalf("invalid network counters: %+v", totals)
	}
}
