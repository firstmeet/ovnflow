package ovnflow

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
)

type OpenFlowVersion uint8

const (
	OpenFlow13 OpenFlowVersion = 0x04
	OpenFlow15 OpenFlowVersion = 0x06
)

const (
	openFlowTypeHello            uint8  = 0
	openFlowTypeError            uint8  = 1
	openFlowTypeFeaturesRequest  uint8  = 5
	openFlowTypeFeaturesReply    uint8  = 6
	openFlowTypeFlowMod          uint8  = 14
	openFlowTypeMultipartRequest uint8  = 18
	openFlowTypeMultipartReply   uint8  = 19
	openFlowHeaderLen            uint16 = 8

	openFlowOXMClassBasic uint16 = 0x8000
	openFlowMatchOXM      uint16 = 1

	openFlowInstructionGotoTable     uint16 = 1
	openFlowInstructionWriteMetadata uint16 = 2
	openFlowInstructionApplyActions  uint16 = 4
	openFlowInstructionClearActions  uint16 = 5

	openFlowActionOutput   uint16 = 0
	openFlowActionSetField uint16 = 25

	openFlowMultipartFlow uint16 = 1

	openFlowFlowCommandAdd          uint8 = 0
	openFlowFlowCommandModifyStrict uint8 = 2
	openFlowFlowCommandDelete       uint8 = 3
	openFlowFlowCommandDeleteStrict uint8 = 4

	OpenFlowPortAny        uint32 = 0xffffffff
	OpenFlowGroupAny       uint32 = 0xffffffff
	OpenFlowControllerPort uint32 = 0xfffffffd
)

const (
	openFlowOFBInPort   uint8 = 0
	openFlowOFBMetadata uint8 = 2
	openFlowOFBEthDst   uint8 = 3
	openFlowOFBEthSrc   uint8 = 4
	openFlowOFBEthType  uint8 = 5
	openFlowOFBVLANVID  uint8 = 6
	openFlowOFBIPProto  uint8 = 10
	openFlowOFBIPv4Src  uint8 = 11
	openFlowOFBIPv4Dst  uint8 = 12
	openFlowOFBTCPSrc   uint8 = 13
	openFlowOFBTCPDst   uint8 = 14
	openFlowOFBUDPSrc   uint8 = 15
	openFlowOFBUDPDst   uint8 = 16
)

type OpenFlowMessage struct {
	Version OpenFlowVersion
	Type    uint8
	XID     uint32
	Body    []byte
}

type OpenFlowFeatures struct {
	Version      OpenFlowVersion
	XID          uint32
	DatapathID   uint64
	Buffers      uint32
	Tables       uint8
	AuxiliaryID  uint8
	Capabilities uint32
}

type OpenFlowProtocolError struct {
	Version OpenFlowVersion
	XID     uint32
	Type    uint16
	Code    uint16
	Data    []byte
}

func (e *OpenFlowProtocolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("openflow error type=%d code=%d xid=%d", e.Type, e.Code, e.XID)
}

func MarshalOpenFlowMessage(msg OpenFlowMessage) ([]byte, error) {
	if err := validateOpenFlowVersion(msg.Version); err != nil {
		return nil, err
	}
	length := int(openFlowHeaderLen) + len(msg.Body)
	if length > 0xffff {
		return nil, wrap(ErrorValidation, "", "", "marshal", "openflow", "message is too large", nil)
	}
	out := make([]byte, length)
	out[0] = byte(msg.Version)
	out[1] = msg.Type
	binary.BigEndian.PutUint16(out[2:4], uint16(length))
	binary.BigEndian.PutUint32(out[4:8], msg.XID)
	copy(out[8:], msg.Body)
	return out, nil
}

