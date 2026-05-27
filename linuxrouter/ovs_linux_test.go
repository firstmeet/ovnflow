//go:build linux

package linuxrouter

import (
	"context"
	"testing"

	"github.com/firstmeet/ovnflow/v2"
)

func TestSDKRouterOVSManagerWritesPortAndInterfaceExternalIDs(t *testing.T) {
	ovs := &fakeRouterOVSClient{
		bridges: map[string]*ovnflow.OVSBridge{"br-int": {Name: "br-int"}},
	}
	router := Router{Name: "edge", Spec: Spec{
		Namespace: "ovnflow-edge",
		Interfaces: []Interface{{
			Name:                 "lan0",
			Bridge:               "br-int",
			OVSPort:              "edge-lan",
			PortExternalIDs:      map[string]string{"port-owner": "virtverse"},
			InterfaceExternalIDs: map[string]string{"iface-id": "nsr-xxx"},
		}},
	}}
	if err := (sdkRouterOVSManager{ovs: ovs}).EnsureRouter(context.Background(), router); err != nil {
		t.Fatalf("EnsureRouter returned error: %v", err)
	}
	if len(ovs.ensureCalls) != 1 {
		t.Fatalf("ensure calls = %#v, want one", ovs.ensureCalls)
	}
	call := ovs.ensureCalls[0]
	if call.bridge != "br-int" || call.portName != "edge-lan" {
		t.Fatalf("ensure call = %#v", call)
	}
	if call.portIDs[ovnflow.ExternalIDKindKey] != "LinuxRouter" || call.portIDs[ovnflow.ExternalIDPrefix+"linux-router-ns"] != "ovnflow-edge" {
		t.Fatalf("port external_ids missing ovnflow metadata: %#v", call.portIDs)
	}
	if call.portIDs["port-owner"] != "virtverse" {
		t.Fatalf("port external_ids = %#v, want custom port-owner", call.portIDs)
	}
	if call.ifaceIDs[ovnflow.ExternalIDKindKey] != "LinuxRouter" || call.ifaceIDs[ovnflow.ExternalIDPrefix+"linux-router-iface"] != "lan0" {
		t.Fatalf("interface external_ids missing ovnflow metadata: %#v", call.ifaceIDs)
	}
	if call.ifaceIDs["iface-id"] != "nsr-xxx" {
		t.Fatalf("interface external_ids = %#v, want iface-id", call.ifaceIDs)
	}
}

func TestSDKRouterOVSManagerRejectsUnownedExistingPort(t *testing.T) {
	ovs := &fakeRouterOVSClient{
		bridges: map[string]*ovnflow.OVSBridge{"br-int": {Name: "br-int"}},
		ports:   map[string]*ovnflow.OVSPort{"edge-lan": {Name: "edge-lan", ExternalIDs: map[string]string{"owner": "other"}}},
	}
	router := Router{Name: "edge", Spec: Spec{
		Namespace:  "ovnflow-edge",
		Interfaces: []Interface{{Name: "lan0", Bridge: "br-int", OVSPort: "edge-lan"}},
	}}
	err := (sdkRouterOVSManager{ovs: ovs}).EnsureRouter(context.Background(), router)
	if !ovnflow.IsKind(err, ovnflow.ErrorOwnershipViolation) {
		t.Fatalf("EnsureRouter error = %v, want ownership violation", err)
	}
	if len(ovs.ensureCalls) != 0 {
		t.Fatalf("ensure should not be called after ownership violation: %#v", ovs.ensureCalls)
	}
}

func TestSDKRouterOVSManagerRejectsDifferentNamespaceInterface(t *testing.T) {
	ovs := &fakeRouterOVSClient{
		bridges: map[string]*ovnflow.OVSBridge{"br-int": {Name: "br-int"}},
		ports: map[string]*ovnflow.OVSPort{"edge-lan": {
			Name:        "edge-lan",
			Interfaces:  []string{"iface-uuid"},
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "ovnflow-edge", "lan0"),
		}},
		ifaces: map[string]*ovnflow.OVSInterface{"edge-lan": {
			Name:        "edge-lan",
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "other-ns", "lan0"),
		}},
		ifacesByUUID: map[string]*ovnflow.OVSInterface{"iface-uuid": {
			Name:        "edge-lan",
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "other-ns", "lan0"),
		}},
	}
	router := Router{Name: "edge", Spec: Spec{
		Namespace:  "ovnflow-edge",
		Interfaces: []Interface{{Name: "lan0", Bridge: "br-int", OVSPort: "edge-lan"}},
	}}
	err := (sdkRouterOVSManager{ovs: ovs}).EnsureRouter(context.Background(), router)
	if !ovnflow.IsKind(err, ovnflow.ErrorOwnershipViolation) {
		t.Fatalf("EnsureRouter error = %v, want ownership violation", err)
	}
}

