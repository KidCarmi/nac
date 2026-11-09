package pilot

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kidcarmi/nac/internal/domain"
)

// Known defect D1 (ASTRA chaos run, pilot-rc10): a switch was reported as
// configured as soon as nacd had WRITTEN the FreeRADIUS client file. When
// FreeRADIUS rejected the file (an IPv4-mapped source IP), it kept the old
// list, refused to start at its next restart, and the console still said
// "configured: yes". Two fixes:
//   - only canonical dotted IPv4 is accepted (canonicalIPv4), so that input
//     can no longer produce an invalid or misleading client entry;
//   - "configured" is derived from what FreeRADIUS actually loaded: the
//     reload helper records the SHA-256 of the client file it validated and
//     the restart result, and nacd compares it with the list it would write
//     now (loadState).

// reloadGrace is how long a newly written client list may stay unconfirmed
// before it is reported as not loaded. The lab reload path settled within
// 12 s in ASTRA's chaos run (C1-C3).
const reloadGrace = 60 * time.Second

// canonicalIPv4 accepts only the dotted-quad form of an IPv4 address, exactly
// as FreeRADIUS will be given it. IPv4-mapped IPv6 (::ffff:10.0.0.1 or
// ::ffff:a00:1) is rejected: FreeRADIUS either refuses it or treats it as an
// IPv6 client, and it would bypass the uniqueness check on the source IP.
func canonicalIPv4(s string) (string, bool) {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return "", false
	}
	c := ip.To4().String()
	return c, c == s
}

// asIPv4 converts any IPv4 spelling (including IPv4-mapped IPv6) stored by an
// earlier version to the canonical form; false if it is not IPv4 at all.
func asIPv4(s string) (string, bool) {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return "", false
	}
	return ip.To4().String(), true
}

func (s *Service) reloadStatusFile() string {
	if s.cfg.ReloadStatusFile != "" {
		return s.cfg.ReloadStatusFile
	}
	if s.cfg.ClientsFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.cfg.ClientsFile), "freeradius-reload.status")
}

type reloadRecord struct {
	Result, SHA256, Detail string
	At                     *time.Time
}

func readReloadRecord(path string) (*reloadRecord, error) {
	b, err := os.ReadFile(path) //nolint:gosec // operator-configured path
	if err != nil {
		return nil, err
	}
	r := &reloadRecord{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "result":
			r.Result = v
		case "clients_sha256":
			r.SHA256 = v
		case "detail":
			r.Detail = v
		case "at":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				r.At = &t
			}
		}
	}
	return r, nil
}