func ParseOpenFlowMessage(data []byte) (OpenFlowMessage, error) {
	if len(data) < int(openFlowHeaderLen) {
		return OpenFlowMessage{}, wrap(ErrorValidation, "", "", "parse", "openflow", "short OpenFlow header", nil)
	}
	version := OpenFlowVersion(data[0])
	if err := validateOpenFlowVersion(version); err != nil {
		return OpenFlowMessage{}, err
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if length < int(openFlowHeaderLen) {
		return OpenFlowMessage{}, wrap(ErrorValidation, "", "", "parse", "openflow", "invalid OpenFlow message length", nil)
	}
	if length > len(data) {
		return OpenFlowMessage{}, wrap(ErrorValidation, "", "", "parse", "openflow", "truncated OpenFlow message", nil)
	}
	body := append([]byte{}, data[8:length]...)
	return OpenFlowMessage{Version: version, Type: data[1], XID: binary.BigEndian.Uint32(data[4:8]), Body: body}, nil
}

func ReadOpenFlowMessage(r io.Reader) (OpenFlowMessage, error) {
	header := make([]byte, openFlowHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return OpenFlowMessage{}, err
	}
	length := int(binary.BigEndian.Uint16(header[2:4]))
	if length < int(openFlowHeaderLen) {
		return OpenFlowMessage{}, wrap(ErrorValidation, "", "", "read", "openflow", "invalid OpenFlow message length", nil)
	}
	data := make([]byte, length)
	copy(data, header)
	if _, err := io.ReadFull(r, data[8:]); err != nil {
		return OpenFlowMessage{}, err
	}
	return ParseOpenFlowMessage(data)
}

func WriteOpenFlowMessage(w io.Writer, msg OpenFlowMessage) error {
	data, err := MarshalOpenFlowMessage(msg)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func OpenFlowHelloMessage(version OpenFlowVersion, xid uint32, versions ...OpenFlowVersion) (OpenFlowMessage, error) {
	if err := validateOpenFlowVersion(version); err != nil {
		return OpenFlowMessage{}, err
	}
	if len(versions) == 0 {
		return OpenFlowMessage{Version: version, Type: openFlowTypeHello, XID: xid}, nil
	}
	bitmap := uint32(0)
	for _, v := range versions {
		if err := validateOpenFlowVersion(v); err != nil {
			return OpenFlowMessage{}, err
		}
		if v < 32 {
			bitmap |= 1 << uint(v)
		}
	}
	body := make([]byte, 8)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint16(body[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(body[4:8], bitmap)
	return OpenFlowMessage{Version: version, Type: openFlowTypeHello, XID: xid, Body: body}, nil
}

func OpenFlowFeaturesRequest(version OpenFlowVersion, xid uint32) (OpenFlowMessage, error) {
	if err := validateOpenFlowVersion(version); err != nil {
		return OpenFlowMessage{}, err
	}
	return OpenFlowMessage{Version: version, Type: openFlowTypeFeaturesRequest, XID: xid}, nil
}

func ParseOpenFlowFeatures(msg OpenFlowMessage) (OpenFlowFeatures, error) {
	if msg.Type != openFlowTypeFeaturesReply {
		return OpenFlowFeatures{}, wrap(ErrorValidation, "", "", "parse", "features", "message is not a features reply", nil)
	}
	if len(msg.Body) < 24 {
		return OpenFlowFeatures{}, wrap(ErrorValidation, "", "", "parse", "features", "short features reply", nil)
	}
	return OpenFlowFeatures{
		Version:      msg.Version,
		XID:          msg.XID,
		DatapathID:   binary.BigEndian.Uint64(msg.Body[0:8]),
		Buffers:      binary.BigEndian.Uint32(msg.Body[8:12]),
		Tables:       msg.Body[12],
		AuxiliaryID:  msg.Body[13],
		Capabilities: binary.BigEndian.Uint32(msg.Body[16:20]),
	}, nil
}

func ParseOpenFlowProtocolError(msg OpenFlowMessage) (*OpenFlowProtocolError, error) {
	if msg.Type != openFlowTypeError {
		return nil, wrap(ErrorValidation, "", "", "parse", "error", "message is not an error", nil)
	}
	if len(msg.Body) < 4 {
		return nil, wrap(ErrorValidation, "", "", "parse", "error", "short OpenFlow error", nil)
	}
	return &OpenFlowProtocolError{
		Version: msg.Version,
		XID:     msg.XID,
		Type:    binary.BigEndian.Uint16(msg.Body[0:2]),
		Code:    binary.BigEndian.Uint16(msg.Body[2:4]),
		Data:    append([]byte{}, msg.Body[4:]...),
	}, nil
}

type OpenFlowActionType string

const (
	OpenFlowActionOutput   OpenFlowActionType = "output"
	OpenFlowActionSetField OpenFlowActionType = "set_field"
)

type OpenFlowAction struct {
	Type  OpenFlowActionType
	Port  uint32
	Field string
	Value string
}

type OpenFlowInstruction struct {
	GotoTable     *uint8
	ClearActions  bool
	WriteMetadata *OpenFlowMetadata
	Actions       []OpenFlowAction
}

type OpenFlowMetadata struct {
	Value uint64
	Mask  uint64
}

type OpenFlowMatch struct {
	InPort   *uint32
	Metadata *OpenFlowMetadata
	EthSrc   string
	EthDst   string
	EthType  *uint16
	VLANVID  *uint16
	IPProto  *uint8
	IPv4Src  string
	IPv4Dst  string
	TCPSrc   *uint16
	TCPDst   *uint16
	UDPSrc   *uint16
	UDPDst   *uint16
}

type OpenFlowFlow struct {
	Name         string
	Bridge       string
	Cookie       uint64
	CookieMask   uint64
	TableID      uint8
	Priority     uint16
	IdleTimeout  uint16
	HardTimeout  uint16
	Importance   uint16
	Match        OpenFlowMatch
	Instructions []OpenFlowInstruction
	Actions      []OpenFlowAction
	Owner        OwnerRef
	Labels       Labels
}

type OpenFlowFlowStatsRequest struct {
	TableID    *uint8
	OutPort    uint32
	OutGroup   uint32
	Cookie     uint64
	CookieMask uint64
	Match      OpenFlowMatch
}

func MarshalOpenFlowFlowMod(version OpenFlowVersion, xid uint32, command uint8, flow OpenFlowFlow) (OpenFlowMessage, error) {
	if err := validateOpenFlowVersion(version); err != nil {
		return OpenFlowMessage{}, err
	}
	match, err := marshalOpenFlowMatch(flow.Match)
	if err != nil {
		return OpenFlowMessage{}, err
	}
	instructions, err := marshalOpenFlowInstructions(flow)
	if err != nil {
		return OpenFlowMessage{}, err
	}
	body := &bytes.Buffer{}
	writeBE(body, flow.Cookie)
	writeBE(body, flow.CookieMask)
	body.WriteByte(flow.TableID)
	body.WriteByte(command)
	writeBE(body, flow.IdleTimeout)
	writeBE(body, flow.HardTimeout)
	writeBE(body, flow.Priority)
	writeBE(body, uint32(0xffffffff))
	writeBE(body, OpenFlowPortAny)
	writeBE(body, OpenFlowGroupAny)
	writeBE(body, uint16(0))
	if version == OpenFlow15 {
		writeBE(body, flow.Importance)
	} else {
		writeBE(body, uint16(0))
	}
	body.Write(match)
	body.Write(instructions)
	return OpenFlowMessage{Version: version, Type: openFlowTypeFlowMod, XID: xid, Body: body.Bytes()}, nil
}

func MarshalOpenFlowFlowStatsRequest(version OpenFlowVersion, xid uint32, request OpenFlowFlowStatsRequest) (OpenFlowMessage, error) {
	if err := validateOpenFlowVersion(version); err != nil {
		return OpenFlowMessage{}, err
	}
	match, err := marshalOpenFlowMatch(request.Match)
	if err != nil {
		return OpenFlowMessage{}, err
	}
	tableID := uint8(0xff)
	if request.TableID != nil {
		tableID = *request.TableID
	}
	outPort := request.OutPort
	if outPort == 0 {
		outPort = OpenFlowPortAny
	}
	outGroup := request.OutGroup
	if outGroup == 0 {
		outGroup = OpenFlowGroupAny
	}
	flowBody := &bytes.Buffer{}
	flowBody.WriteByte(tableID)
	flowBody.Write([]byte{0, 0, 0})
	writeBE(flowBody, outPort)
	writeBE(flowBody, outGroup)
	writeBE(flowBody, request.Cookie)
	writeBE(flowBody, request.CookieMask)
	flowBody.Write(match)

	body := &bytes.Buffer{}
	writeBE(body, openFlowMultipartFlow)
	writeBE(body, uint16(0))
	writeBE(body, uint32(0))
	body.Write(flowBody.Bytes())
	return OpenFlowMessage{Version: version, Type: openFlowTypeMultipartRequest, XID: xid, Body: body.Bytes()}, nil
}

func ParseOpenFlowMultipartReply(msg OpenFlowMessage) (uint16, uint16, []byte, error) {
	if msg.Type != openFlowTypeMultipartReply {
		return 0, 0, nil, wrap(ErrorValidation, "", "", "parse", "multipart", "message is not a multipart reply", nil)
	}
	if len(msg.Body) < 8 {
		return 0, 0, nil, wrap(ErrorValidation, "", "", "parse", "multipart", "short multipart reply", nil)
	}
	return binary.BigEndian.Uint16(msg.Body[0:2]), binary.BigEndian.Uint16(msg.Body[2:4]), append([]byte{}, msg.Body[8:]...), nil
}

func marshalOpenFlowInstructions(flow OpenFlowFlow) ([]byte, error) {
	instructions := append([]OpenFlowInstruction{}, flow.Instructions...)
	if len(flow.Actions) > 0 {
		instructions = append(instructions, OpenFlowInstruction{Actions: append([]OpenFlowAction{}, flow.Actions...)})
	}
	out := &bytes.Buffer{}
	for _, instruction := range instructions {
		switch {
		case instruction.GotoTable != nil:
			writeBE(out, openFlowInstructionGotoTable)
			writeBE(out, uint16(8))
			out.WriteByte(*instruction.GotoTable)
			out.Write([]byte{0, 0, 0})
		case instruction.ClearActions:
			writeBE(out, openFlowInstructionClearActions)
			writeBE(out, uint16(8))
			out.Write([]byte{0, 0, 0, 0})
		case instruction.WriteMetadata != nil:
			writeBE(out, openFlowInstructionWriteMetadata)
			writeBE(out, uint16(24))
			out.Write([]byte{0, 0, 0, 0})
			writeBE(out, instruction.WriteMetadata.Value)
			writeBE(out, instruction.WriteMetadata.Mask)
		case len(instruction.Actions) > 0:
			actions, err := marshalOpenFlowActions(instruction.Actions)
			if err != nil {
				return nil, err
			}
			length := 8 + len(actions)
			writeBE(out, openFlowInstructionApplyActions)
			writeBE(out, uint16(length))
			out.Write([]byte{0, 0, 0, 0})
			out.Write(actions)
			padTo(out, 8)
		}
	}
	return out.Bytes(), nil
}

func marshalOpenFlowActions(actions []OpenFlowAction) ([]byte, error) {
	out := &bytes.Buffer{}
	for _, action := range actions {
		switch action.Type {
		case OpenFlowActionOutput:
			writeBE(out, openFlowActionOutput)
			writeBE(out, uint16(16))
			writeBE(out, action.Port)
			writeBE(out, uint16(0xffff))
			out.Write([]byte{0, 0, 0, 0, 0, 0})
		case OpenFlowActionSetField:
			field, err := marshalSetField(action.Field, action.Value)
			if err != nil {
				return nil, err
			}
			length := 4 + len(field)
			writeBE(out, openFlowActionSetField)
			writeBE(out, uint16(length))
			out.Write(field)
			padTo(out, 8)
		default:
			return nil, wrap(ErrorUnsupported, "", "", "marshal", "openflow-action", "unsupported OpenFlow action", nil)
		}
	}
	return out.Bytes(), nil
}

func marshalSetField(field, value string) ([]byte, error) {
	switch field {
	case "eth_src":
		mac, err := net.ParseMAC(value)
		if err != nil {
			return nil, wrap(ErrorValidation, "", "", "marshal", field, "invalid MAC address", err)
		}
		return marshalOXM(openFlowOFBEthSrc, false, mac, nil), nil
	case "eth_dst":
		mac, err := net.ParseMAC(value)
		if err != nil {
			return nil, wrap(ErrorValidation, "", "", "marshal", field, "invalid MAC address", err)
		}
		return marshalOXM(openFlowOFBEthDst, false, mac, nil), nil
	default:
		return nil, wrap(ErrorUnsupported, "", "", "marshal", field, "unsupported set_field target", nil)
	}
}

func marshalOpenFlowMatch(match OpenFlowMatch) ([]byte, error) {
	fields := &bytes.Buffer{}
	if match.InPort != nil {
		value := make([]byte, 4)
		binary.BigEndian.PutUint32(value, *match.InPort)
		fields.Write(marshalOXM(openFlowOFBInPort, false, value, nil))
	}
	if match.Metadata != nil {
		value := make([]byte, 8)
		mask := make([]byte, 8)
		binary.BigEndian.PutUint64(value, match.Metadata.Value)
		binary.BigEndian.PutUint64(mask, match.Metadata.Mask)
		fields.Write(marshalOXM(openFlowOFBMetadata, true, value, mask))
	}
	if match.EthDst != "" {
		mac, err := net.ParseMAC(match.EthDst)
		if err != nil {
			return nil, wrap(ErrorValidation, "", "", "marshal", "eth_dst", "invalid MAC address", err)
		}
		fields.Write(marshalOXM(openFlowOFBEthDst, false, mac, nil))
	}
	if match.EthSrc != "" {
		mac, err := net.ParseMAC(match.EthSrc)
		if err != nil {
			return nil, wrap(ErrorValidation, "", "", "marshal", "eth_src", "invalid MAC address", err)
		}
		fields.Write(marshalOXM(openFlowOFBEthSrc, false, mac, nil))
	}
	if match.EthType != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.EthType)
		fields.Write(marshalOXM(openFlowOFBEthType, false, value, nil))
	}
	if match.VLANVID != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.VLANVID)
		fields.Write(marshalOXM(openFlowOFBVLANVID, false, value, nil))
	}
	if match.IPProto != nil {
		fields.Write(marshalOXM(openFlowOFBIPProto, false, []byte{*match.IPProto}, nil))
	}
	if match.IPv4Src != "" {
		value, mask, masked, err := ipv4OXM(match.IPv4Src)
		if err != nil {
			return nil, err
		}
		fields.Write(marshalOXM(openFlowOFBIPv4Src, masked, value, mask))
	}
	if match.IPv4Dst != "" {
		value, mask, masked, err := ipv4OXM(match.IPv4Dst)
		if err != nil {
			return nil, err
		}
		fields.Write(marshalOXM(openFlowOFBIPv4Dst, masked, value, mask))
	}
	if match.TCPSrc != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.TCPSrc)
		fields.Write(marshalOXM(openFlowOFBTCPSrc, false, value, nil))
	}
	if match.TCPDst != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.TCPDst)
		fields.Write(marshalOXM(openFlowOFBTCPDst, false, value, nil))
	}
	if match.UDPSrc != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.UDPSrc)
		fields.Write(marshalOXM(openFlowOFBUDPSrc, false, value, nil))
	}
	if match.UDPDst != nil {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, *match.UDPDst)
		fields.Write(marshalOXM(openFlowOFBUDPDst, false, value, nil))
	}

	length := 4 + fields.Len()
	out := &bytes.Buffer{}
	writeBE(out, openFlowMatchOXM)
	writeBE(out, uint16(length))
	out.Write(fields.Bytes())
	padTo(out, 8)
	return out.Bytes(), nil
}

