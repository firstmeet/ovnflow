//go:build !linux

package sdwanlinux

import (
	"context"

	"github.com/firstmeet/ovnflow/v2"
)

type Command struct {
	Program             string
	Args                []string
	IgnoreNotFound      bool
	IgnoreAlreadyExists bool
}

type Executor interface {
	Run(context.Context, Command) error
}

type OVSManager interface {
	EnsureTunnel(context.Context, OVSTunnel) error
	DeleteTunnel(context.Context, OVSTunnel) error
}

type OpenFlowManager interface {
	EnsureRule(context.Context, OpenFlowRule) error
	DeleteRule(context.Context, OpenFlowRule) error
}

type OVSTunnel struct {
	Network    string
	Link       string
	LocalSite  string
	RemoteSite string
	Bridge     string
	Port       string
	Type       string
	RemoteIP   string
	Key        string
	DstPort    string
	ExternalID map[string]string
}

type OpenFlowRule struct {
	Network   string
	Link      string
	Bridge    string
	RuleName  string
	TableID   uint8
	Priority  uint16
	Cookie    uint64
	Match     ovnflow.OpenFlowMatch
	Actions   []ovnflow.OpenFlowAction
	Transport ovnflow.SDWANTransport
}

type Config struct {
	LocalSite       string
	Executor        Executor
	OVS             OVSManager
	OpenFlow        OpenFlowManager
	InterfacePrefix string
	RouteTable      int
}

type Backend struct{}

type FakeExecutor struct{}

func NewBackend(Config) (*Backend, error) {
	return nil, unsupported()
}

func MustNewBackend(Config) *Backend {
	panic(unsupported())
}

func (b *Backend) GetSDWAN(context.Context, string) (*ovnflow.SDWANNetwork, error) {
	return nil, unsupported()
}

func (b *Backend) ApplySDWAN(context.Context, ovnflow.SDWANNetwork, ovnflow.SDWANApplyPlan) error {
	return unsupported()
}

func (b *Backend) DeleteSDWAN(context.Context, string) error {
	return unsupported()
}

func (f *FakeExecutor) Run(context.Context, Command) error {
	return unsupported()
}

func (f *FakeExecutor) Snapshot() []Command {
	return nil
}

type unsupportedOVSManager struct{}

func NewOVSManager(*ovnflow.OVSClient) OVSManager {
	return unsupportedOVSManager{}
}

func (unsupportedOVSManager) EnsureTunnel(context.Context, OVSTunnel) error {
	return unsupported()
}

func (unsupportedOVSManager) DeleteTunnel(context.Context, OVSTunnel) error {
	return unsupported()
}

type unsupportedOpenFlowManager struct{}

func NewOpenFlowManager(*ovnflow.OpenFlowClient) OpenFlowManager {
	return unsupportedOpenFlowManager{}
}

func (unsupportedOpenFlowManager) EnsureRule(context.Context, OpenFlowRule) error {
	return unsupported()
}

func (unsupportedOpenFlowManager) DeleteRule(context.Context, OpenFlowRule) error {
	return unsupported()
}

func unsupported() error {
	return &ovnflow.Error{
		Kind:      ovnflow.ErrorUnsupported,
		Operation: "sdwanlinux",
		Message:   "sdwanlinux backend is only supported on linux builds",
	}
}
