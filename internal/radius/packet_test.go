package radius

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

var secret = []byte("testing123")

// coaLikeLab builds the CoA-Request that vEOS 4.29.2F ACKed in the lab
// (lab/coa-radius-timeline.txt, request ID 172).
func coaLikeLab() *Packet {
	p := &Packet{Code: CodeCoARequest, Identifier: 172}
	p.AddString(AttrUserName, "wired-endpoint-01")
	p.AddString(AttrCallingStationID, "00-50-00-00-02-00")
	p.AddIPv4(AttrNASIPAddress, [4]byte{10, 70, 99, 1})
	p.AddString(AttrNASPortID, "Ethernet1")
	p.AddTunnelInt(AttrTunnelType, TunnelTypeVLAN)
	p.AddTunnelInt(AttrTunnelMediumType, TunnelMediumIEEE802)
	p.AddTunnelString(AttrTunnelPrivateGroupID, "10")
	return p
}

func TestEncodeDecodeRoundTripAndRequestAuthenticator(t *testing.T) {
	p := coaLikeLab()
	raw, err := p.EncodeRequest(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyRequest(raw, secret) {
		t.Fatalf("request authenticator does not verify")
	}
	if VerifyRequest(raw, []byte("wrong")) {
		t.Fatalf("request authenticator verified with wrong secret")
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != CodeCoARequest || got.Identifier != 172 || got.String(AttrNASPortID) != "Ethernet1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if v, _ := got.Get(AttrNASIPAddress); net.IP(v).String() != "10.70.99.1" {
		t.Fatalf("NAS-IP-Address = %v", net.IP(v))
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	badAttrLen := append([]byte{43, 1, 0, 22}, make([]byte, 16)...)
	badAttrLen = append(badAttrLen, 1, 1) // attribute length 1 < 2
	lengthBeyondBuffer := append([]byte{43, 1, 0, 40}, make([]byte, 16)...)
	for _, b := range [][]byte{nil, make([]byte, 19), badAttrLen, lengthBeyondBuffer} {
		if _, err := Decode(b); err == nil {
			t.Fatalf("expected malformed for %v", b)
		}
	}
}

// fakeSwitch answers CoA requests; it drops the first `drop` packets to
// exercise retransmission, and records every packet it received.
type recorder struct {
	mu   sync.Mutex
	pkts [][]byte
}

func (r *recorder) add(b []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pkts = append(r.pkts, b)
	return len(r.pkts)
}

func (r *recorder) all() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.pkts...)
}

func fakeSwitch(t *testing.T, drop int, respond func(*Packet) *Packet) (string, *recorder) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	seen := &recorder{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			raw := append([]byte{}, buf[:n]...)
			if seen.add(raw) <= drop {
				continue
			}
			req, err := Decode(raw)
			if err != nil || !VerifyRequest(raw, secret) {
				continue
			}
			resp := respond(req)
			out, _ := resp.EncodeResponse(secret, req)
			_, _ = pc.WriteTo(out, addr)
		}
	}()
	return pc.LocalAddr().String(), seen
}

func TestExchange_RetransmitsIdenticalBytesAndVerifiesACK(t *testing.T) {
	addr, seen := fakeSwitch(t, 1, func(req *Packet) *Packet {
		r := &Packet{Code: CodeCoAACK}
		r.AddUint32(AttrErrorCause, 200)
		return r
	})
	req := coaLikeLab()
	raw, _ := req.EncodeRequest(secret, false)
	resp, attempts, err := Exchange(context.Background(), addr, req, raw, secret, 200*time.Millisecond, 3)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.Code != CodeCoAACK || resp.Uint32(AttrErrorCause) != 200 || attempts != 2 {
		t.Fatalf("resp=%v attempts=%d", resp.Code, attempts)
	}
	if got := seen.all(); len(got) != 2 || string(got[0]) != string(got[1]) {
		t.Fatalf("retransmission must resend identical bytes")
	}
}

func TestExchange_IgnoresResponseWithWrongSecretAndTimesOut(t *testing.T) {
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req, _ := Decode(buf[:n])
			r := &Packet{Code: CodeCoAACK}
			out, _ := r.EncodeResponse([]byte("forged"), req)
			_, _ = pc.WriteTo(out, addr)
		}
	}()
	req := coaLikeLab()
	raw, _ := req.EncodeRequest(secret, false)
	_, attempts, err := Exchange(context.Background(), pc.LocalAddr().String(), req, raw, secret, 100*time.Millisecond, 2)
	if !errors.Is(err, ErrTimeout) || attempts != 2 {
		t.Fatalf("forged ACK must be ignored: err=%v attempts=%d", err, attempts)
	}
}

func TestErrorCauseText(t *testing.T) {
	if ErrorCauseText(402)[:3] != "402" || ErrorCauseText(0) != "" {
		t.Fatalf("unexpected text")
	}
}
