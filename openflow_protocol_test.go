package ovnflow

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestOpenFlowHelloWithVersionBitmap(t *testing.T) {
	msg, err := OpenFlowHelloMessage(OpenFlow15, 7, OpenFlow15, OpenFlow13)
	if err != nil {
		t.Fatalf("OpenFlowHelloMessage() = %v", err)
	}
	data, err := MarshalOpenFlowMessage(msg)
	if err != nil {
		t.Fatalf("MarshalOpenFlowMessage() = %v", err)
	}
	want := []byte{
		0x06, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x07,
		0x00, 0x01, 0x00, 0x08, 0x00, 0x00, 0x00, 0x50,
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("hello bytes = % x, want % x", data, want)
	}
	parsed, err := ParseOpenFlowMessage(data)
	if err != nil {
		t.Fatalf("ParseOpenFlowMessage() = %v", err)
	}
	if parsed.Version != OpenFlow15 || parsed.Type != openFlowTypeHello || parsed.XID != 7 {
		t.Fatalf("parsed hello = %#v", parsed)
	}
}

func TestOpenFlowFlowModVersionDifference(t *testing.T) {
	inPort := uint32(9)
	ethType := uint16(0x0800)
	tcp := uint16(80)
	flow := OpenFlowFlow{
		Cookie:     openFlowCookieForName("br0", "web"),
		CookieMask: openFlowCookieMask,
		TableID:    2,
		Priority:   200,
		Importance: 77,
		Match: OpenFlowMatch{
			InPort:  &inPort,
			EthType: &ethType,
			IPv4Dst: "10.0.0.10/32",
			TCPDst:  &tcp,
		},
		Actions: []OpenFlowAction{{Type: OpenFlowActionOutput, Port: 10}},
	}
	v13, err := MarshalOpenFlowFlowMod(OpenFlow13, 1, openFlowFlowCommandAdd, flow)
	if err != nil {
		t.Fatalf("MarshalOpenFlowFlowMod(1.3) = %v", err)
	}
	v15, err := MarshalOpenFlowFlowMod(OpenFlow15, 1, openFlowFlowCommandAdd, flow)
	if err != nil {
		t.Fatalf("MarshalOpenFlowFlowMod(1.5) = %v", err)
	}
	if v13.Body[38] != 0 || v13.Body[39] != 0 {
		t.Fatalf("OpenFlow 1.3 importance/pad bytes = % x, want zero", v13.Body[38:40])
	}
	if got := binary.BigEndian.Uint16(v15.Body[38:40]); got != 77 {
		t.Fatalf("OpenFlow 1.5 importance = %d, want 77", got)
	}
	if !bytes.Contains(v15.Body, []byte{0x80, 0x00, 0x18, 0x04, 10, 0, 0, 10}) {
		t.Fatalf("flow mod body missing masked IPv4 dst OXM: % x", v15.Body)
	}
}

func TestOpenFlowMatchRoundTrip(t *testing.T) {
	inPort := uint32(3)
	ethType := uint16(0x0800)
	proto := uint8(6)
	tcpDst := uint16(443)
	match := OpenFlowMatch{InPort: &inPort, EthType: &ethType, IPProto: &proto, IPv4Dst: "10.20.0.0/16", TCPDst: &tcpDst}
	raw, err := marshalOpenFlowMatch(match)
	if err != nil {
		t.Fatalf("marshalOpenFlowMatch() = %v", err)
	}
	parsed, err := ParseOpenFlowMatch(raw)
	if err != nil {
		t.Fatalf("ParseOpenFlowMatch() = %v", err)
	}
	want := OpenFlowMatch{InPort: &inPort, EthType: &ethType, IPProto: &proto, IPv4Dst: "10.20.0.0/16", TCPDst: &tcpDst}
	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("parsed match = %#v, want %#v", parsed, want)
	}
}

func TestOpenFlowClientHandshakeAndAddFlow(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	dialer := pipeOpenFlowDialer{conn: client}
	of := NewOpenFlowClientWithDialer(OpenFlowConfig{Endpoint: "tcp:127.0.0.1:6653", Versions: []OpenFlowVersion{OpenFlow15, OpenFlow13}}, dialer)

	serverErr := make(chan error, 1)
	received := make(chan OpenFlowMessage, 1)
	go func() {
		defer server.Close()
		hello, err := ReadOpenFlowMessage(server)
		if err != nil {
			serverErr <- err
			return
		}
		reply, _ := OpenFlowHelloMessage(OpenFlow15, hello.XID, OpenFlow15, OpenFlow13)
		if err := WriteOpenFlowMessage(server, reply); err != nil {
			serverErr <- err
			return
		}
		featuresReq, err := ReadOpenFlowMessage(server)
		if err != nil {
			serverErr <- err
			return
		}
		if featuresReq.Type != openFlowTypeFeaturesRequest {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		body := make([]byte, 24)
		binary.BigEndian.PutUint64(body[0:8], 99)
		body[12] = 254
		_ = WriteOpenFlowMessage(server, OpenFlowMessage{Version: OpenFlow15, Type: openFlowTypeFeaturesReply, XID: featuresReq.XID, Body: body})
		flowMod, err := ReadOpenFlowMessage(server)
		if err != nil {
			serverErr <- err
			return
		}
		received <- flowMod
		serverErr <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := of.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	defer session.Close()
	if session.Version() != OpenFlow15 {
		t.Fatalf("negotiated version = %d, want %d", session.Version(), OpenFlow15)
	}
	if err := session.AddFlow(ctx, OpenFlowFlow{Cookie: openFlowCookieForName("br0", "web"), CookieMask: openFlowCookieMask, Actions: []OpenFlowAction{{Type: OpenFlowActionOutput, Port: 1}}}); err != nil {
		t.Fatalf("AddFlow() = %v", err)
	}
	select {
	case msg := <-received:
		if msg.Type != openFlowTypeFlowMod || msg.Version != OpenFlow15 {
			t.Fatalf("flow mod = %#v", msg)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server = %v", err)
	}
}

func TestOpenFlowRuleBuilderDryRunAndCookieOwnership(t *testing.T) {
	builder := NewOpenFlowClient(OpenFlowConfig{Endpoint: "tcp:127.0.0.1:6653"}).
		Bridge("br0").
		EnsureFlow("web").
		InPort(1).
		IPv4Dst("10.0.0.10").
		TCPDst(80).
		Actions().Output(2)

	dryRun, err := builder.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun() = %v", err)
	}
	if len(dryRun.Plan.Operations) != 1 || dryRun.Plan.Operations[0].Resource != "OpenFlowRule" {
		t.Fatalf("plan = %#v", dryRun.Plan)
	}
	if builder.Flow().Cookie&openFlowCookieMask != openFlowCookieNamespace {
		t.Fatalf("cookie = %#x outside ovnflow namespace", builder.Flow().Cookie)
	}

	err = NewOpenFlowClient(OpenFlowConfig{Endpoint: "tcp:127.0.0.1:6653"}).
		Bridge("br0").
		EnsureFlow("bad").
		Cookie(1).
		Actions().Output(1).
		Validate()
	if !IsKind(err, ErrorValidation) {
		t.Fatalf("Validate foreign cookie = %v, want validation", err)
	}
}

type pipeOpenFlowDialer struct {
	conn net.Conn
}

func (d pipeOpenFlowDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return d.conn, nil
}
