package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestRemoteTrufflesPath(t *testing.T) {
	if got := remoteTrufflesPath(Slave{}); got != "/usr/local/bin/truffles" {
		t.Fatalf("default = %q", got)
	}
	if got := remoteTrufflesPath(Slave{TrufflesPath: "truffles"}); got != "/usr/local/bin/truffles" {
		t.Fatalf("bare name = %q", got)
	}
	if got := remoteTrufflesPath(Slave{TrufflesPath: "/opt/bin/truffles"}); got != "/opt/bin/truffles" {
		t.Fatalf("custom = %q", got)
	}
}

func TestNeedsSudoInstall(t *testing.T) {
	if !needsSudoInstall("/usr/local/bin/truffles") {
		t.Fatal("expected sudo for /usr/local")
	}
	if needsSudoInstall("/home/u/bin/truffles") {
		t.Fatal("home install should not need sudo")
	}
}

func TestGCEResetArgsRequireZone(t *testing.T) {
	err := gceReset(Slave{GCEInstance: "x", GCEProject: "p"})
	if err == nil || !strings.Contains(err.Error(), "gce_zone") {
		t.Fatalf("err = %v, want gce_zone", err)
	}
}

func TestClusterAutoHealEnabled(t *testing.T) {
	off := false
	on := true
	if clusterAutoHealEnabled(&Cluster{AutoHeal: &off, Slaves: []Slave{{GCEInstance: "x", Enabled: true}}}) {
		t.Fatal("explicit false should win")
	}
	if !clusterAutoHealEnabled(&Cluster{AutoHeal: &on}) {
		t.Fatal("explicit true")
	}
	if !clusterAutoHealEnabled(&Cluster{Slaves: []Slave{{Name: "w", GCEInstance: "x", Enabled: true}}}) {
		t.Fatal("gce_instance should default auto_heal on")
	}
	if clusterAutoHealEnabled(&Cluster{Slaves: []Slave{{Name: "w", Enabled: true}}}) {
		t.Fatal("no gce → auto_heal off by default")
	}
}

func TestIsSSHTransportErr(t *testing.T) {
	if !isSSHTransportErr(fmt.Errorf("Timeout, server 1.2.3.4 not responding")) {
		t.Fatal("want timeout match")
	}
	if isSSHTransportErr(fmt.Errorf("trufflehog exited 1")) {
		t.Fatal("scan exit should not be transport")
	}
}

func TestIsStaleBinaryErr(t *testing.T) {
	if !isStaleBinaryErr(fmt.Errorf("exit status 1: flag provided but not defined: -skip-file")) {
		t.Fatal("want stale match")
	}
	if isStaleBinaryErr(fmt.Errorf("trufflehog exited 1")) {
		t.Fatal("normal exit should not be stale")
	}
}

func TestScanNoProxyFlag(t *testing.T) {
	if got := scanNoProxyFlag(nil); got != "-no-proxy" {
		t.Fatalf("nil = %q", got)
	}
	tr, fa := true, false
	if got := scanNoProxyFlag(&tr); got != "-no-proxy" {
		t.Fatalf("true = %q", got)
	}
	if got := scanNoProxyFlag(&fa); got != "-no-proxy=false" {
		t.Fatalf("false = %q", got)
	}
}
