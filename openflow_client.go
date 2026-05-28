package ovnflow

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const dbOpenFlow = "OpenFlow"

type OpenFlowDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type OpenFlowConfig struct {
	Endpoint                      string
	Versions                      []OpenFlowVersion
	AutoConfigureBridgeController bool
	ControllerTarget              string
	ConnectTimeout                time.Duration
}

type OpenFlowClient struct {
	ovs    *OVSClient
	config OpenFlowConfig
	dialer OpenFlowDialer
	xid    atomic.Uint32
}

type openFlowNetDialer struct{}

func (openFlowNetDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func NewOpenFlowClient(config OpenFlowConfig) *OpenFlowClient {
	return &OpenFlowClient{config: config, dialer: openFlowNetDialer{}}
}

func NewOpenFlowClientWithDialer(config OpenFlowConfig, dialer OpenFlowDialer) *OpenFlowClient {
	if dialer == nil {
		dialer = openFlowNetDialer{}
	}
	return &OpenFlowClient{config: config, dialer: dialer}
}

func (c *OpenFlowClient) WithEndpoint(endpoint string) *OpenFlowClient {
	c.config.Endpoint = endpoint
	return c
}

func (c *OpenFlowClient) WithVersions(versions ...OpenFlowVersion) *OpenFlowClient {
	c.config.Versions = append([]OpenFlowVersion{}, versions...)
	return c
}

func (c *OpenFlowClient) WithDialer(dialer OpenFlowDialer) *OpenFlowClient {
	if dialer != nil {
		c.dialer = dialer
	}
	return c
}

func (c *OpenFlowClient) AutoConfigureBridgeController(target string) *OpenFlowClient {
	c.config.AutoConfigureBridgeController = true
	c.config.ControllerTarget = target
	return c
}

func (c *OpenFlowClient) Bridge(name string) *OpenFlowBridgeRef {
	return &OpenFlowBridgeRef{client: c, name: name}
}

func (c *OpenFlowClient) Dial(ctx context.Context) (*OpenFlowSession, error) {
	if c == nil {
		return nil, ErrBackendUnavailable
	}
	endpoint := c.config.Endpoint
	if endpoint == "" {
		endpoint = endpointFromControllerTarget(c.config.ControllerTarget)
	}
	if endpoint == "" {
		return nil, wrap(ErrorValidation, dbOpenFlow, "", "connect", "", "OpenFlow endpoint is required", nil)
	}
	versions, err := normalizeOpenFlowVersions(c.config.Versions)
	if err != nil {
		return nil, err
	}
	network, address, err := parseOpenFlowEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if c.dialer == nil {
		c.dialer = openFlowNetDialer{}
	}
	if c.config.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.ConnectTimeout)
		defer cancel()
	}
	conn, err := c.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, classifyContext(err, dbOpenFlow, "", "connect", endpoint)
	}
	session := &OpenFlowSession{conn: conn, versions: versions, version: highestOpenFlowVersion(versions), nextXID: &c.xid}
	if err := session.Handshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return session, nil
}

func (c *OpenFlowClient) ConfigureBridgeController(ctx context.Context, bridge string) error {
	if c == nil {
		return ErrBackendUnavailable
	}
	target := c.config.ControllerTarget
	if target == "" {
		target = controllerTargetFromEndpoint(c.config.Endpoint)
	}
	if target == "" {
		return wrap(ErrorValidation, dbOpenFlow, tableBridge, "configure", bridge, "controller target is required", nil)
	}
	if c.ovs == nil {
		return ErrBackendUnavailable
	}
	return c.ovs.Bridge(bridge).Ensure().WithControllerTarget(target).Execute(ctx)
}

type OpenFlowSession struct {
	conn     net.Conn
	versions []OpenFlowVersion
	version  OpenFlowVersion
	nextXID  *atomic.Uint32
}

func (s *OpenFlowSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *OpenFlowSession) Version() OpenFlowVersion {
	if s == nil {
		return 0
	}
	return s.version
}

