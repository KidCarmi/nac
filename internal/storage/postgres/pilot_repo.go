package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
	"github.com/kidcarmi/nac/internal/secrets"
)

// PilotRepo persists switches, bindings, sessions, decisions, timeline events
// and CoA requests. Secrets are sealed with box before they reach the database.
type PilotRepo struct {
	pool *pgxpool.Pool
	box  *secrets.Box
}

// NewPilotRepo constructs the repo. box may be nil only if no switch secrets
// are ever written (tests of read paths).
func NewPilotRepo(pool *pgxpool.Pool, box *secrets.Box) *PilotRepo {
	return &PilotRepo{pool: pool, box: box}
}

// ---------------------------------------------------------------- switches

const switchColumns = `id, name, radius_source_ip, nas_ip, radius_secret_enc, coa_port, segment_vlans, access_ports,
	eos_version, radius_server_ip, eapi_url, eapi_username, eapi_password_enc, last_radius_at, last_probe_at,
	last_probe_ok, last_probe_detail, verified_at, verified_detail, verified_session_id, created_at, updated_at,
	platform, enforcement, segment_roles, access_vlan`

func (r *PilotRepo) scanSwitch(row rowScanner) (*domain.Switch, error) {
	var (
		s                domain.Switch
		secretEnc, pwEnc string
		vlans, ports     []byte
		roles            []byte
	)
	if err := row.Scan(&s.ID, &s.Name, &s.RadiusSourceIP, &s.NASIP, &secretEnc, &s.CoAPort, &vlans, &ports,
		&s.EOSVersion, &s.RadiusServerIP, &s.EAPIURL, &s.EAPIUsername, &pwEnc, &s.LastRadiusAt, &s.LastProbeAt,
		&s.LastProbeOK, &s.LastProbeDetail, &s.VerifiedAt, &s.VerifiedDetail, &s.VerifiedSessionID, &s.CreatedAt, &s.UpdatedAt,
		&s.Platform, &s.Enforcement, &roles, &s.AccessVLAN); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(vlans, &s.SegmentVLANs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(ports, &s.AccessPorts); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(roles, &s.SegmentRoles); err != nil {
		return nil, err
	}
	s.SecretSet, s.EAPIPasswordSet = secretEnc != "", pwEnc != ""
	if r.box != nil {
		var err error
		if s.RadiusSecret, err = r.box.Open(secretEnc); err != nil {
			return nil, fmt.Errorf("switch %s RADIUS secret: %w", s.Name, err)
		}
		if s.EAPIPassword, err = r.box.Open(pwEnc); err != nil {
			return nil, fmt.Errorf("switch %s eAPI password: %w", s.Name, err)
		}
	}
	return &s, nil
}

func (r *PilotRepo) oneSwitch(ctx context.Context, where string, arg any) (*domain.Switch, error) {
	s, err := r.scanSwitch(r.pool.QueryRow(ctx, `SELECT `+switchColumns+` FROM switches WHERE `+where, arg))
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return s, err
}

// GetSwitch returns a switch by ID.
func (r *PilotRepo) GetSwitch(ctx context.Context, id uuid.UUID) (*domain.Switch, error) {
	return r.oneSwitch(ctx, "id = $1", id)
}

// SwitchBySourceIP resolves the RADIUS packet source to a switch.
func (r *PilotRepo) SwitchBySourceIP(ctx context.Context, ip string) (*domain.Switch, error) {
	return r.oneSwitch(ctx, "radius_source_ip = $1", ip)
}

// ListSwitches lists all switches by name.
func (r *PilotRepo) ListSwitches(ctx context.Context) ([]domain.Switch, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+switchColumns+` FROM switches ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Switch{}
	for rows.Next() {
		s, err := r.scanSwitch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// SaveSwitch inserts or updates a switch (by ID).
func (r *PilotRepo) SaveSwitch(ctx context.Context, s *domain.Switch) error {
	if r.box == nil {
		return secrets.ErrNoKey
	}
	secretEnc, err := r.box.Seal(s.RadiusSecret)
	if err != nil {
		return err
	}
	pwEnc, err := r.box.Seal(s.EAPIPassword)
	if err != nil {
		return err
	}
	vlans, _ := json.Marshal(s.SegmentVLANs)
	if s.AccessPorts == nil {
		s.AccessPorts = []string{}
	}
	ports, _ := json.Marshal(s.AccessPorts)
	if s.SegmentVLANs == nil {
		vlans = []byte("{}")
	}
	if s.SegmentRoles == nil {
		s.SegmentRoles = map[string]string{}
	}
	roles, _ := json.Marshal(s.SegmentRoles)
	if s.Platform == "" {
		s.Platform = domain.PlatformAristaEOS
	}
	if s.Enforcement == "" {
		s.Enforcement = domain.EnforcementVLAN
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO switches (id, name, radius_source_ip, nas_ip, radius_secret_enc, coa_port, segment_vlans, access_ports,
			eos_version, radius_server_ip, eapi_url, eapi_username, eapi_password_enc, created_at, updated_at,
			platform, enforcement, segment_roles, access_vlan)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (id) DO UPDATE SET name=$2, radius_source_ip=$3, nas_ip=$4, radius_secret_enc=$5, coa_port=$6,
			segment_vlans=$7, access_ports=$8, eos_version=$9, radius_server_ip=$10, eapi_url=$11, eapi_username=$12,
			eapi_password_enc=$13, updated_at=$15, platform=$16, enforcement=$17, segment_roles=$18, access_vlan=$19`,
		s.ID, s.Name, s.RadiusSourceIP, s.NASIP, secretEnc, s.CoAPort, vlans, ports, s.EOSVersion, s.RadiusServerIP,
		s.EAPIURL, s.EAPIUsername, pwEnc, s.CreatedAt, s.UpdatedAt, s.Platform, s.Enforcement, roles, s.AccessVLAN)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: a switch with this name or RADIUS source IP already exists", domain.ErrConflict)
	}
	return err
}

