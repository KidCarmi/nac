// Package radius is a minimal RFC 2865/5176 codec used by nacd to send
// dynamic-authorization (CoA) requests to switches and to query FreeRADIUS
// status. It implements only what the pilot needs: packet encode/decode,
// request/response authenticators and Message-Authenticator. It is stdlib-only
// by design (see ADR 0005: minimal dependencies).
package radius

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // RADIUS authenticators are defined over MD5 (RFC 2865/5176).
	"encoding/binary"
	"errors"
	"fmt"
)

// Code is a RADIUS packet code.
type Code byte

const (
	CodeAccessRequest      Code = 1
	CodeAccessAccept       Code = 2
	CodeAccessReject       Code = 3
	CodeAccountingRequest  Code = 4
	CodeAccountingResponse Code = 5
	CodeStatusServer       Code = 12
	CodeDisconnectRequest  Code = 40
	CodeDisconnectACK      Code = 41
	CodeDisconnectNAK      Code = 42
	CodeCoARequest         Code = 43
	CodeCoAACK             Code = 44
	CodeCoANAK             Code = 45
)

func (c Code) String() string {
	switch c {
	case CodeAccessRequest:
		return "Access-Request"
	case CodeAccessAccept:
		return "Access-Accept"
	case CodeAccessReject:
		return "Access-Reject"
	case CodeAccountingRequest:
		return "Accounting-Request"
	case CodeAccountingResponse:
		return "Accounting-Response"
	case CodeStatusServer:
		return "Status-Server"
	case CodeDisconnectRequest:
		return "Disconnect-Request"
	case CodeDisconnectACK:
		return "Disconnect-ACK"
	case CodeDisconnectNAK:
		return "Disconnect-NAK"
	case CodeCoARequest:
		return "CoA-Request"
	case CodeCoAACK:
		return "CoA-ACK"
	case CodeCoANAK:
		return "CoA-NAK"
	default:
		return fmt.Sprintf("Code(%d)", byte(c))
	}
}

// Attribute types used by the pilot.
const (
	AttrUserName             byte = 1
	AttrNASIPAddress         byte = 4
	AttrNASPort              byte = 5
	AttrReplyMessage         byte = 18
	AttrClass                byte = 25
	AttrCallingStationID     byte = 31
	AttrNASIdentifier        byte = 32
	AttrAcctSessionID        byte = 44
	AttrEventTimestamp       byte = 55
	AttrTunnelType           byte = 64
	AttrTunnelMediumType     byte = 65
	AttrEAPMessage           byte = 79
	AttrMessageAuthenticator byte = 80
	AttrTunnelPrivateGroupID byte = 81
	AttrNASPortID            byte = 87
	AttrErrorCause           byte = 101
)

// Tunnel attribute values (RFC 2868).
const (
	TunnelTypeVLAN         = 13
	TunnelMediumIEEE802    = 6
	ErrorCauseResidualGone = 201
)

// Attribute is a raw type/value pair. Tunnel attributes carry a leading tag
// octet in Value (tag 0 for untagged), as on the wire.
type Attribute struct {
	Type  byte
	Value []byte
}

// Packet is a decoded RADIUS packet.
type Packet struct {
	Code          Code
	Identifier    byte
	Authenticator [16]byte
	Attributes    []Attribute
}

// Get returns the first attribute of type t.
func (p *Packet) Get(t byte) ([]byte, bool) {
	for _, a := range p.Attributes {
		if a.Type == t {
			return a.Value, true
		}
	}
	return nil, false
}

// String returns the first attribute of type t as a string.
func (p *Packet) String(t byte) string {
	v, _ := p.Get(t)
	return string(v)
}

// Uint32 returns an integer attribute (0 if absent or malformed).
func (p *Packet) Uint32(t byte) uint32 {
	v, ok := p.Get(t)
	if !ok || len(v) != 4 {
		return 0
	}
	return binary.BigEndian.Uint32(v)
}

// AddString appends a text attribute.
func (p *Packet) AddString(t byte, s string) {
	p.Attributes = append(p.Attributes, Attribute{t, []byte(s)})
}

// AddUint32 appends an integer attribute.
func (p *Packet) AddUint32(t byte, v uint32) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	p.Attributes = append(p.Attributes, Attribute{t, b})
}

// AddIPv4 appends an IPv4 address attribute.
func (p *Packet) AddIPv4(t byte, ip [4]byte) {
	p.Attributes = append(p.Attributes, Attribute{t, ip[:]})
}

// AddTunnelInt appends a tagged 3-octet tunnel integer (Tunnel-Type etc.).
func (p *Packet) AddTunnelInt(t byte, v uint32) {
	p.Attributes = append(p.Attributes, Attribute{t, []byte{0, byte(v >> 16), byte(v >> 8), byte(v)}})
}

// AddTunnelString appends a tunnel string attribute. RFC 2868 makes the tag
// optional for Tunnel-Private-Group-Id; like FreeRADIUS and the switch capture
// in lab/coa-radius-timeline.txt, it is sent untagged.
func (p *Packet) AddTunnelString(t byte, s string) {
	p.Attributes = append(p.Attributes, Attribute{t, []byte(s)})
}

// ErrMalformed is returned for undecodable packets.
var ErrMalformed = errors.New("malformed RADIUS packet")