func (s *OpenFlowSession) Handshake(ctx context.Context) error {
	if s == nil || s.conn == nil {
		return ErrBackendUnavailable
	}
	version := highestOpenFlowVersion(s.versions)
	xid := s.next()
	hello, err := OpenFlowHelloMessage(version, xid, s.versions...)
	if err != nil {
		return err
	}
	if err := s.write(ctx, hello); err != nil {
		return err
	}
	peer, err := s.read(ctx)
	if err != nil {
		return err
	}
	if peer.Type == openFlowTypeError {
		return s.protocolError(peer)
	}
	if peer.Type != openFlowTypeHello {
		return wrap(ErrorConflict, dbOpenFlow, "", "handshake", "", "expected OpenFlow hello", nil)
	}
	selected, err := negotiateOpenFlowVersion(s.versions, peer)
	if err != nil {
		return err
	}
	s.version = selected
	req, err := OpenFlowFeaturesRequest(selected, s.next())
	if err != nil {
		return err
	}
	if err := s.write(ctx, req); err != nil {
		return err
	}
	reply, err := s.read(ctx)
	if err != nil {
		return err
	}
	if reply.Type == openFlowTypeError {
		return s.protocolError(reply)
	}
	if _, err := ParseOpenFlowFeatures(reply); err != nil {
		return err
	}
	return nil
}

func (s *OpenFlowSession) AddFlow(ctx context.Context, flow OpenFlowFlow) error {
	return s.flowMod(ctx, openFlowFlowCommandAdd, flow)
}

func (s *OpenFlowSession) ModifyFlowStrict(ctx context.Context, flow OpenFlowFlow) error {
	return s.flowMod(ctx, openFlowFlowCommandModifyStrict, flow)
}

func (s *OpenFlowSession) DeleteFlow(ctx context.Context, flow OpenFlowFlow) error {
	if flow.CookieMask == 0 {
		flow.CookieMask = ^uint64(0)
	}
	return s.flowMod(ctx, openFlowFlowCommandDeleteStrict, flow)
}

func (s *OpenFlowSession) DumpFlows(ctx context.Context, request OpenFlowFlowStatsRequest) ([]OpenFlowMessage, error) {
	if s == nil || s.conn == nil {
		return nil, ErrBackendUnavailable
	}
	msg, err := MarshalOpenFlowFlowStatsRequest(s.version, s.next(), request)
	if err != nil {
		return nil, err
	}
	if err := s.write(ctx, msg); err != nil {
		return nil, err
	}
	var out []OpenFlowMessage
	for {
		reply, err := s.read(ctx)
		if err != nil {
			return nil, err
		}
		if reply.Type == openFlowTypeError {
			return nil, s.protocolError(reply)
		}
		if reply.Type != openFlowTypeMultipartReply {
			return nil, wrap(ErrorConflict, dbOpenFlow, "", "dump", "flows", "expected multipart reply", nil)
		}
		_, flags, _, err := ParseOpenFlowMultipartReply(reply)
		if err != nil {
			return nil, err
		}
		out = append(out, reply)
		if flags&1 == 0 {
			return out, nil
		}
	}
}

func (s *OpenFlowSession) flowMod(ctx context.Context, command uint8, flow OpenFlowFlow) error {
	if s == nil || s.conn == nil {
		return ErrBackendUnavailable
	}
	msg, err := MarshalOpenFlowFlowMod(s.version, s.next(), command, flow)
	if err != nil {
		return err
	}
	if err := s.write(ctx, msg); err != nil {
		return err
	}
	return nil
}

func (s *OpenFlowSession) write(ctx context.Context, msg OpenFlowMessage) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(deadline)
	}
	if err := WriteOpenFlowMessage(s.conn, msg); err != nil {
		return classifyContext(err, dbOpenFlow, "", "write", "")
	}
	return nil
}

func (s *OpenFlowSession) read(ctx context.Context) (OpenFlowMessage, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetReadDeadline(deadline)
	}
	msg, err := ReadOpenFlowMessage(s.conn)
	if err != nil {
		return OpenFlowMessage{}, classifyContext(err, dbOpenFlow, "", "read", "")
	}
	return msg, nil
}

func (s *OpenFlowSession) protocolError(msg OpenFlowMessage) error {
	protocolErr, err := ParseOpenFlowProtocolError(msg)
	if err != nil {
		return err
	}
	return wrap(classifyOpenFlowProtocolError(protocolErr), dbOpenFlow, "", "protocol", "", protocolErr.Error(), protocolErr)
}

