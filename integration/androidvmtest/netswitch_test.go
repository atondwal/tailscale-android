// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package androidvmtest

import (
	"flag"
	"strings"
	"testing"
	"time"
)

var netSwitches = flag.Int("android.net-switches", 12, "number of Wi-Fi/cellular switches in TestNetworkSwitchKeepsDefaultRoute")

// TestNetworkSwitchKeepsDefaultRoute moves the emulator between Wi-Fi and
// cellular and checks that the app never pushes a DNS config built from a
// network's empty placeholder LinkProperties, and that an empty default route
// interface never reaches netmon (tailscale/tailscale#17747).
//
// Each switch takes the current network down before bringing the other up, so
// the new network is the only candidate when it appears. That is when
// onCapabilitiesChanged can make it the default before onLinkPropertiesChanged
// has filled in its LinkProperties.
func TestNetworkSwitchKeepsDefaultRoute(t *testing.T) {
	requireAndroid(t)
	control, _, controlURL := startTestControl(t)
	adb := adbRunner{path: *adbPath, serial: *adbSerial}
	installAndLogin(t, adb, controlURL)
	waitForNodeCount(t, control, 1)

	t.Cleanup(func() {
		adb.runAllowError(t, "shell", "svc", "wifi", "enable")
		adb.runAllowError(t, "shell", "svc", "data", "enable")
	})
	adb.run(t, "shell", "svc", "data", "disable")
	time.Sleep(5 * time.Second)

	var placeholderPushes, placeholderSkips, emptyRouteWins, noLinkChange int
	for i := range *netSwitches {
		down, up := "wifi", "data"
		if i%2 == 1 {
			down, up = "data", "wifi"
		}
		adb.run(t, "shell", "logcat", "-c")
		adb.run(t, "shell", "svc", down, "disable")
		time.Sleep(3 * time.Second)
		adb.run(t, "shell", "svc", up, "enable")
		time.Sleep(10 * time.Second)
		logs := adb.runOutput(t, "shell", "logcat", "-d")

		pushes := strings.Count(logs, "updated DNS config for iface=null")
		skips := strings.Count(logs, "default network has no link properties yet")
		wins := strings.Count(logs, "defaultroute: update from Android, ifName =  (was")
		relinked := strings.Contains(logs, "LinkChange: major") && strings.Contains(logs, "dns: Set")
		t.Logf("switch %d (%s -> %s): placeholder pushes=%d skips=%d, empty default route wins=%d, LinkChange+dns.Set=%v",
			i, down, up, pushes, skips, wins, relinked)
		placeholderPushes += pushes
		placeholderSkips += skips
		emptyRouteWins += wins
		if !relinked {
			noLinkChange++
		}
	}
	t.Logf("totals over %d switches: placeholder pushes=%d skips=%d, empty default route wins=%d, switches without LinkChange+dns.Set=%d",
		*netSwitches, placeholderPushes, placeholderSkips, emptyRouteWins, noLinkChange)
	if placeholderPushes > 0 || emptyRouteWins > 0 {
		t.Fatalf("empty LinkProperties reached the backend: %d placeholder pushes, %d empty default route wins", placeholderPushes, emptyRouteWins)
	}
}