// Decode parses a packet without verifying authenticators.
func Decode(b []byte) (*Packet, error) {
	if len(b) < 20 {
		return nil, ErrMalformed
	}
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length < 20 || length > len(b) || length > 4096 {
		return nil, ErrMalformed
	}
	p := &Packet{Code: Code(b[0]), Identifier: b[1]}
	copy(p.Authenticator[:], b[4:20])
	for i := 20; i < length; {
		if i+2 > length {
			return nil, ErrMalformed
		}
		t, l := b[i], int(b[i+1])
		if l < 2 || i+l > length {
			return nil, ErrMalformed
		}
		v := make([]byte, l-2)
		copy(v, b[i+2:i+l])
		p.Attributes = append(p.Attributes, Attribute{t, v})
		i += l
	}
	return p, nil
}

func (p *Packet) encodeAttrs() ([]byte, error) {
	var out []byte
	for _, a := range p.Attributes {
		if len(a.Value) > 253 {
			return nil, fmt.Errorf("attribute %d too long", a.Type)
		}
		out = append(out, a.Type, byte(len(a.Value)+2))
		out = append(out, a.Value...)
	}
	return out, nil
}

func (p *Packet) raw(attrs []byte, auth [16]byte) []byte {
	b := make([]byte, 20, 20+len(attrs))
	b[0], b[1] = byte(p.Code), p.Identifier
	binary.BigEndian.PutUint16(b[2:4], uint16(20+len(attrs))) //nolint:gosec // bounded: <= 4096 enforced by attribute limits
	copy(b[4:20], auth[:])
	return append(b, attrs...)
}

// EncodeRequest encodes a CoA/Disconnect/Accounting request: the
// Request Authenticator is MD5(Code|ID|Length|16 zero octets|Attributes|Secret)
// (RFC 5176 §2.3, RFC 2866 §3). If withMessageAuthenticator is set, a
// Message-Authenticator (RFC 3579) is added and computed first.
func (p *Packet) EncodeRequest(secret []byte, withMessageAuthenticator bool) ([]byte, error) {
	if withMessageAuthenticator {
		p.setMessageAuthenticator(secret, [16]byte{})
	}
	attrs, err := p.encodeAttrs()
	if err != nil {
		return nil, err
	}
	b := p.raw(attrs, [16]byte{})
	sum := md5.Sum(append(append([]byte{}, b...), secret...)) //nolint:gosec // protocol-mandated
	copy(p.Authenticator[:], sum[:])
	copy(b[4:20], sum[:])
	return b, nil
}

// EncodeResponse encodes a response to req: the Response Authenticator is
// MD5(Code|ID|Length|RequestAuth|Attributes|Secret).
func (p *Packet) EncodeResponse(secret []byte, req *Packet) ([]byte, error) {
	p.Identifier = req.Identifier
	attrs, err := p.encodeAttrs()
	if err != nil {
		return nil, err
	}
	b := p.raw(attrs, req.Authenticator)
	sum := md5.Sum(append(append([]byte{}, b...), secret...)) //nolint:gosec // protocol-mandated
	copy(p.Authenticator[:], sum[:])
	copy(b[4:20], sum[:])
	return b, nil
}

// EncodeStatusServer encodes a Status-Server request (RFC 5997): random
// authenticator plus a mandatory Message-Authenticator.
func (p *Packet) EncodeStatusServer(secret []byte, auth [16]byte) ([]byte, error) {
	p.Code = CodeStatusServer
	p.Authenticator = auth
	p.setMessageAuthenticator(secret, auth)
	attrs, err := p.encodeAttrs()
	if err != nil {
		return nil, err
	}
	return p.raw(attrs, auth), nil
}

// setMessageAuthenticator computes HMAC-MD5 over the packet with the
// Message-Authenticator zeroed and the given authenticator field.
func (p *Packet) setMessageAuthenticator(secret []byte, auth [16]byte) {
	idx := -1
	for i, a := range p.Attributes {
		if a.Type == AttrMessageAuthenticator {
			idx = i
		}
	}
	if idx < 0 {
		p.Attributes = append(p.Attributes, Attribute{AttrMessageAuthenticator, make([]byte, 16)})
		idx = len(p.Attributes) - 1
	}
	p.Attributes[idx].Value = make([]byte, 16)
	attrs, _ := p.encodeAttrs()
	mac := hmac.New(md5.New, secret)
	mac.Write(p.raw(attrs, auth))
	p.Attributes[idx].Value = mac.Sum(nil)
}

// VerifyResponse checks a response's authenticator against the request we
// sent (whose Authenticator field holds the value we put on the wire).
func VerifyResponse(raw []byte, req *Packet, secret []byte) bool {
	if len(raw) < 20 {
		return false
	}
	length := int(binary.BigEndian.Uint16(raw[2:4]))
	if length > len(raw) || length < 20 {
		return false
	}
	b := append([]byte{}, raw[:length]...)
	got := append([]byte{}, b[4:20]...)
	copy(b[4:20], req.Authenticator[:])
	sum := md5.Sum(append(b, secret...)) //nolint:gosec // protocol-mandated
	return hmac.Equal(got, sum[:])
}

// VerifyRequest checks a CoA/Disconnect/Accounting request authenticator
// (used by the switch emulator in tests and by any future listener).
func VerifyRequest(raw []byte, secret []byte) bool {
	if len(raw) < 20 {
		return false
	}
	length := int(binary.BigEndian.Uint16(raw[2:4]))
	if length > len(raw) || length < 20 {
		return false
	}
	b := append([]byte{}, raw[:length]...)
	got := append([]byte{}, b[4:20]...)
	for i := 4; i < 20; i++ {
		b[i] = 0
	}
	sum := md5.Sum(append(b, secret...)) //nolint:gosec // protocol-mandated
	return hmac.Equal(got, sum[:])
}