// DeleteSwitch removes a switch and its sessions.
func (r *PilotRepo) DeleteSwitch(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM switches WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

// TouchSwitchRadius records that the switch sent a RADIUS request.
func (r *PilotRepo) TouchSwitchRadius(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE switches SET last_radius_at = $2 WHERE id = $1`, id, at)
	return err
}

// RecordProbe stores a management reachability probe result.
func (r *PilotRepo) RecordProbe(ctx context.Context, id uuid.UUID, at time.Time, ok bool, detail string) error {
	_, err := r.pool.Exec(ctx, `UPDATE switches SET last_probe_at=$2, last_probe_ok=$3, last_probe_detail=$4 WHERE id=$1`, id, at, ok, detail)
	return err
}

// RecordVerification stores a completed verification.
func (r *PilotRepo) RecordVerification(ctx context.Context, id uuid.UUID, at time.Time, sessionID uuid.UUID, detail string) error {
	_, err := r.pool.Exec(ctx, `UPDATE switches SET verified_at=$2, verified_session_id=$3, verified_detail=$4 WHERE id=$1`, id, at, sessionID, detail)
	return err
}

// SegmentMappings satisfies policyver.SegmentSource.
func (r *PilotRepo) SegmentMappings(ctx context.Context) (map[string]map[string]int, error) {
	return segmentMappings(ctx, r.pool)
}

// segmentMappings reads switch name -> segment -> VLAN through q (a pool or
// a transaction).
func segmentMappings(ctx context.Context, q querier) (map[string]map[string]int, error) {
	// A role-enforced switch maps segments to local roles, not VLANs; it reports
	// each role segment with VLAN 0 so policy validation sees it as mapped.
	rows, err := q.Query(ctx, `SELECT name, CASE WHEN enforcement = 'role'
		THEN COALESCE((SELECT jsonb_object_agg(k, 0) FROM jsonb_object_keys(segment_roles) AS k), '{}'::jsonb)
		ELSE segment_vlans END FROM switches`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var (
			name string
			b    []byte
		)
		if err := rows.Scan(&name, &b); err != nil {
			return nil, err
		}
		m := map[string]int{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		out[name] = m
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- bindings

const bindingColumns = `id, endpoint_id, issuer, serial, subject_cn, revision, note, created_by, created_at, updated_at`

func scanBinding(row rowScanner) (*domain.IdentityBinding, error) {
	var b domain.IdentityBinding
	err := row.Scan(&b.ID, &b.EndpointID, &b.Issuer, &b.Serial, &b.SubjectCN, &b.Revision, &b.Note, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return &b, err
}

// BindingByCert looks up the binding for a certificate.
func (r *PilotRepo) BindingByCert(ctx context.Context, issuer, serial string) (*domain.IdentityBinding, error) {
	return scanBinding(r.pool.QueryRow(ctx, `SELECT `+bindingColumns+` FROM identity_bindings WHERE issuer=$1 AND serial=$2`, issuer, serial))
}

// GetBinding returns a binding by ID.
func (r *PilotRepo) GetBinding(ctx context.Context, id uuid.UUID) (*domain.IdentityBinding, error) {
	return scanBinding(r.pool.QueryRow(ctx, `SELECT `+bindingColumns+` FROM identity_bindings WHERE id=$1`, id))
}

// ListBindings lists bindings, optionally for one endpoint.
func (r *PilotRepo) ListBindings(ctx context.Context, endpointID *uuid.UUID) ([]domain.IdentityBinding, error) {
	q := `SELECT ` + bindingColumns + ` FROM identity_bindings`
	args := []any{}
	if endpointID != nil {
		q += ` WHERE endpoint_id = $1`
		args = append(args, *endpointID)
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.IdentityBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// CreateBinding inserts a binding.
func (r *PilotRepo) CreateBinding(ctx context.Context, b *domain.IdentityBinding) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO identity_bindings (`+bindingColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		b.ID, b.EndpointID, b.Issuer, b.Serial, b.SubjectCN, b.Revision, b.Note, b.CreatedBy, b.CreatedAt, b.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: this certificate (issuer and serial) is already bound", domain.ErrConflict)
	}
	if err != nil && strings.Contains(err.Error(), "foreign key") {
		return fmt.Errorf("%w: endpoint does not exist", domain.ErrInvalidInput)
	}
	return err
}

