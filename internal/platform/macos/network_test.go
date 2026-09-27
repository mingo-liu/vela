package macos

import "testing"

func TestParseInterfaceKind(t *testing.T) {
	output := `
Hardware Port: Ethernet Adapter (en4)
Device: en4
Ethernet Address: 00:11:22:33:44:55

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 66:77:88:99:aa:bb

Hardware Port: Thunderbolt Bridge
Device: bridge0
Ethernet Address: N/A
`
	for iface, want := range map[string]string{"en0": NetworkWireless, "en4": NetworkWired, "utun3": ""} {
		if got := parseInterfaceKind(output, iface); got != want {
			t.Errorf("%s kind = %q, want %q", iface, got, want)
		}
	}
}
