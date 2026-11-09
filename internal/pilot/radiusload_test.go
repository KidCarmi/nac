package pilot

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kidcarmi/nac/internal/domain"
)

func TestCanonicalIPv4(t *testing.T) {
	for in, want := range map[string]bool{
		"10.70.99.1": true, "192.0.2.255": true,
		"::ffff:10.70.99.1": false, // ASTRA C4: accepted by rc10, rejected by FreeRADIUS -C
		"::ffff:a46:6301":   false, // loads, but FreeRADIUS treats it as an IPv6 client
		"010.70.99.1":       false, "10.70.99": false, " 10.70.99.1": false, "2001:db8::1": false, "": false,
	} {
		if _, ok := canonicalIPv4(in); ok != want {
			t.Errorf("canonicalIPv4(%q) = %v, want %v", in, ok, want)
		}
	}
}

func TestRenderClientsNeverEmitsMappedOrDuplicateAddresses(t *testing.T) {
	out := RenderClients([]domain.Switch{
		{Name: "a", RadiusSourceIP: "::ffff:10.0.0.1", RadiusSecret: "s"}, // stored by rc10
		{Name: "b", RadiusSourceIP: "10.0.0.1", RadiusSecret: "s"},        // same switch again
		{Name: "c", RadiusSourceIP: "2001:db8::1", RadiusSecret: "s"},
	})
	if strings.Contains(out, "::ffff") || strings.Contains(out, "ipaddr = 2001") {
		t.Fatalf("non-IPv4 address rendered:\n%s", out)
	}
	if strings.Count(out, "ipaddr = 10.0.0.1") != 1 || !strings.Contains(out, "# skipped nac-b") || !strings.Contains(out, "# skipped nac-c") {
		t.Fatalf("duplicate/invalid entries not skipped:\n%s", out)
	}
}

type listStore struct {
	Store
	sws []domain.Switch
}

func (f *listStore) ListSwitches(context.Context) ([]domain.Switch, error) { return f.sws, nil }

func TestLoadState(t *testing.T) {
	dir := t.TempDir()
	clients := filepath.Join(dir, "freeradius-clients.conf")
	statusFile := filepath.Join(dir, "freeradius-reload.status")
	sws := []domain.Switch{{Name: "sw1", RadiusSourceIP: "10.0.0.1", RadiusSecret: "lab-radius-secret-0123456789"}}
	s := New(&listStore{sws: sws}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{ClientsFile: clients})
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	running := true
	s.frRunning = func() (bool, error) { return running, nil }
	s.WriteClientsFile(context.Background())
	sum := sha256Hex([]byte(RenderClients(sws)))
	rec := func(result, sha, detail string) {
		if err := os.WriteFile(statusFile, []byte("result="+result+"\nclients_sha256="+sha+"\nat=2026-11-09T12:00:00Z\ndetail="+detail+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(name, state, detailPart string) {
		t.Helper()
		cur, _ := s.store.ListSwitches(context.Background())
		got := s.loadState(cur)
		if got.State != state || !strings.Contains(got.Detail, detailPart) {
			t.Fatalf("%s: got %s %q, want %s containing %q", name, got.State, got.Detail, state, detailPart)
		}
	}

	expect("no record yet", "pending", "no reload result")
	rec("ok", "0000", "")
	expect("record for an older list", "pending", "different switch list")
	rec("fail", sum, "freeradius -C rejected the configuration")
	expect("this list rejected", "no", "freeradius -C rejected")
	rec("not_running", sum, "FreeRADIUS is stopped")
	expect("FreeRADIUS stopped", "no", "stopped")
	rec("ok", sum, "FreeRADIUS restarted with this client list")
	expect("loaded", "yes", "restarted with this switch list")
	running = false
	expect("loaded, then FreeRADIUS stopped or crashed", "no", "not running now")
	running = true

	// A change in the database that FreeRADIUS has not loaded is not "yes",
	// even though the file was written and the old list was loaded.
	sws2 := append([]domain.Switch{}, sws...)
	sws2 = append(sws2, domain.Switch{Name: "sw2", RadiusSourceIP: "10.0.0.2", RadiusSecret: "x"})
	s.store = &listStore{sws: sws2}
	s.WriteClientsFile(context.Background())
	expect("new switch, old load", "pending", "different switch list")
	now = now.Add(2 * reloadGrace)
	expect("never confirmed", "no", "has not loaded")

	s.cfg.ClientsFile = ""
	expect("unmanaged", "unknown", "does not manage")
}

// ASTRA (pilot-aruba-rc2 chaos C2/C3): a burst left FreeRADIUS on an older
// list and no further reload ran. The backstop rewrites the identical file
// (a new inode, so the systemd path unit fires) once the list stays
// unconfirmed, rate-limited, and stops as soon as the record matches.
func TestReloadBackstop(t *testing.T) {
	dir := t.TempDir()
	clients := filepath.Join(dir, "freeradius-clients.conf")
	statusFile := filepath.Join(dir, "freeradius-reload.status")
	sws := []domain.Switch{{Name: "sw1", RadiusSourceIP: "10.0.0.1", RadiusSecret: "lab-radius-secret-0123456789"}}
	s := New(&listStore{sws: sws}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{ClientsFile: clients})
	s.frRunning = func() (bool, error) { return true, nil }
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	ctx := context.Background()
	s.WriteClientsFile(ctx)
	want := sha256Hex([]byte(RenderClients(sws)))
	if err := os.WriteFile(statusFile, []byte("result=ok\nclients_sha256=older\nat=2026-11-09T18:47:21Z\ndetail=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fiBefore, _ := os.Stat(clients)
	rewritten := func() bool {
		t.Helper()
		fi, err := os.Stat(clients)
		if err != nil {
			t.Fatal(err)
		}
		changed := !os.SameFile(fiBefore, fi)
		fiBefore = fi
		return changed
	}

	now = now.Add(10 * time.Second)
	s.ReloadBackstop(ctx)
	if rewritten() {
		t.Fatal("re-triggered before reloadRetriggerAfter: the normal path may still be working")
	}
	now = now.Add(15 * time.Second) // 25 s after the write
	s.ReloadBackstop(ctx)
	if !rewritten() {
		t.Fatal("unconfirmed list was not re-triggered")
	}
	if b, _ := os.ReadFile(clients); sha256Hex(b) != want {
		t.Fatal("backstop changed the switch list")
	}
	now = now.Add(10 * time.Second)
	s.ReloadBackstop(ctx)
	if rewritten() {
		t.Fatal("re-triggered again within reloadRetriggerEvery")
	}
	// The rewrites must not reset the grace period: a reload path that never
	// answers ends in "no", not an endless "pending".
	now = now.Add(time.Minute)
	if st := s.loadState(sws); st.State != "no" {
		t.Fatalf("never-confirmed list after the grace period: %s %q", st.State, st.Detail)
	}
	s.ReloadBackstop(ctx)
	if !rewritten() {
		t.Fatal("still unconfirmed after reloadRetriggerEvery: expected another re-trigger")
	}
	if err := os.WriteFile(statusFile, []byte("result=ok\nclients_sha256="+want+"\nat=2026-11-09T18:48:00.123Z\ndetail=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	s.ReloadBackstop(ctx)
	if rewritten() {
		t.Fatal("re-triggered although FreeRADIUS confirmed this list")
	}
	if st := s.loadState(sws); st.State != "yes" || st.At == nil || st.At.Nanosecond() != 123e6 {
		t.Fatalf("confirmed list (with the helper's millisecond time): %s %v %q", st.State, st.At, st.Detail)
	}
}