// DeleteBinding removes a binding.
func (r *PilotRepo) DeleteBinding(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM identity_bindings WHERE id=$1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

// GetEndpoint returns an endpoint by ID.
func (r *PilotRepo) GetEndpoint(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error) {
	return NewEndpointRepo(r.pool).GetByID(ctx, id)
}

// ---------------------------------------------------------------- sessions

const sessionColumns = `s.id, s.switch_id, sw.name, s.nas_port_id, s.mac, s.calling_station_id, s.nas_ip_address, s.user_name,
	s.acct_session_id, s.cert_issuer, s.cert_serial, s.cert_cn, s.endpoint_id, s.binding_id, s.state, s.override, s.override_by,
	s.override_at, s.last_decision_id, s.requested_action, s.requested_segment, s.requested_vlan, s.started_at, s.last_auth_at,
	s.ended_at, s.observed_status, s.observed_vlan, s.observed_at, s.observed_source, s.observed_detail, s.requested_role`

const sessionFrom = ` FROM radius_sessions s JOIN switches sw ON sw.id = s.switch_id`

func scanSession(row rowScanner) (*domain.Session, error) {
	var s domain.Session
	err := row.Scan(&s.ID, &s.SwitchID, &s.SwitchName, &s.NASPortID, &s.MAC, &s.CallingStationID, &s.NASIPAddress, &s.UserName,
		&s.AcctSessionID, &s.CertIssuer, &s.CertSerial, &s.CertCN, &s.EndpointID, &s.BindingID, &s.State, &s.Override, &s.OverrideBy,
		&s.OverrideAt, &s.LastDecisionID, &s.RequestedAction, &s.RequestedSegment, &s.RequestedVLAN, &s.StartedAt, &s.LastAuthAt,
		&s.EndedAt, &s.ObservedStatus, &s.ObservedVLAN, &s.ObservedAt, &s.ObservedSource, &s.ObservedDetail, &s.RequestedRole)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return &s, err
}

// CurrentSession returns the non-ended session for switch+port+MAC.
func (r *PilotRepo) CurrentSession(ctx context.Context, switchID uuid.UUID, port, mac string) (*domain.Session, error) {
	return scanSession(r.pool.QueryRow(ctx, `SELECT `+sessionColumns+sessionFrom+
		` WHERE s.switch_id=$1 AND s.nas_port_id=$2 AND s.mac=$3 AND s.state <> 'ended'`, switchID, port, mac))
}

// PreviousSession returns the most recent session of the same identity on
// the same port: switch + port + MAC + certificate issuer + serial (any
// state). An operator override follows this identity across session
// replacement (accounting Stop, reconnect) until an operator restores it.
// exclude is the session that is acquiring the identity (never its own
// predecessor).
func (r *PilotRepo) PreviousSession(ctx context.Context, exclude, switchID uuid.UUID, port, mac, issuer, serial string) (*domain.Session, error) {
	return scanSession(r.pool.QueryRow(ctx, `SELECT `+sessionColumns+sessionFrom+
		` WHERE s.id <> $1 AND s.switch_id=$2 AND s.nas_port_id=$3 AND s.mac=$4 AND s.cert_issuer=$5 AND s.cert_serial=$6
		ORDER BY s.started_at DESC LIMIT 1`, exclude, switchID, port, mac, issuer, serial))
}

// GetSession returns a session by ID.
func (r *PilotRepo) GetSession(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	return getSession(ctx, r.pool, id)
}

func getSession(ctx context.Context, q querier, id uuid.UUID) (*domain.Session, error) {
	return scanSession(q.QueryRow(ctx, `SELECT `+sessionColumns+sessionFrom+` WHERE s.id=$1`, id))
}

// CreateSession inserts a session.
func (r *PilotRepo) CreateSession(ctx context.Context, s *domain.Session) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO radius_sessions (id, switch_id, nas_port_id, mac, calling_station_id, nas_ip_address, user_name, acct_session_id,
			cert_issuer, cert_serial, cert_cn, state, started_at, last_auth_at, override, override_by, override_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		s.ID, s.SwitchID, s.NASPortID, s.MAC, s.CallingStationID, s.NASIPAddress, s.UserName, s.AcctSessionID,
		s.CertIssuer, s.CertSerial, s.CertCN, s.State, s.StartedAt, s.LastAuthAt, s.Override, s.OverrideBy, s.OverrideAt)
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}
	return err
}