func (s *OpenFlowSession) next() uint32 {
	if s.nextXID == nil {
		return 1
	}
	return s.nextXID.Add(1)
}

func negotiateOpenFlowVersion(local []OpenFlowVersion, peer OpenFlowMessage) (OpenFlowVersion, error) {
	peerVersions := parseHelloVersions(peer)
	if len(peerVersions) == 0 {
		peerVersions = []OpenFlowVersion{peer.Version}
	}
	localSet := map[OpenFlowVersion]bool{}
	for _, version := range local {
		localSet[version] = true
	}
	var common []OpenFlowVersion
	for _, version := range peerVersions {
		if localSet[version] {
			common = append(common, version)
		}
	}
	if len(common) == 0 {
		return 0, wrap(ErrorUnsupported, dbOpenFlow, "", "handshake", "", "no common OpenFlow version", nil)
	}
	return highestOpenFlowVersion(common), nil
}

func parseHelloVersions(msg OpenFlowMessage) []OpenFlowVersion {
	var out []OpenFlowVersion
	for offset := 0; offset+4 <= len(msg.Body); {
		elementType := binaryBE16(msg.Body[offset : offset+2])
		length := int(binaryBE16(msg.Body[offset+2 : offset+4]))
		if length < 4 || offset+length > len(msg.Body) {
			return nil
		}
		if elementType == 1 && length >= 8 {
			for i := offset + 4; i+4 <= offset+length; i += 4 {
				bitmap := binaryBE32(msg.Body[i : i+4])
				for bit := 0; bit < 32; bit++ {
					if bitmap&(1<<uint(bit)) != 0 {
						v := OpenFlowVersion(bit)
						if v == OpenFlow13 || v == OpenFlow15 {
							out = append(out, v)
						}
					}
				}
			}
		}
		offset += length
	}
	return out
}

func classifyOpenFlowProtocolError(err *OpenFlowProtocolError) ErrorKind {
	if err == nil {
		return ErrorUnavailable
	}
	switch err.Type {
	case 1, 2, 3, 5:
		return ErrorValidation
	default:
		return ErrorUnsupported
	}
}

func parseOpenFlowEndpoint(endpoint string) (string, string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", wrap(ErrorValidation, dbOpenFlow, "", "parse", "", "endpoint is required", nil)
	}
	if strings.HasPrefix(endpoint, "tcp:") {
		parts := strings.Split(strings.TrimPrefix(endpoint, "tcp:"), ":")
		if len(parts) != 2 {
			return "", "", wrap(ErrorValidation, dbOpenFlow, "", "parse", endpoint, "invalid tcp endpoint", nil)
		}
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return "", "", wrap(ErrorValidation, dbOpenFlow, "", "parse", endpoint, "invalid tcp endpoint port", err)
		}
		return "tcp", net.JoinHostPort(parts[0], parts[1]), nil
	}
	if strings.HasPrefix(endpoint, "unix:") {
		return "unix", strings.TrimPrefix(endpoint, "unix:"), nil
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", "", wrap(ErrorValidation, dbOpenFlow, "", "parse", endpoint, "endpoint must be tcp:host:port, host:port, or unix:path", err)
	}
	return "tcp", net.JoinHostPort(host, port), nil
}

func endpointFromControllerTarget(target string) string {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "ptcp:") {
		parts := strings.Split(strings.TrimPrefix(target, "ptcp:"), ":")
		switch len(parts) {
		case 1:
			return "tcp:127.0.0.1:" + parts[0]
		case 2:
			return "tcp:" + parts[1] + ":" + parts[0]
		}
	}
	if strings.HasPrefix(target, "tcp:") || strings.HasPrefix(target, "unix:") {
		return target
	}
	return ""
}

func controllerTargetFromEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(endpoint, "tcp:") {
		parts := strings.Split(strings.TrimPrefix(endpoint, "tcp:"), ":")
		if len(parts) == 2 {
			return "ptcp:" + parts[1] + ":" + parts[0]
		}
	}
	if strings.HasPrefix(endpoint, "unix:") {
		return endpoint
	}
	return ""
}

func binaryBE16(data []byte) uint16 {
	return uint16(data[0])<<8 | uint16(data[1])
}

func binaryBE32(data []byte) uint32 {
	return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
}