// procHasFreeRADIUS reports whether a freeradius process runs on this host.
// The pilot runs nacd and FreeRADIUS on one host; nacd.service does not hide
// other processes. Ubuntu's freeradius.service runs it in the foreground
// without a PID file, so /proc is the dependable source.
func procHasFreeRADIUS() (bool, error) {
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, d := range dirs {
		if d.Name()[0] < '0' || d.Name()[0] > '9' {
			continue
		}
		if b, err := os.ReadFile("/proc/" + d.Name() + "/comm"); err != nil || strings.TrimSpace(string(b)) != "freeradius" {
			continue
		}
		// A zombie (state Z) or dead (X) process serves nothing.
		if st, err := os.ReadFile("/proc/" + d.Name() + "/stat"); err == nil {
			if i := bytes.LastIndexByte(st, ')'); i >= 0 && i+2 < len(st) && (st[i+2] == 'Z' || st[i+2] == 'X') {
				continue
			}
		}
		return true, nil
	}
	return false, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// loadState says whether the running FreeRADIUS has loaded the client list
// for the given switches (the current database state):
//
//	yes      the reload helper restarted FreeRADIUS successfully with exactly
//	         this list
//	pending  written, not yet confirmed (within reloadGrace)
//	no       not loaded: rejected, FreeRADIUS stopped/failed, write failed,
//	         or no confirmation within reloadGrace
//	unknown  nacd does not manage the client file
func (s *Service) loadState(sws []domain.Switch) StatusItem {
	now := s.now().UTC()
	if s.cfg.ClientsFile == "" {
		return StatusItem{State: "unknown", Detail: "NAC does not manage the FreeRADIUS client file (NAC_FREERADIUS_CLIENTS_FILE is not set); confirm the client in FreeRADIUS yourself"}
	}
	s.mu.Lock()
	wrote := s.clientsFileStatus
	s.mu.Unlock()
	if wrote.State == "no" {
		return StatusItem{State: "no", At: wrote.At, Detail: wrote.Detail}
	}
	want := sha256Hex([]byte(RenderClients(sws)))
	fi, err := os.Stat(s.cfg.ClientsFile)
	if err != nil {
		return StatusItem{State: "no", Detail: "client file " + s.cfg.ClientsFile + " is missing: " + err.Error()}
	}
	written := fi.ModTime().UTC()
	pendingOrNo := func(why string) StatusItem {
		if now.Sub(written) < reloadGrace {
			return StatusItem{State: "pending", At: &written, Detail: "written at " + written.Format(time.RFC3339) + "; waiting for FreeRADIUS to load it (" + why + ")"}
		}
		return StatusItem{State: "no", At: &written, Detail: fmt.Sprintf("FreeRADIUS has not loaded the client list written %s ago (%s). "+
			"Check: systemctl status nac-freeradius-reload.path nac-freeradius-reload.service freeradius", now.Sub(written).Truncate(time.Second), why)}
	}
	if b, err := os.ReadFile(s.cfg.ClientsFile); err != nil || sha256Hex(b) != want {
		return pendingOrNo("the file does not yet hold the current switch list")
	}
	rec, err := readReloadRecord(s.reloadStatusFile())
	if err != nil {
		return pendingOrNo("no reload result in " + s.reloadStatusFile() + " yet")
	}
	if rec.SHA256 != want {
		last := ""
		if rec.At != nil {
			last = " at " + rec.At.Format(time.RFC3339)
		}
		return pendingOrNo("the last reload" + last + " was for a different switch list")
	}
	switch rec.Result {
	case "ok":
		// The record says FreeRADIUS loaded this list when it restarted. A
		// FreeRADIUS restarted since (Restart=on-failure, reboot) read the
		// same file, but one that has since stopped or crashed serves
		// nothing, so the record alone is not enough.
		if !s.cfg.SkipProcessCheck {
			running, err := s.frRunning()
			switch {
			case err != nil:
				return StatusItem{State: "unknown", At: rec.At, Detail: "FreeRADIUS loaded this switch list, but nacd cannot tell whether it is still running: " + err.Error()}
			case !running:
				return StatusItem{State: "no", At: rec.At, Detail: "FreeRADIUS loaded this switch list but is not running now (no freeradius process): check systemctl status freeradius"}
			}
		}
		return StatusItem{State: "yes", At: rec.At, Detail: "FreeRADIUS restarted with this switch list and is running"}
	default:
		return StatusItem{State: "no", At: rec.At, Detail: "FreeRADIUS did not load this switch list: " + rec.Detail}
	}
}

// NormalizeStoredIPs rewrites IPv4 addresses that an earlier version stored
// in a non-canonical spelling (e.g. ::ffff:10.0.0.1) to dotted IPv4, so the
// client file, the RADIUS source lookup and the uniqueness check all see the
// same string. A rewrite that would collide with another switch is left for
// the operator and logged; RenderClients skips the duplicate.
func (s *Service) NormalizeStoredIPs(ctx context.Context) {
	sws, err := s.store.ListSwitches(ctx)
	if err != nil {
		s.logger.Error("could not check stored switch addresses", "error", err)
		return
	}
	taken := map[string]string{}
	for _, sw := range sws {
		if c, ok := canonicalIPv4(sw.RadiusSourceIP); ok {
			taken[c] = sw.Name
		}
	}
	for i := range sws {
		sw := &sws[i]
		src, okSrc := asIPv4(sw.RadiusSourceIP)
		nas, okNAS := asIPv4(sw.NASIP)
		if (!okSrc || src == sw.RadiusSourceIP) && (!okNAS || nas == sw.NASIP) {
			continue
		}
		if okSrc && src != sw.RadiusSourceIP {
			if other, dup := taken[src]; dup && other != sw.Name {
				s.logger.Error("switch source IP duplicates another switch after normalisation; fix it under Switches",
					"switch", sw.Name, "stored", sw.RadiusSourceIP, "other", other)
				continue
			}
		}
		from := sw.RadiusSourceIP + " / " + sw.NASIP
		if okSrc {
			sw.RadiusSourceIP = src
			taken[src] = sw.Name
		}
		if okNAS {
			sw.NASIP = nas
		}
		sw.UpdatedAt = s.now().UTC()
		if err := s.store.SaveSwitch(ctx, sw); err != nil {
			s.logger.Error("could not normalise switch address", "switch", sw.Name, "error", err)
			continue
		}
		s.audit(ctx, "switch.normalize_ip", "switch", sw.ID.String(), "success",
			map[string]string{"name": sw.Name, "from": from, "to": sw.RadiusSourceIP + " / " + sw.NASIP})
		s.logger.Warn("normalised switch address to dotted IPv4", "switch", sw.Name, "from", from)
	}
}
