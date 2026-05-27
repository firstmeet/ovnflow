//go:build linux

package linuxrouter

import (
	"context"

	"github.com/firstmeet/ovnflow/v2"
)

type routerOVSManager interface {
	EnsureRouter(context.Context, Router) error
}

type routerOVSClient interface {
	GetBridge(context.Context, string) (*ovnflow.OVSBridge, error)
	GetPort(context.Context, string) (*ovnflow.OVSPort, error)
	GetInterface(context.Context, string) (*ovnflow.OVSInterface, error)
	GetInterfaceByUUID(context.Context, string) (*ovnflow.OVSInterface, error)
	EnsureInternalPort(context.Context, string, string, map[string]string, map[string]string) error
}

type sdkOVSClient struct {
	ovs *ovnflow.OVSClient
}

func (c sdkOVSClient) GetBridge(ctx context.Context, name string) (*ovnflow.OVSBridge, error) {
	if c.ovs == nil {
		return nil, ovnflow.ErrBackendUnavailable
	}
	return c.ovs.GetBridge(ctx, name)
}

func (c sdkOVSClient) GetPort(ctx context.Context, name string) (*ovnflow.OVSPort, error) {
	if c.ovs == nil {
		return nil, ovnflow.ErrBackendUnavailable
	}
	return c.ovs.GetPort(ctx, name)
}

func (c sdkOVSClient) GetInterface(ctx context.Context, name string) (*ovnflow.OVSInterface, error) {
	if c.ovs == nil {
		return nil, ovnflow.ErrBackendUnavailable
	}
	return c.ovs.GetInterface(ctx, name)
}

func (c sdkOVSClient) GetInterfaceByUUID(ctx context.Context, uuid string) (*ovnflow.OVSInterface, error) {
	if c.ovs == nil {
		return nil, ovnflow.ErrBackendUnavailable
	}
	return c.ovs.GetInterfaceByUUID(ctx, uuid)
}

func (c sdkOVSClient) EnsureInternalPort(ctx context.Context, bridge, portName string, portIDs, ifaceIDs map[string]string) error {
	if c.ovs == nil {
		return ovnflow.ErrBackendUnavailable
	}
	port := c.ovs.Bridge(bridge).Ensure().AddPort(portName).WithInterfaceType("internal")
	for _, key := range sortedMapKeys(portIDs) {
		port.WithExternalID(key, portIDs[key])
	}
	for _, key := range sortedMapKeys(ifaceIDs) {
		port.WithInterfaceExternalID(key, ifaceIDs[key])
	}
	return port.Execute(ctx)
}

type sdkRouterOVSManager struct {
	ovs routerOVSClient
}

func (m sdkRouterOVSManager) EnsureRouter(ctx context.Context, router Router) error {
	if m.ovs == nil {
		return ovnflow.ErrBackendUnavailable
	}
	ns := router.Spec.namespaceOrDefault(router.Name)
	for _, iface := range router.Spec.Interfaces {
		if iface.Bridge == "" || iface.OVSPort == "" {
			continue
		}
		if err := m.ensureInterface(ctx, router.Name, ns, iface); err != nil {
			return err
		}
	}
	return nil
}

func (m sdkRouterOVSManager) ensureInterface(ctx context.Context, routerName, ns string, iface Interface) error {
	if _, err := m.ovs.GetBridge(ctx, iface.Bridge); err != nil {
		return err
	}
	portExists := false
	var existingPort *ovnflow.OVSPort
	if port, err := m.ovs.GetPort(ctx, iface.OVSPort); err == nil {
		portExists = true
		existingPort = port
		if !linuxRouterOVSOwnedBy(port.ExternalIDs, ns) {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Port:" + iface.OVSPort, Message: "port is not managed by ovnflow LinuxRouter namespace " + ns}
		}
	} else if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return err
	}
	if ovsIface, err := m.ovs.GetInterface(ctx, iface.OVSPort); err == nil {
		if !linuxRouterOVSOwnedBy(ovsIface.ExternalIDs, ns) {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Interface:" + iface.OVSPort, Message: "interface is not managed by ovnflow LinuxRouter namespace " + ns}
		}
	} else if portExists && ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Interface:" + iface.OVSPort, Message: "existing port is missing the expected ovnflow LinuxRouter interface"}
	} else if !ovnflow.IsKind(err, ovnflow.ErrorNotFound) {
		return err
	}
	if existingPort != nil {
		if err := m.validateExistingPortInterfaces(ctx, existingPort, ns, iface); err != nil {
			return err
		}
	}
	portIDs := linuxRouterOVSExternalIDs(routerName, ns, iface.Name)
	mergeExternalIDs(portIDs, iface.PortExternalIDs)
	ifaceIDs := linuxRouterOVSExternalIDs(routerName, ns, iface.Name)
	mergeExternalIDs(ifaceIDs, iface.InterfaceExternalIDs)
	return m.ovs.EnsureInternalPort(ctx, iface.Bridge, iface.OVSPort, portIDs, ifaceIDs)
}

func (m sdkRouterOVSManager) validateExistingPortInterfaces(ctx context.Context, port *ovnflow.OVSPort, ns string, iface Interface) error {
	if len(port.Interfaces) == 0 {
		return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Port:" + iface.OVSPort, Message: "existing port has no interface to update safely"}
	}
	for _, ifaceUUID := range port.Interfaces {
		ovsIface, err := m.ovs.GetInterfaceByUUID(ctx, ifaceUUID)
		if err != nil {
			return err
		}
		if ovsIface.Name != iface.OVSPort {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Interface:" + ovsIface.Name, Message: "existing port references an unexpected interface"}
		}
		if !linuxRouterOVSOwnedBy(ovsIface.ExternalIDs, ns) {
			return &ovnflow.Error{Kind: ovnflow.ErrorOwnershipViolation, Operation: "ensure", Object: "Interface:" + ovsIface.Name, Message: "port interface is not managed by ovnflow LinuxRouter namespace " + ns}
		}
	}
	return nil
}

func linuxRouterOVSOwnedBy(externalIDs map[string]string, ns string) bool {
	return externalIDs[ovnflow.ExternalIDManagedByKey] == "ovnflow" &&
		externalIDs[ovnflow.ExternalIDKindKey] == "LinuxRouter" &&
		externalIDs[ovnflow.ExternalIDPrefix+"linux-router-ns"] == ns
}

func linuxRouterOVSExternalIDs(routerName, ns, ifaceName string) map[string]string {
	return map[string]string{
		ovnflow.ExternalIDManagedByKey:                  "ovnflow",
		ovnflow.ExternalIDAPIVersionKey:                 "v2",
		ovnflow.ExternalIDKindKey:                       "LinuxRouter",
		ovnflow.ExternalIDNameKey:                       routerName,
		ovnflow.ExternalIDPrefix + "linux-router-ns":    ns,
		ovnflow.ExternalIDPrefix + "linux-router-iface": ifaceName,
	}
}

func mergeExternalIDs(dst, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}
