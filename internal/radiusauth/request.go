// Package radiusauth is the supported RADIUS integration: FreeRADIUS validates
// EAP-TLS and calls nacd (rlm_rest) after authentication; nacd resolves the
// authenticated identity to a device through an explicit binding, evaluates
// the active policy version over recorded evidence, maps the segment to the
// switch's VLAN and records the session, decision and timeline.
package radiusauth

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Request is a normalised RADIUS request as forwarded by FreeRADIUS.
type Request struct {
	ClientIP         string // packet source (authenticated by the shared secret)
	UserName         string // outer EAP identity: an untrusted claim
	CallingStationID string
	NASIPAddress     string
	NASPortID        string
	AcctSessionID    string
	AcctStatusType   string
	Class            string
	CertSerial       string
	CertIssuer       string
	CertCN           string
	CertSubject      string
	CertExpiration   string
	ModuleFailure    []string
	ReplyMessage     string
}

// dropped attributes are never stored: they carry EAP payloads or secrets.
var dropped = map[string]bool{
	"EAP-Message": true, "Message-Authenticator": true, "State": true,
	"User-Password": true, "CHAP-Password": true, "MS-CHAP-Challenge": true,
}

// ParseFreeRADIUS decodes the rlm_rest `body = json` format:
// {"Attr-Name": {"type": "string", "value": ["v1", ...]}, ...}.
func ParseFreeRADIUS(body []byte, clientIP string) (Request, map[string][]string, error) {
	var raw map[string]struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Request{}, nil, errors.New("body is not FreeRADIUS rest JSON")
	}
	attrs := map[string][]string{}
	for name, a := range raw {
		if dropped[name] {
			continue
		}
		var vals []any
		if err := json.Unmarshal(a.Value, &vals); err != nil {
			var one any
			if err := json.Unmarshal(a.Value, &one); err != nil {
				continue
			}
			vals = []any{one}
		}
		for _, v := range vals {
			switch t := v.(type) {
			case string:
				attrs[name] = append(attrs[name], t)
			case float64:
				attrs[name] = append(attrs[name], strconv.FormatFloat(t, 'f', -1, 64))
			}
		}
	}
	first := func(k string) string {
		if v := attrs[k]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	r := Request{
		ClientIP:         clientIP,
		UserName:         first("User-Name"),
		CallingStationID: first("Calling-Station-Id"),
		NASIPAddress:     first("NAS-IP-Address"),
		NASPortID:        first("NAS-Port-Id"),
		AcctSessionID:    first("Acct-Session-Id"),
		AcctStatusType:   first("Acct-Status-Type"),
		Class:            first("Class"),
		CertSerial:       first("TLS-Client-Cert-Serial"),
		CertIssuer:       first("TLS-Client-Cert-Issuer"),
		CertCN:           first("TLS-Client-Cert-Common-Name"),
		CertSubject:      first("TLS-Client-Cert-Subject"),
		CertExpiration:   first("TLS-Client-Cert-Expiration"),
		ModuleFailure:    attrs["Module-Failure-Message"],
		ReplyMessage:     first("Reply-Message"),
	}
	return r, attrs, nil
}

var hexOnly = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// NormalizeSerial returns upper-case hex without separators or leading zeros
// (the same normalisation as ASTRA's lab adapter).
func NormalizeSerial(s string) string {
	s = strings.NewReplacer(":", "", " ", "", "0x", "", "0X", "").Replace(strings.TrimSpace(s))
	if !hexOnly.MatchString(s) {
		return ""
	}
	s = strings.TrimLeft(strings.ToUpper(s), "0")
	if s == "" {
		return "0"
	}
	return s
}

// HyphenMAC renders aa:bb:.. as AA-BB-.. the way vEOS sends Calling-Station-Id.
func HyphenMAC(mac string) string {
	return strings.ToUpper(strings.ReplaceAll(mac, ":", "-"))
}