// Session columns are owned by one writer each, so concurrent writers that
// each read the row earlier cannot undo one another:
//   - UpdateSession (RADIUS authorize/reject/accounting): identity, state,
//     requested authorization, timestamps.
//   - SetSessionOverride (operator CoA actions, and inheritance by an
//     identity-less session acquiring an identity): override_*.
//   - CreateSession writes every column once, including an inherited
//     override_*, so a new session is never visible without it.
//   - UpdateSessionObservation (eAPI poller): observed_*.

// UpdateSession writes the fields owned by the RADIUS path. It never writes
// the operator override or switch observations.
func (r *PilotRepo) UpdateSession(ctx context.Context, s *domain.Session) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE radius_sessions SET calling_station_id=$2, nas_ip_address=$3, user_name=$4, acct_session_id=$5, cert_issuer=$6,
			cert_serial=$7, cert_cn=$8, endpoint_id=$9, binding_id=$10, state=$11,
			last_decision_id=$12, requested_action=$13, requested_segment=$14, requested_vlan=$15, last_auth_at=$16, ended_at=$17,
			requested_role=$18
		WHERE id=$1`,
		s.ID, s.CallingStationID, s.NASIPAddress, s.UserName, s.AcctSessionID, s.CertIssuer, s.CertSerial, s.CertCN,
		s.EndpointID, s.BindingID, s.State, s.LastDecisionID, s.RequestedAction,
		s.RequestedSegment, s.RequestedVLAN, s.LastAuthAt, s.EndedAt, s.RequestedRole)
	return err
}

// SetSessionOverride writes only the operator override.
func (r *PilotRepo) SetSessionOverride(ctx context.Context, id uuid.UUID, override, by string, at *time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE radius_sessions SET override=$2, override_by=$3, override_at=$4 WHERE id=$1`, id, override, by, at)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

