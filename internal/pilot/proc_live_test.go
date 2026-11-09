package pilot

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// Runs only where FreeRADIUS is installed (NAC_TEST_FREERADIUS=1): checks the
// /proc lookup against a real freeradius process, then after it exits.
func TestProcHasFreeRADIUS_Live(t *testing.T) {
	if os.Getenv("NAC_TEST_FREERADIUS") != "1" {
		t.Skip("set NAC_TEST_FREERADIUS=1 on a host with FreeRADIUS installed")
	}
	cmd := exec.Command("/usr/sbin/freeradius", "-f", "-d", "/etc/freeradius/3.0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if ok, err := procHasFreeRADIUS(); err != nil || !ok {
		t.Fatalf("running freeradius not found: %v %v", ok, err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // reaped: no zombie left behind
	if ok, _ := procHasFreeRADIUS(); ok {
		t.Fatal("freeradius reported running after it exited")
	}
}
