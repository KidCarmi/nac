// Package pilot implements the operator workflows of the wired pilot:
// switch onboarding and status, identity bindings, session troubleshooting
// and active-session CoA actions.
package pilot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

var (
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	portRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9/.:-]{0,63}$`)
	segRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

// TestedEOSVersion is the only EOS version the generated configuration has
// been validated on (ASTRA lab, vEOS-lab 4.29.2F).
const TestedEOSVersion = "4.29.2F"

// SwitchInput is the operator's register/update payload.
type SwitchInput struct {
	Name           string         `json:"name"`
	RadiusSourceIP string         `json:"radius_source_ip"`
	NASIP          string         `json:"nas_ip"`
	RadiusSecret   string         `json:"radius_secret"` // empty on update = keep
	CoAPort        int            `json:"coa_port"`
	SegmentVLANs   map[string]int `json:"segment_vlans"`
	AccessPorts    []string       `json:"access_ports"`
	RadiusServerIP string         `json:"radius_server_ip"`
	EAPIURL        string         `json:"eapi_url"`
	EAPIUsername   string         `json:"eapi_username"`
	EAPIPassword   string         `json:"eapi_password"` // empty on update = keep
	ClearEAPI      bool           `json:"clear_eapi"`
}

func validateSecret(s string) error {
	if len(s) < 16 || len(s) > 128 {
		return errors.New("must be 16-128 characters")
	}
	for _, r := range s {
		// '$' is refused because FreeRADIUS expands ${...} inside quoted
		// configuration values, so the loaded secret would differ from the
		// one configured on the switch.
		if r < 0x21 || r > 0x7e || r == '"' || r == '\\' || r == '$' {
			return errors.New("must be printable ASCII without spaces, quotes, backslashes or '$' (it is written into FreeRADIUS and EOS configuration)")
		}
	}
	return nil
}

func (s *Service) validateSwitch(in *SwitchInput, creating bool) error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	in.Name = strings.TrimSpace(in.Name)
	if !nameRe.MatchString(in.Name) {
		add("name: 1-63 letters, digits, '.', '_' or '-'")
	}
	in.RadiusSourceIP, in.NASIP = strings.TrimSpace(in.RadiusSourceIP), strings.TrimSpace(in.NASIP)
	if _, ok := canonicalIPv4(in.RadiusSourceIP); !ok {
		add("radius_source_ip: must be the IPv4 address FreeRADIUS sees as the packet source, in dotted form such as 10.70.99.1 " +
			"(IPv4-mapped IPv6 such as ::ffff:10.70.99.1 is not accepted: FreeRADIUS rejects or misreads it)")
	}
	if in.NASIP == "" {
		in.NASIP = in.RadiusSourceIP
	}
	if _, ok := canonicalIPv4(in.NASIP); !ok {
		add("nas_ip: must be an IPv4 address in dotted form such as 10.70.99.1 (sent as NAS-IP-Address in CoA)")
	}
	if in.RadiusSecret != "" || creating {
		if err := validateSecret(in.RadiusSecret); err != nil {
			add("radius_secret: %v", err)
		}
	}
	if in.CoAPort == 0 {
		in.CoAPort = 3799
	}
	if in.CoAPort < 1 || in.CoAPort > 65535 {
		add("coa_port: 1-65535")
	}
	if len(in.SegmentVLANs) == 0 {
		add(`segment_vlans: map each segment to a VLAN, e.g. {"corp": 10, "quarantine": 20}`)
	}
	seen := map[int]string{}
	for seg, vlan := range in.SegmentVLANs {
		if !segRe.MatchString(seg) {
			add("segment_vlans: segment %q must be lower-case letters, digits, '-' or '_'", seg)
		}
		if vlan < 1 || vlan > 4094 {
			add("segment_vlans: VLAN for %q must be 1-4094", seg)
		}
		if other, dup := seen[vlan]; dup {
			add("segment_vlans: %q and %q both map to VLAN %d", seg, other, vlan)
		}
		seen[vlan] = seg
	}
	if _, ok := in.SegmentVLANs["quarantine"]; !ok {
		add(`segment_vlans: a "quarantine" segment is required (operator session quarantine uses it)`)
	}
	for _, p := range in.AccessPorts {
		if !portRe.MatchString(p) {
			add("access_ports: %q is not an interface name such as Ethernet1", p)
		}
	}
	if in.RadiusServerIP != "" {
		if ip := net.ParseIP(in.RadiusServerIP); ip == nil {
			add("radius_server_ip: must be an IP address (the FreeRADIUS address the switch sends to)")
		}
	}
	if in.EAPIURL != "" && !strings.HasPrefix(in.EAPIURL, "https://") && !strings.HasPrefix(in.EAPIURL, "http://") {
		add("eapi_url: must start with https:// (e.g. https://10.70.99.1)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", domain.ErrInvalidInput, strings.Join(errs, "; "))
	}
	return nil
}

// CreateSwitch registers a switch and regenerates the FreeRADIUS client file.
func (s *Service) CreateSwitch(ctx context.Context, in SwitchInput, actor string) (*domain.Switch, error) {
	if err := s.validateSwitch(&in, true); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	sw := &domain.Switch{ID: uuid.New(), Name: in.Name, RadiusSourceIP: in.RadiusSourceIP, NASIP: in.NASIP,
		RadiusSecret: in.RadiusSecret, CoAPort: in.CoAPort, SegmentVLANs: in.SegmentVLANs, AccessPorts: in.AccessPorts,
		EOSVersion: TestedEOSVersion, RadiusServerIP: in.RadiusServerIP, EAPIURL: strings.TrimRight(in.EAPIURL, "/"),
		EAPIUsername: in.EAPIUsername, EAPIPassword: in.EAPIPassword, CreatedAt: now, UpdatedAt: now}
	if err := s.store.SaveSwitch(ctx, sw); err != nil {
		return nil, err
	}
	s.afterSwitchChange(ctx, actor, "switch.create", sw)
	return s.store.GetSwitch(ctx, sw.ID)
}

// UpdateSwitch changes a switch. Empty secret/password fields keep the
// stored values.
func (s *Service) UpdateSwitch(ctx context.Context, id uuid.UUID, in SwitchInput, actor string) (*domain.Switch, error) {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.validateSwitch(&in, false); err != nil {
		return nil, err
	}
	sw.Name, sw.RadiusSourceIP, sw.NASIP, sw.CoAPort = in.Name, in.RadiusSourceIP, in.NASIP, in.CoAPort
	sw.SegmentVLANs, sw.AccessPorts, sw.RadiusServerIP = in.SegmentVLANs, in.AccessPorts, in.RadiusServerIP
	if in.RadiusSecret != "" {
		sw.RadiusSecret = in.RadiusSecret
	}
	if in.ClearEAPI {
		sw.EAPIURL, sw.EAPIUsername, sw.EAPIPassword = "", "", ""
	} else {
		sw.EAPIURL, sw.EAPIUsername = strings.TrimRight(in.EAPIURL, "/"), in.EAPIUsername
		if in.EAPIPassword != "" {
			sw.EAPIPassword = in.EAPIPassword
		}
	}
	sw.UpdatedAt = s.now().UTC()
	if err := s.store.SaveSwitch(ctx, sw); err != nil {
		return nil, err
	}
	s.afterSwitchChange(ctx, actor, "switch.update", sw)
	return s.store.GetSwitch(ctx, sw.ID)
}

// DeleteSwitch removes a switch.
func (s *Service) DeleteSwitch(ctx context.Context, id uuid.UUID, actor string) error {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSwitch(ctx, id); err != nil {
		return err
	}
	s.afterSwitchChange(ctx, actor, "switch.delete", sw)
	return nil
}

func (s *Service) afterSwitchChange(ctx context.Context, actor, action string, sw *domain.Switch) {
	res := s.WriteClientsFile(ctx)
	s.audit(ctx, action, "switch", sw.ID.String(), "success", map[string]string{"name": sw.Name, "clients_file": res.State})
}

// SwitchStatus separates the four onboarding facts an operator needs.
type SwitchStatus struct {
	// Configured is "yes" only when the switch is registered AND the
	// running FreeRADIUS has loaded the client list that contains it
	// (FreeRADIUSLoad); see known defect D1.
	Configured  StatusItem `json:"configured"`
	Reachable   StatusItem `json:"reachable"`
	AuthSeen    StatusItem `json:"auth_request_seen"`
	Verified    StatusItem `json:"verified"`
	ClientsFile StatusItem `json:"radius_client_file"`
	// FreeRADIUSLoad says whether the running FreeRADIUS has loaded the
	// current client list: yes | pending | no | unknown.
	FreeRADIUSLoad StatusItem `json:"freeradius_load"`
}

// StatusItem is one status fact.
type StatusItem struct {
	State  string     `json:"state"` // yes | no | unknown
	At     *time.Time `json:"at,omitempty"`
	Detail string     `json:"detail"`
	// Historical marks a record of a past event (Verified) as opposed to
	// current state; Note then says how that record relates to now.
	Historical bool   `json:"historical,omitempty"`
	Note       string `json:"note,omitempty"`
}

// SwitchView is a switch plus its status.
type SwitchView struct {
	domain.Switch
	Status SwitchStatus `json:"status"`
}

// ListSwitches returns all switches with status.
func (s *Service) ListSwitches(ctx context.Context) ([]SwitchView, error) {
	sws, err := s.store.ListSwitches(ctx)
	if err != nil {
		return nil, err
	}
	load := s.loadState(sws)
	out := make([]SwitchView, 0, len(sws))
	for _, sw := range sws {
		out = append(out, SwitchView{Switch: sw, Status: s.status(ctx, &sw, load)})
	}
	return out, nil
}

// GetSwitch returns one switch with status.
func (s *Service) GetSwitch(ctx context.Context, id uuid.UUID) (*SwitchView, error) {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return nil, err
	}
	sws, err := s.store.ListSwitches(ctx)
	if err != nil {
		return nil, err
	}
	return &SwitchView{Switch: *sw, Status: s.status(ctx, sw, s.loadState(sws))}, nil
}

func (s *Service) status(ctx context.Context, sw *domain.Switch, load StatusItem) SwitchStatus {
	st := SwitchStatus{FreeRADIUSLoad: load}
	if sw.SecretSet && len(sw.SegmentVLANs) > 0 {
		at := load.At
		if at == nil {
			at = &sw.UpdatedAt
		}
		st.Configured = StatusItem{State: load.State, At: at, Detail: fmt.Sprintf("registered in NAC: RADIUS source %s, VLANs %s. FreeRADIUS: %s. "+
			"The switch itself is configured by applying the generated EOS configuration; NAC cannot confirm that until a request arrives.",
			sw.RadiusSourceIP, formatVLANs(sw.SegmentVLANs), load.Detail)}
	} else {
		st.Configured = StatusItem{State: "no", Detail: "RADIUS secret or VLAN mapping missing"}
	}
	switch {
	case sw.EAPIURL == "":
		st.Reachable = StatusItem{State: "unknown", Detail: "no switch management (eAPI) access configured; NAC cannot probe the switch. RADIUS requests still prove the RADIUS path."}
	case sw.LastProbeAt == nil:
		st.Reachable = StatusItem{State: "unknown", Detail: "not probed yet"}
	case sw.LastProbeOK != nil && *sw.LastProbeOK:
		st.Reachable = StatusItem{State: "yes", At: sw.LastProbeAt, Detail: sw.LastProbeDetail}
	default:
		st.Reachable = StatusItem{State: "no", At: sw.LastProbeAt, Detail: sw.LastProbeDetail}
	}
	if sw.LastRadiusAt != nil {
		st.AuthSeen = StatusItem{State: "yes", At: sw.LastRadiusAt, Detail: "FreeRADIUS forwarded a request from " + sw.RadiusSourceIP}
	} else {
		st.AuthSeen = StatusItem{State: "no", Detail: "no RADIUS request from " + sw.RadiusSourceIP + " has reached NAC yet. Check the switch radius-server host/key and the FreeRADIUS client entry."}
	}
	if sw.VerifiedAt != nil {
		// Verified is a commissioning record (the switch once carried a NAC
		// decision end to end), not live state. Say which session it was
		// and whether that session is still the active one, so it is never
		// read as describing the current session (ASTRA, pilot-rc7).
		st.Verified = StatusItem{State: "yes", At: sw.VerifiedAt, Detail: sw.VerifiedDetail, Historical: true,
			Note: s.verifiedNote(ctx, sw)}
	} else {
		st.Verified = StatusItem{State: "no", Detail: "run Verify after a test endpoint authenticates"}
	}
	s.mu.Lock()
	st.ClientsFile = s.clientsFileStatus
	s.mu.Unlock()
	return st
}

// verifiedNote relates the Verified record to the present.
func (s *Service) verifiedNote(ctx context.Context, sw *domain.Switch) string {
	const tail = " For the current state, see Reachable and each session's observation and liveness."
	if sw.VerifiedSessionID == nil {
		return "Record of a past verification." + tail
	}
	id := sw.VerifiedSessionID.String()
	sess, err := s.store.GetSession(ctx, *sw.VerifiedSessionID)
	switch {
	case err != nil:
		return "Record of a past verification with session " + id + ", which no longer exists." + tail
	case sess.State == domain.SessionActive:
		return "Verified with session " + id + ", which is still active." + tail
	case sess.EndedAt != nil:
		return "Record of a past verification: session " + id + " ended at " + sess.EndedAt.UTC().Format(time.RFC3339) + "." + tail
	default:
		return "Record of a past verification: session " + id + " is no longer active (" + string(sess.State) + ")." + tail
	}
}

func formatVLANs(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// EOSConfig renders the switch configuration. The mandatory part reproduces
// the configuration validated on vEOS-lab 4.29.2F (lab/configs.sanitized.txt);
// optional, unvalidated lines are emitted only as comments.
func EOSConfig(sw *domain.Switch, includeSecret bool) string {
	key := "<RADIUS_SHARED_SECRET>"
	if includeSecret {
		key = sw.RadiusSecret
	}
	server := sw.RadiusServerIP
	if server == "" {
		server = "<FREERADIUS_IP>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "! NAC pilot configuration for %s\n", sw.Name)
	fmt.Fprintf(&b, "! Validated on EOS %s (vEOS-lab, NAC pilot lab). Other versions are untested.\n", TestedEOSVersion)
	b.WriteString("! Apply in configuration mode. Review before applying to a production switch.\n!\n")
	segs := make([]string, 0, len(sw.SegmentVLANs))
	for seg := range sw.SegmentVLANs {
		segs = append(segs, seg)
	}
	sort.Slice(segs, func(i, j int) bool { return sw.SegmentVLANs[segs[i]] < sw.SegmentVLANs[segs[j]] })
	for _, seg := range segs {
		fmt.Fprintf(&b, "vlan %d\n   name %s\n", sw.SegmentVLANs[seg], seg)
	}
	fmt.Fprintf(&b, "!\nradius-server host %s key %s\n", server, key)
	b.WriteString("aaa authentication dot1x default group radius\n")
	b.WriteString("dot1x system-auth-control\n")
	b.WriteString("! Accept CoA from NAC (validated: CoA-Request with NAS-IP-Address + NAS-Port-Id)\n")
	b.WriteString("dot1x dynamic-authorization\n!\n")
	ports := sw.AccessPorts
	if len(ports) == 0 {
		ports = []string{"<ACCESS_INTERFACE>"}
	}
	initial := sw.SegmentVLANs["corp"]
	if initial == 0 {
		initial = sw.SegmentVLANs["quarantine"]
	}
	for _, p := range ports {
		fmt.Fprintf(&b, "interface %s\n   switchport access vlan %d\n   dot1x pae authenticator\n   dot1x port-control auto\n", p, initial)
	}
	b.WriteString("!\n! ---- OPTIONAL, NOT validated on EOS 4.29.2F in the pilot lab ----\n")
	b.WriteString("! RADIUS accounting (lets NAC end sessions on logout/link-down):\n")
	b.WriteString("!   aaa accounting dot1x default start-stop group radius\n")
	b.WriteString("! Periodic re-authentication (bounds how long a session survives a NAC outage):\n")
	for _, p := range ports {
		fmt.Fprintf(&b, "!   interface %s\n!      dot1x reauthentication\n!      dot1x timeout reauth-period 3600\n", p)
	}
	b.WriteString("! eAPI for NAC session observation (`show dot1x hosts | json`); needs a NAC-reachable management address:\n")
	b.WriteString("!   management api http-commands\n!      no shutdown\n")
	return b.String()
}

// SwitchConfig returns the generated configuration.
func (s *Service) SwitchConfig(ctx context.Context, id uuid.UUID, includeSecret bool, actor string) (string, error) {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return "", err
	}
	if includeSecret {
		s.audit(ctx, "switch.config.reveal_secret", "switch", sw.ID.String(), "success", map[string]string{"name": sw.Name})
	}
	return EOSConfig(sw, includeSecret), nil
}

// RenderClients renders the FreeRADIUS client definitions for all switches.
func RenderClients(sws []domain.Switch) string {
	var b strings.Builder
	b.WriteString("# Generated by nacd. Do not edit: it is rewritten on every switch change.\n")
	b.WriteString("# FreeRADIUS reads clients only at start; nac-freeradius-reload.path restarts it when this file changes.\n")
	seen := map[string]string{}
	for _, sw := range sws {
		// Defence in depth for rows stored before canonical IPv4 was
		// enforced: never hand FreeRADIUS an address it rejects or reads as
		// IPv6, and never two clients with one address (known defect D1).
		ip, ok := asIPv4(sw.RadiusSourceIP)
		if !ok {
			fmt.Fprintf(&b, "\n# skipped nac-%s: source %q is not IPv4\n", sw.Name, sw.RadiusSourceIP)
			continue
		}
		if other, dup := seen[ip]; dup {
			fmt.Fprintf(&b, "\n# skipped nac-%s: source %s is already used by nac-%s\n", sw.Name, ip, other)
			continue
		}
		seen[ip] = sw.Name
		fmt.Fprintf(&b, "\nclient %s {\n\tipaddr = %s\n\tsecret = \"%s\"\n\tshortname = %s\n\tnas_type = other\n}\n",
			"nac-"+sw.Name, ip, sw.RadiusSecret, sw.Name)
	}
	return b.String()
}

// WriteClientsFile atomically rewrites NAC_FREERADIUS_CLIENTS_FILE.
func (s *Service) WriteClientsFile(ctx context.Context) StatusItem {
	now := s.now().UTC()
	var st StatusItem
	if s.cfg.ClientsFile == "" {
		st = StatusItem{State: "unknown", At: &now, Detail: "NAC_FREERADIUS_CLIENTS_FILE is not set: add the client to FreeRADIUS clients.conf manually"}
	} else {
		s.clientsMu.Lock()
		sws, err := s.store.ListSwitches(ctx)
		if err == nil {
			data := []byte(RenderClients(sws))
			if err = writeAtomic(s.cfg.ClientsFile, data, 0o640); err == nil {
				s.noteDesired(sha256Hex(data))
			}
		}
		s.clientsMu.Unlock()
		if err != nil {
			st = StatusItem{State: "no", At: &now, Detail: "could not write " + s.cfg.ClientsFile + ": " + err.Error()}
		} else {
			st = StatusItem{State: "yes", At: &now, Detail: fmt.Sprintf("%d switch(es) written to %s; FreeRADIUS restarts to load them (see freeradius_load for whether it did)", len(sws), s.cfg.ClientsFile)}
		}
	}
	s.mu.Lock()
	s.clientsFileStatus = st
	s.mu.Unlock()
	return st
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nac-clients-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