// UpdateSessionObservation writes only the switch-observed state.
func (r *PilotRepo) UpdateSessionObservation(ctx context.Context, id uuid.UUID, status, vlan string, at time.Time, source, detail string) error {
	_, err := r.pool.Exec(ctx, `UPDATE radius_sessions SET observed_status=$2, observed_vlan=$3, observed_at=$4, observed_source=$5,
		observed_detail=$6 WHERE id=$1`, id, status, vlan, at, source, detail)
	return err
}

// SessionFilter narrows session listings.
type SessionFilter struct {
	State    string
	SwitchID *uuid.UUID
	Search   string
	Limit    int
}

// ListSessions lists sessions, newest authentication first.
func (r *PilotRepo) ListSessions(ctx context.Context, f SessionFilter) ([]domain.Session, error) {
	return listSessions(ctx, r.pool, f)
}

func listSessions(ctx context.Context, q querier, f SessionFilter) ([]domain.Session, error) {
	where, args := []string{"TRUE"}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.State != "" {
		add("s.state = $%d", f.State)
	}
	if f.SwitchID != nil {
		add("s.switch_id = $%d", *f.SwitchID)
	}
	if f.Search != "" {
		add("(s.mac ILIKE '%%'||$%[1]d||'%%' OR s.cert_cn ILIKE '%%'||$%[1]d||'%%' OR s.nas_port_id ILIKE '%%'||$%[1]d||'%%')", f.Search)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := q.Query(ctx, `SELECT `+sessionColumns+sessionFrom+` WHERE `+strings.Join(where, " AND ")+
		fmt.Sprintf(` ORDER BY s.last_auth_at DESC LIMIT %d`, f.Limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ActiveSessionsOnSwitch lists ALL active sessions of one switch, for the
// observation poller and Verify. Unlike ListSessions (an API listing with a
// page cap), it is unbounded: a capped list would leave the oldest active
// sessions unobserved forever once a switch exceeded the cap.
func (r *PilotRepo) ActiveSessionsOnSwitch(ctx context.Context, switchID uuid.UUID) ([]domain.Session, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+sessionColumns+sessionFrom+
		` WHERE s.switch_id = $1 AND s.state = 'active' ORDER BY s.last_auth_at DESC`, switchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- decisions

const decisionColumns = `id, occurred_at, session_id, switch_id, subject, inputs, policy_version, policy_version_id, matched_rule,
	matched_rule_id, trace, insufficient, action, segment, vlan, result, reason, role`

// InsertDecision persists a decision record.
func (r *PilotRepo) InsertDecision(ctx context.Context, d *domain.DecisionRecord) error {
	inputs, _ := json.Marshal(d.Inputs)
	trace, _ := json.Marshal(d.Trace)
	insuff, _ := json.Marshal(d.Insufficient)
	var pv *int
	if d.PolicyVersion > 0 {
		pv = &d.PolicyVersion
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO auth_decisions (`+decisionColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		d.ID, d.OccurredAt, d.SessionID, d.SwitchID, d.Subject, inputs, pv, d.PolicyVersionID, d.MatchedRule, d.MatchedRuleID,
		trace, insuff, string(d.Action), d.Segment, d.VLAN, d.Result, d.Reason, d.Role)
	return err
}

func scanDecision(row rowScanner) (*domain.DecisionRecord, error) {
	var (
		d                     domain.DecisionRecord
		inputs, trace, insuff []byte
		pv                    *int
	)
	err := row.Scan(&d.ID, &d.OccurredAt, &d.SessionID, &d.SwitchID, &d.Subject, &inputs, &pv, &d.PolicyVersionID, &d.MatchedRule,
		&d.MatchedRuleID, &trace, &insuff, &d.Action, &d.Segment, &d.VLAN, &d.Result, &d.Reason, &d.Role)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if pv != nil {
		d.PolicyVersion = *pv
	}
	_ = json.Unmarshal(inputs, &d.Inputs)
	_ = json.Unmarshal(trace, &d.Trace)
	_ = json.Unmarshal(insuff, &d.Insufficient)
	return &d, nil
}

// GetDecision returns a decision by ID.
func (r *PilotRepo) GetDecision(ctx context.Context, id uuid.UUID) (*domain.DecisionRecord, error) {
	return scanDecision(r.pool.QueryRow(ctx, `SELECT `+decisionColumns+` FROM auth_decisions WHERE id=$1`, id))
}

// SessionDecisions lists a session's decisions, oldest first.
func (r *PilotRepo) SessionDecisions(ctx context.Context, sessionID uuid.UUID) ([]domain.DecisionRecord, error) {
	return sessionDecisions(ctx, r.pool, sessionID)
}

func sessionDecisions(ctx context.Context, q querier, sessionID uuid.UUID) ([]domain.DecisionRecord, error) {
	rows, err := q.Query(ctx, `SELECT `+decisionColumns+` FROM auth_decisions WHERE session_id=$1 ORDER BY occurred_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DecisionRecord{}
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// RecentCases satisfies policyver.CaseSource: the newest decision per session
// since `since`, with its recorded inputs.
func (r *PilotRepo) RecentCases(ctx context.Context, since time.Time, limit int) ([]policyver.RecordedCase, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (d.session_id) d.id, d.session_id, d.subject, d.occurred_at, d.inputs, d.action, d.segment,
			COALESCE(d.policy_version, 0), COALESCE(s.override, '')
		FROM auth_decisions d LEFT JOIN radius_sessions s ON s.id = d.session_id
		WHERE d.occurred_at >= $1 AND d.session_id IS NOT NULL
		ORDER BY d.session_id, d.occurred_at DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []policyver.RecordedCase{}
	for rows.Next() {
		var (
			c      policyver.RecordedCase
			inputs []byte
		)
		if err := rows.Scan(&c.DecisionID, &c.SessionID, &c.Subject, &c.OccurredAt, &inputs, &c.RecordedAction, &c.RecordedSegment, &c.RecordedVersion, &c.Override); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(inputs, &c.Inputs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- events

const eventColumns = `id, occurred_at, session_id, switch_id, client_ip, nas_port_id, mac, stage, result, reason, source, decision_id, coa_id, detail`

// InsertEvent appends a timeline event. With an ImportKey it is idempotent and
// reports whether a row was inserted.
func (r *PilotRepo) InsertEvent(ctx context.Context, e *domain.AuthEvent) (bool, error) {
	detail, _ := json.Marshal(orEmptyMap(e.Detail))
	var key *string
	if e.ImportKey != "" {
		key = &e.ImportKey
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO auth_events (`+eventColumns+`, import_key) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT DO NOTHING`,
		e.ID, e.OccurredAt, e.SessionID, e.SwitchID, e.ClientIP, e.NASPortID, e.MAC, e.Stage, e.Result, e.Reason, e.Source,
		e.DecisionID, e.CoAID, detail, key)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func scanEvents(rows pgx.Rows) ([]domain.AuthEvent, error) {
	defer rows.Close()
	out := []domain.AuthEvent{}
	for rows.Next() {
		var (
			e      domain.AuthEvent
			detail []byte
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.SessionID, &e.SwitchID, &e.ClientIP, &e.NASPortID, &e.MAC, &e.Stage, &e.Result,
			&e.Reason, &e.Source, &e.DecisionID, &e.CoAID, &detail); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detail, &e.Detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SessionEvents lists a session's events, oldest first. Events recorded
// before the session existed (e.g. outage rejects for the same port and MAC)
// are included by matching switch, port and MAC.
// SessionEventLimit bounds a session timeline. The NEWEST events are kept
// (then shown oldest first), so recent failures never fall off the view.
const SessionEventLimit = 1000

func (r *PilotRepo) SessionEvents(ctx context.Context, s *domain.Session) ([]domain.AuthEvent, error) {
	return sessionEvents(ctx, r.pool, s)
}

func sessionEvents(ctx context.Context, q querier, s *domain.Session) ([]domain.AuthEvent, error) {
	rows, err := q.Query(ctx, `SELECT `+eventColumns+` FROM (
		SELECT * FROM auth_events
		WHERE session_id=$1 OR (session_id IS NULL AND switch_id=$2 AND nas_port_id=$3 AND mac=$4)
		ORDER BY occurred_at DESC, id DESC LIMIT $5) newest
		ORDER BY occurred_at, id`, s.ID, s.SwitchID, s.NASPortID, s.MAC, SessionEventLimit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// EventFilter narrows event listings.
type EventFilter struct {
	Stage  string
	Result string
	MAC    string
	Limit  int
}

// ListEvents lists recent events, newest first.
func (r *PilotRepo) ListEvents(ctx context.Context, f EventFilter) ([]domain.AuthEvent, error) {
	where, args := []string{"TRUE"}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Stage != "" {
		add("stage = $%d", f.Stage)
	}
	if f.Result != "" {
		add("result = $%d", f.Result)
	}
	if f.MAC != "" {
		add("mac = $%d", f.MAC)
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 200
	}
	rows, err := r.pool.Query(ctx, `SELECT `+eventColumns+` FROM auth_events WHERE `+strings.Join(where, " AND ")+
		fmt.Sprintf(` ORDER BY occurred_at DESC LIMIT %d`, f.Limit), args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// LastEvent returns the newest event of a stage (nil if none).
func (r *PilotRepo) LastEvent(ctx context.Context, stage string) (*domain.AuthEvent, error) {
	evs, err := r.ListEvents(ctx, EventFilter{Stage: stage, Limit: 1})
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	return &evs[0], nil
}

// ---------------------------------------------------------------- CoA

const coaColumns = `id, session_id, switch_id, kind, target, requested_segment, requested_vlan, state, attempts, radius_id,
	error_cause, detail, created_by, created_at, sent_at, responded_at, observed_at, observed_decision_id, updated_at, requested_role`

func scanCoA(row rowScanner) (*domain.CoARequest, error) {
	var (
		c      domain.CoARequest
		target []byte
	)
	err := row.Scan(&c.ID, &c.SessionID, &c.SwitchID, &c.Kind, &target, &c.RequestedSegment, &c.RequestedVLAN, &c.State, &c.Attempts,
		&c.RadiusID, &c.ErrorCause, &c.Detail, &c.CreatedBy, &c.CreatedAt, &c.SentAt, &c.RespondedAt, &c.ObservedAt,
		&c.ObservedDecisionID, &c.UpdatedAt, &c.RequestedRole)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, json.Unmarshal(target, &c.Target)
}

// InsertCoA inserts a CoA request. Only one non-final CoA per session is
// allowed; a second concurrent request returns ErrConflict.
func (r *PilotRepo) InsertCoA(ctx context.Context, c *domain.CoARequest) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialise per session so two operators cannot both start a CoA.
	if _, err := tx.Exec(ctx, `SELECT id FROM radius_sessions WHERE id=$1 FOR UPDATE`, c.SessionID); err != nil {
		return err
	}
	var inFlight int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM coa_requests WHERE session_id=$1 AND state IN ('requested','sent')`, c.SessionID).Scan(&inFlight); err != nil {
		return err
	}
	if inFlight > 0 {
		return fmt.Errorf("%w: a CoA for this session is already in progress", domain.ErrConflict)
	}
	target, _ := json.Marshal(c.Target)
	if _, err := tx.Exec(ctx, `INSERT INTO coa_requests (`+coaColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		c.ID, c.SessionID, c.SwitchID, c.Kind, target, c.RequestedSegment, c.RequestedVLAN, c.State, c.Attempts, c.RadiusID,
		c.ErrorCause, c.Detail, c.CreatedBy, c.CreatedAt, c.SentAt, c.RespondedAt, c.ObservedAt, c.ObservedDecisionID, c.UpdatedAt,
		c.RequestedRole); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateCoA writes mutable CoA fields.
func (r *PilotRepo) UpdateCoA(ctx context.Context, c *domain.CoARequest) error {
	_, err := r.pool.Exec(ctx, `UPDATE coa_requests SET state=$2, attempts=$3, radius_id=$4, error_cause=$5, detail=$6, sent_at=$7,
		responded_at=$8, observed_at=$9, observed_decision_id=$10, updated_at=$11 WHERE id=$1`,
		c.ID, c.State, c.Attempts, c.RadiusID, c.ErrorCause, c.Detail, c.SentAt, c.RespondedAt, c.ObservedAt, c.ObservedDecisionID, c.UpdatedAt)
	return err
}

// UpdateCoAFrom writes the CoA only if its current state is one of from. It
// reports whether the row was updated. Used where two writers race (the CoA
// exchange and the re-authentication that the CoA triggers).
func (r *PilotRepo) UpdateCoAFrom(ctx context.Context, c *domain.CoARequest, from []string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE coa_requests SET state=$2, attempts=$3, radius_id=$4, error_cause=$5, detail=$6, sent_at=$7,
		responded_at=$8, observed_at=$9, observed_decision_id=$10, updated_at=$11 WHERE id=$1 AND state = ANY($12)`,
		c.ID, c.State, c.Attempts, c.RadiusID, c.ErrorCause, c.Detail, c.SentAt, c.RespondedAt, c.ObservedAt, c.ObservedDecisionID, c.UpdatedAt, from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetCoA returns a CoA request.
func (r *PilotRepo) GetCoA(ctx context.Context, id uuid.UUID) (*domain.CoARequest, error) {
	return scanCoA(r.pool.QueryRow(ctx, `SELECT `+coaColumns+` FROM coa_requests WHERE id=$1`, id))
}

// SessionCoAs lists a session's CoA requests, oldest first.
func (r *PilotRepo) SessionCoAs(ctx context.Context, sessionID uuid.UUID) ([]domain.CoARequest, error) {
	return sessionCoAs(ctx, r.pool, sessionID)
}

func sessionCoAs(ctx context.Context, q querier, sessionID uuid.UUID) ([]domain.CoARequest, error) {
	// A CoA belongs to the session it targeted, and also to the session whose
	// re-authentication observed it (the switch may replace the session).
	rows, err := q.Query(ctx, `SELECT `+coaColumns+` FROM coa_requests WHERE session_id=$1
		OR observed_decision_id IN (SELECT id FROM auth_decisions WHERE session_id=$1) ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CoARequest{}
	for rows.Next() {
		c, err := scanCoA(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// PendingCoA returns the newest CoA of the session that was sent or ACKed
// and not yet observed (nil if none).
func (r *PilotRepo) PendingCoA(ctx context.Context, sessionID uuid.UUID) (*domain.CoARequest, error) {
	c, err := scanCoA(r.pool.QueryRow(ctx, `SELECT `+coaColumns+` FROM coa_requests WHERE session_id=$1 AND state IN ('sent','acked')
		ORDER BY created_at DESC LIMIT 1`, sessionID))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return c, err
}

// FailInterruptedCoAs marks CoAs left mid-exchange by a restart as failed.
func (r *PilotRepo) FailInterruptedCoAs(ctx context.Context, at time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE coa_requests SET state='failed', updated_at=$1,
		detail='nacd restarted before the exchange completed; the switch may or may not have applied it. Check the observed state and retry the action.'
		WHERE state IN ('requested','sent')`, at)
	return tag.RowsAffected(), err
}
