package radius

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrTimeout means no valid response arrived after all attempts.
var ErrTimeout = errors.New("no response from RADIUS peer")

// Exchange sends an already-encoded request and waits for a response whose
// Identifier and Response Authenticator match. Retransmissions resend the
// IDENTICAL bytes (same Identifier and Request Authenticator), so the peer can
// recognise duplicates and a retry can never be mistaken for a new request
// (RFC 5176 §2.3, RFC 5080 §2.2.1). Responses that fail verification are
// ignored, not trusted.
func Exchange(ctx context.Context, addr string, req *Packet, raw []byte, secret []byte, timeout time.Duration, attempts int) (*Packet, int, error) {
	if attempts < 1 {
		attempts = 1
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, 0, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	buf := make([]byte, 4096)
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, attempt - 1, err
		}
		if _, err := conn.Write(raw); err != nil {
			return nil, attempt, fmt.Errorf("send to %s: %w", addr, err)
		}
		deadline := time.Now().Add(timeout)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = conn.SetReadDeadline(deadline)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					break // retransmit
				}
				// ICMP port unreachable surfaces here as "connection refused".
				return nil, attempt, fmt.Errorf("receive from %s: %w", addr, err)
			}
			resp, derr := Decode(buf[:n])
			if derr != nil || resp.Identifier != req.Identifier || !VerifyResponse(buf[:n], req, secret) {
				continue // not ours / forged / wrong secret: keep waiting
			}
			return resp, attempt, nil
		}
	}
	return nil, attempts, ErrTimeout
}

// RandomIdentifier returns a random packet identifier.
func RandomIdentifier() byte {
	var b [1]byte
	_, _ = rand.Read(b[:])
	return b[0]
}

// RandomAuthenticator returns 16 random octets.
func RandomAuthenticator() [16]byte {
	var a [16]byte
	_, _ = rand.Read(a[:])
	return a
}

// ErrorCauseText explains RFC 5176 Error-Cause values in operator language.
func ErrorCauseText(v uint32) string {
	switch v {
	case 0:
		return ""
	case 200:
		return "200 Success"
	case 201:
		return "201 Residual session context removed"
	case 401:
		return "401 Unsupported attribute"
	case 402:
		return "402 Missing attribute (the switch needs more identifiers to find the session, e.g. NAS-IP-Address and NAS-Port-Id)"
	case 403:
		return "403 NAS identification mismatch (NAS-IP-Address does not match the switch)"
	case 404:
		return "404 Invalid request"
	case 405:
		return "405 Unsupported service"
	case 406:
		return "406 Unsupported extension"
	case 407:
		return "407 Invalid attribute value"
	case 501:
		return "501 Administratively prohibited (dynamic authorization disabled or this server not allowed)"
	case 502:
		return "502 Request not routable"
	case 503:
		return "503 Session context not found (the session ended or the identifiers do not match)"
	case 504:
		return "504 Session context not removable"
	case 505:
		return "505 Other proxy processing error"
	case 506:
		return "506 Resources unavailable"
	case 507:
		return "507 Request initiated"
	case 508:
		return "508 Multiple session selection unsupported"
	default:
		return fmt.Sprintf("%d (unrecognised Error-Cause)", v)
	}
}