func TestSDKRouterOVSManagerRejectsUnexpectedPortInterface(t *testing.T) {
	ovs := &fakeRouterOVSClient{
		bridges: map[string]*ovnflow.OVSBridge{"br-int": {Name: "br-int"}},
		ports: map[string]*ovnflow.OVSPort{"edge-lan": {
			Name:        "edge-lan",
			Interfaces:  []string{"iface-uuid"},
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "ovnflow-edge", "lan0"),
		}},
		ifaces: map[string]*ovnflow.OVSInterface{"edge-lan": {
			Name:        "edge-lan",
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "ovnflow-edge", "lan0"),
		}},
		ifacesByUUID: map[string]*ovnflow.OVSInterface{"iface-uuid": {
			Name:        "other-iface",
			ExternalIDs: linuxRouterOVSExternalIDs("edge", "ovnflow-edge", "lan0"),
		}},
	}
	router := Router{Name: "edge", Spec: Spec{
		Namespace:  "ovnflow-edge",
		Interfaces: []Interface{{Name: "lan0", Bridge: "br-int", OVSPort: "edge-lan"}},
	}}
	err := (sdkRouterOVSManager{ovs: ovs}).EnsureRouter(context.Background(), router)
	if !ovnflow.IsKind(err, ovnflow.ErrorOwnershipViolation) {
		t.Fatalf("EnsureRouter error = %v, want ownership violation", err)
	}
}

func TestMixedClientRunsOVSDBBeforeInterfaceCommands(t *testing.T) {
	exec := &FakeExecutor{}
	ovs := &fakeRouterOVSManager{}
	client := newObservedClient(exec, LinuxRenderer{OVSDBManaged: true}, nil, ovs)
	router := Router{Name: "edge", Spec: Spec{
		Namespace: "ovnflow-edge",
		Interfaces: []Interface{{
			Name:                 "lan0",
			Bridge:               "br-int",
			OVSPort:              "edge-lan",
			InterfaceExternalIDs: map[string]string{"iface-id": "nsr-xxx"},
		}},
	}}
	if err := client.Router("edge").Apply(context.Background(), router); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(ovs.routers) != 1 {
		t.Fatalf("OVS EnsureRouter calls = %#v, want one", ovs.routers)
	}
	commands := exec.Snapshot()
	assertNoProgram(t, commands, "ovs-vsctl")
	assertCommand(t, commands, "ip", "netns", "add", "ovnflow-edge")
	assertCommandContains(t, commands, "ip", "link", "set", "edge-lan", "netns", "ovnflow-edge")
}

type fakeRouterOVSClient struct {
	bridges      map[string]*ovnflow.OVSBridge
	ports        map[string]*ovnflow.OVSPort
	ifaces       map[string]*ovnflow.OVSInterface
	ifacesByUUID map[string]*ovnflow.OVSInterface
	ensureCalls  []fakeEnsureInternalPortCall
}

type fakeEnsureInternalPortCall struct {
	bridge   string
	portName string
	portIDs  map[string]string
	ifaceIDs map[string]string
}

func (f *fakeRouterOVSClient) GetBridge(_ context.Context, name string) (*ovnflow.OVSBridge, error) {
	if value := f.bridges[name]; value != nil {
		return value, nil
	}
	return nil, ovnflow.ErrNotFound
}

func (f *fakeRouterOVSClient) GetPort(_ context.Context, name string) (*ovnflow.OVSPort, error) {
	if value := f.ports[name]; value != nil {
		return value, nil
	}
	return nil, ovnflow.ErrNotFound
}

func (f *fakeRouterOVSClient) GetInterface(_ context.Context, name string) (*ovnflow.OVSInterface, error) {
	if value := f.ifaces[name]; value != nil {
		return value, nil
	}
	return nil, ovnflow.ErrNotFound
}

func (f *fakeRouterOVSClient) GetInterfaceByUUID(_ context.Context, uuid string) (*ovnflow.OVSInterface, error) {
	if value := f.ifacesByUUID[uuid]; value != nil {
		return value, nil
	}
	return nil, ovnflow.ErrNotFound
}

func (f *fakeRouterOVSClient) EnsureInternalPort(_ context.Context, bridge, portName string, portIDs, ifaceIDs map[string]string) error {
	f.ensureCalls = append(f.ensureCalls, fakeEnsureInternalPortCall{
		bridge:   bridge,
		portName: portName,
		portIDs:  cloneStringMap(portIDs),
		ifaceIDs: cloneStringMap(ifaceIDs),
	})
	return nil
}

type fakeRouterOVSManager struct {
	routers []Router
}

func (f *fakeRouterOVSManager) EnsureRouter(_ context.Context, router Router) error {
	f.routers = append(f.routers, cloneRouter(router))
	return nil
}