func ParseOpenFlowMatch(data []byte) (OpenFlowMatch, error) {
	if len(data) < 4 {
		return OpenFlowMatch{}, wrap(ErrorValidation, "", "", "parse", "match", "short OpenFlow match", nil)
	}
	if binary.BigEndian.Uint16(data[0:2]) != openFlowMatchOXM {
		return OpenFlowMatch{}, wrap(ErrorUnsupported, "", "", "parse", "match", "only OXM match is supported", nil)
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if length < 4 || length > len(data) {
		return OpenFlowMatch{}, wrap(ErrorValidation, "", "", "parse", "match", "invalid OpenFlow match length", nil)
	}
	var match OpenFlowMatch
	for offset := 4; offset < length; {
		if offset+4 > length {
			return OpenFlowMatch{}, wrap(ErrorValidation, "", "", "parse", "match", "truncated OXM header", nil)
		}
		class := binary.BigEndian.Uint16(data[offset : offset+2])
		fieldMask := data[offset+2]
		size := int(data[offset+3])
		offset += 4
		if class != openFlowOXMClassBasic {
			return OpenFlowMatch{}, wrap(ErrorUnsupported, "", "", "parse", "match", "unsupported OXM class", nil)
		}
		if offset+size > length {
			return OpenFlowMatch{}, wrap(ErrorValidation, "", "", "parse", "match", "truncated OXM value", nil)
		}
		field := fieldMask >> 1
		hasMask := fieldMask&1 == 1
		raw := data[offset : offset+size]
		offset += size
		value := raw
		mask := []byte(nil)
		if hasMask {
			if size%2 != 0 {
				return OpenFlowMatch{}, wrap(ErrorValidation, "", "", "parse", "match", "invalid masked OXM length", nil)
			}
			value = raw[:size/2]
			mask = raw[size/2:]
		}
		if err := applyParsedOXM(&match, field, value, mask); err != nil {
			return OpenFlowMatch{}, err
		}
	}
	return match, nil
}

func applyParsedOXM(match *OpenFlowMatch, field uint8, value, mask []byte) error {
	switch field {
	case openFlowOFBInPort:
		if len(value) != 4 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint32(value)
		match.InPort = &v
	case openFlowOFBMetadata:
		if len(value) != 8 {
			return invalidOXMLength(field)
		}
		v := OpenFlowMetadata{Value: binary.BigEndian.Uint64(value)}
		if len(mask) == 8 {
			v.Mask = binary.BigEndian.Uint64(mask)
		}
		match.Metadata = &v
	case openFlowOFBEthDst:
		if len(value) != 6 {
			return invalidOXMLength(field)
		}
		match.EthDst = net.HardwareAddr(value).String()
	case openFlowOFBEthSrc:
		if len(value) != 6 {
			return invalidOXMLength(field)
		}
		match.EthSrc = net.HardwareAddr(value).String()
	case openFlowOFBEthType:
		if len(value) != 2 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint16(value)
		match.EthType = &v
	case openFlowOFBIPProto:
		if len(value) != 1 {
			return invalidOXMLength(field)
		}
		v := value[0]
		match.IPProto = &v
	case openFlowOFBIPv4Src:
		cidr, err := formatParsedIPv4(value, mask)
		if err != nil {
			return err
		}
		match.IPv4Src = cidr
	case openFlowOFBIPv4Dst:
		cidr, err := formatParsedIPv4(value, mask)
		if err != nil {
			return err
		}
		match.IPv4Dst = cidr
	case openFlowOFBTCPSrc:
		if len(value) != 2 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint16(value)
		match.TCPSrc = &v
	case openFlowOFBTCPDst:
		if len(value) != 2 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint16(value)
		match.TCPDst = &v
	case openFlowOFBUDPSrc:
		if len(value) != 2 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint16(value)
		match.UDPSrc = &v
	case openFlowOFBUDPDst:
		if len(value) != 2 {
			return invalidOXMLength(field)
		}
		v := binary.BigEndian.Uint16(value)
		match.UDPDst = &v
	default:
		return wrap(ErrorUnsupported, "", "", "parse", "match", fmt.Sprintf("unsupported OXM field %d", field), nil)
	}
	return nil
}

func marshalOXM(field uint8, masked bool, value, mask []byte) []byte {
	length := len(value)
	if masked {
		length += len(mask)
	}
	out := make([]byte, 4+length)
	binary.BigEndian.PutUint16(out[0:2], openFlowOXMClassBasic)
	out[2] = field << 1
	if masked {
		out[2] |= 1
	}
	out[3] = byte(length)
	copy(out[4:], value)
	if masked {
		copy(out[4+len(value):], mask)
	}
	return out
}

func ipv4OXM(raw string) ([]byte, []byte, bool, error) {
	prefix, err := parseOpenFlowIPv4Prefix(raw)
	if err != nil {
		return nil, nil, false, err
	}
	addr := prefix.Addr().As4()
	value := addr[:]
	if prefix.Bits() == 32 {
		return append([]byte{}, value...), nil, false, nil
	}
	mask := make([]byte, 4)
	for i := 0; i < prefix.Bits(); i++ {
		mask[i/8] |= 1 << uint(7-(i%8))
	}
	return append([]byte{}, value...), mask, true, nil
}

func parseOpenFlowIPv4Prefix(raw string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		if !prefix.Addr().Is4() {
			return netip.Prefix{}, wrap(ErrorUnsupported, "", "", "parse", raw, "only IPv4 OpenFlow matches are supported", nil)
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, wrap(ErrorValidation, "", "", "parse", raw, "invalid IPv4 address or CIDR", err)
	}
	if !addr.Is4() {
		return netip.Prefix{}, wrap(ErrorUnsupported, "", "", "parse", raw, "only IPv4 OpenFlow matches are supported", nil)
	}
	return netip.PrefixFrom(addr, 32), nil
}

func formatParsedIPv4(value, mask []byte) (string, error) {
	if len(value) != 4 {
		return "", invalidOXMLength(openFlowOFBIPv4Dst)
	}
	addr := netip.AddrFrom4([4]byte{value[0], value[1], value[2], value[3]})
	if len(mask) == 0 {
		return addr.String(), nil
	}
	if len(mask) != 4 {
		return "", wrap(ErrorValidation, "", "", "parse", "match", "invalid IPv4 mask length", nil)
	}
	bits := 0
	for _, b := range mask {
		for i := 7; i >= 0; i-- {
			if b&(1<<uint(i)) == 0 {
				return netip.PrefixFrom(addr, bits).Masked().String(), nil
			}
			bits++
		}
	}
	return netip.PrefixFrom(addr, 32).String(), nil
}

func invalidOXMLength(field uint8) error {
	return wrap(ErrorValidation, "", "", "parse", "match", fmt.Sprintf("invalid OXM field %d length", field), nil)
}

func validateOpenFlowVersion(version OpenFlowVersion) error {
	switch version {
	case OpenFlow13, OpenFlow15:
		return nil
	default:
		return wrap(ErrorUnsupported, "", "", "validate", "openflow-version", "unsupported OpenFlow version", nil)
	}
}

func highestOpenFlowVersion(versions []OpenFlowVersion) OpenFlowVersion {
	if len(versions) == 0 {
		return OpenFlow15
	}
	out := versions[0]
	for _, version := range versions[1:] {
		if version > out {
			out = version
		}
	}
	return out
}

func normalizeOpenFlowVersions(versions []OpenFlowVersion) ([]OpenFlowVersion, error) {
	if len(versions) == 0 {
		versions = []OpenFlowVersion{OpenFlow15, OpenFlow13}
	}
	seen := map[OpenFlowVersion]bool{}
	out := make([]OpenFlowVersion, 0, len(versions))
	for _, version := range versions {
		if err := validateOpenFlowVersion(version); err != nil {
			return nil, err
		}
		if seen[version] {
			continue
		}
		seen[version] = true
		out = append(out, version)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out, nil
}

func writeBE(buf *bytes.Buffer, value any) {
	_ = binary.Write(buf, binary.BigEndian, value)
}

func padTo(buf *bytes.Buffer, alignment int) {
	if alignment <= 0 {
		return
	}
	for buf.Len()%alignment != 0 {
		buf.WriteByte(0)
	}
}
