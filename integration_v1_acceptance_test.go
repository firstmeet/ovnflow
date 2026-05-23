//go:build integration

package ovnflow

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/firstmeet/ovnflow/internal/ovsdbjson"
	libovsdb "github.com/ovn-kubernetes/libovsdb/ovsdb"
)

const (
	// EnvV1SchemaChecks enables read-only schema checks for the v1.0 NB, SB,
	// and OVS surfaces.
	EnvV1SchemaChecks = "OVNFLOW_V1_SCHEMA_CHECKS"

	// EnvV1MutationChecks enables broad v1.0 lifecycle tests that mutate
	// OVN/OVS rows beyond the always-on integration scenarios.
	EnvV1MutationChecks = "OVNFLOW_V1_MUTATION_CHECKS"

	// EnvV02SchemaChecks is kept as a compatibility alias for existing CI and
	// local scripts.
	EnvV02SchemaChecks = "OVNFLOW_V02_SCHEMA_CHECKS"

	// EnvV02MutationChecks is kept as a compatibility alias for existing CI and
	// local scripts.
	EnvV02MutationChecks = "OVNFLOW_V02_MUTATION_CHECKS"
)

type integrationSchemaCheck struct {
	table   string
	columns []string
}

type integrationSchemaReferenceCheck struct {
	table    string
	column   string
	refTable string
	kind     referenceColumnKind
	keyRef   bool
	valueRef bool
}

type v1NorthboundResources struct {
	lrName             string
	lsName             string
	lrpName            string
	aclMatch           string
	natLogicalIP       string
	lbName             string
	dhcpCIDR           string
	dnsName            string
	qosMatch           string
	meterName          string
	meterBandName      string
	portGroupName      string
	addressSetName     string
	gatewayChassisName string
	haChassisName      string
	haGroupName        string
	bfdDstIP           string
	suffix             string
}

type v1OVSResources struct {
	bridgeName    string
	managerTarget string
	qosName       string
	queueName     string
	mirrorName    string
	flowTableName string
	netFlowName   string
	sFlowName     string
	ipfixName     string
	autoAttach    string
	suffix        string
}

var v1NorthboundSchemaPlan = []integrationSchemaCheck{
	{table: "Logical_Switch", columns: []string{"name", "ports", "qos_rules", "other_config", "external_ids"}},
	{table: "Logical_Switch_Port", columns: []string{"name", "addresses", "options", "type", "external_ids"}},
	{table: "Logical_Router", columns: []string{"name", "ports", "static_routes", "nat", "options", "external_ids"}},
	{table: "Logical_Router_Port", columns: []string{"name", "mac", "networks", "options", "external_ids"}},
	{table: "ACL", columns: []string{"priority", "direction", "match", "action", "external_ids"}},
	{table: "NAT", columns: []string{"type", "logical_ip", "external_ip", "external_ids"}},
	{table: "Load_Balancer", columns: []string{"name", "vips", "protocol", "external_ids"}},
	{table: "DHCP_Options", columns: []string{"cidr", "options", "external_ids"}},
	{table: "DNS", columns: []string{"records", "external_ids"}},
	{table: "QoS", columns: []string{"priority", "direction", "match", "action", "bandwidth", "external_ids"}},
	{table: "Meter", columns: []string{"name", "unit", "bands", "external_ids"}},
	{table: "Meter_Band", columns: []string{"action", "rate", "external_ids"}},
	{table: "Port_Group", columns: []string{"name", "ports", "acls", "external_ids"}},
	{table: "Address_Set", columns: []string{"name", "addresses", "external_ids"}},
	{table: "Gateway_Chassis", columns: []string{"name", "chassis_name", "priority", "external_ids"}},
	{table: "HA_Chassis", columns: []string{"chassis_name", "priority", "external_ids"}},
	{table: "HA_Chassis_Group", columns: []string{"name", "ha_chassis", "external_ids"}},
	{table: "BFD", columns: []string{"logical_port", "dst_ip", "status", "external_ids"}},
}

var v1NorthboundReferencePlan = []integrationSchemaReferenceCheck{
	{table: tableLogicalSwitch, column: colPorts, refTable: tableLogicalSwitchPort, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalSwitch, column: colQoSRules, refTable: tableQoS, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalRouter, column: colPorts, refTable: tableLogicalRouterPort, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalRouter, column: colNAT, refTable: tableNAT, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalRouter, column: colLoadBalancer, refTable: tableLoadBalancer, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalRouterPort, column: colGatewayChassis, refTable: tableGatewayChassis, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableLogicalRouterPort, column: colHAChassisGroup, refTable: tableHAChassisGroup, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableMeter, column: colBands, refTable: tableMeterBand, kind: referenceColumnSetUUID, keyRef: true},
	{table: tablePortGroup, column: colACLs, refTable: tableACL, kind: referenceColumnSetUUID, keyRef: true},
	{table: tablePortGroup, column: colPorts, refTable: tableLogicalSwitchPort, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableHAChassisGroup, column: colHAChassis, refTable: tableHAChassis, kind: referenceColumnSetUUID, keyRef: true},
}

var v1SouthboundSchemaPlan = []integrationSchemaCheck{
	{table: "Chassis", columns: []string{"name", "hostname", "external_ids"}},
	{table: "Port_Binding", columns: []string{"logical_port", "chassis", "datapath", "mac", "external_ids"}},
	{table: "Datapath_Binding", columns: []string{"tunnel_key", "external_ids"}},
	{table: "Logical_Flow", columns: []string{"pipeline", "table_id", "match", "actions", "external_ids"}},
	{table: "MAC_Binding", columns: []string{"logical_port", "ip", "mac", "datapath"}},
	{table: "FDB", columns: []string{"mac", "dp_key", "port_key"}},
	{table: "Multicast_Group", columns: []string{"datapath", "tunnel_key", "ports"}},
	{table: "Service_Monitor", columns: []string{"logical_port", "ip", "protocol", "port", "status"}},
	{table: "RBAC_Role", columns: []string{"name", "permissions"}},
	{table: "RBAC_Permission", columns: []string{"table", "authorization", "insert_delete", "update"}},
	{table: "Meter", columns: []string{"name", "unit", "bands"}},
	{table: "Meter_Band", columns: []string{"action", "rate"}},
	{table: "DNS", columns: []string{"records", "datapaths", "external_ids"}},
	{table: "BFD", columns: []string{"logical_port", "dst_ip", "status", "external_ids"}},
}

var v1SouthboundReferencePlan = []integrationSchemaReferenceCheck{
	{table: tablePortBinding, column: colChassis, refTable: tableChassis, kind: referenceColumnSetUUID, keyRef: true},
	{table: tablePortBinding, column: colDatapath, refTable: tableDatapathBinding, kind: referenceColumnScalarUUID, keyRef: true},
	{table: tableMACBinding, column: colDatapath, refTable: tableDatapathBinding, kind: referenceColumnScalarUUID, keyRef: true},
	{table: tableMulticastGroup, column: colDatapath, refTable: tableDatapathBinding, kind: referenceColumnScalarUUID, keyRef: true},
	{table: tableMulticastGroup, column: colPorts, refTable: tablePortBinding, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableRBACRole, column: "permissions", refTable: tableRBACPermission, kind: referenceColumnMapUUID, valueRef: true},
	{table: tableMeter, column: colBands, refTable: tableMeterBand, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableDNS, column: "datapaths", refTable: tableDatapathBinding, kind: referenceColumnSetUUID, keyRef: true},
}

var v1OpenVSwitchSchemaPlan = []integrationSchemaCheck{
	{table: "Open_vSwitch", columns: []string{"bridges", "manager_options", "ssl", "external_ids"}},
	{table: "Bridge", columns: []string{"name", "ports", "controller", "mirrors", "external_ids"}},
	{table: "Port", columns: []string{"name", "interfaces", "external_ids"}},
	{table: "Interface", columns: []string{"name", "type", "options", "external_ids"}},
	{table: "Controller", columns: []string{"target", "external_ids"}},
	{table: "Manager", columns: []string{"target", "external_ids"}},
	{table: "Mirror", columns: []string{"name", "select_src_port", "select_dst_port", "output_port"}},
	{table: "QoS", columns: []string{"type", "queues", "external_ids"}},
	{table: "Queue", columns: []string{"external_ids", "other_config"}},
	{table: "Flow_Table", columns: []string{"name", "external_ids"}},
	{table: "NetFlow", columns: []string{"targets", "engine_type", "engine_id", "active_timeout"}},
	{table: "sFlow", columns: []string{"agent", "targets", "header", "sampling", "polling"}},
	{table: "IPFIX", columns: []string{"targets", "sampling", "external_ids"}},
	{table: "SSL", columns: []string{"private_key", "certificate", "ca_cert", "bootstrap_ca_cert"}},
	{table: "AutoAttach", columns: []string{"system_name", "system_description", "mappings"}},
}

var v1OpenVSwitchReferencePlan = []integrationSchemaReferenceCheck{
	{table: tableOpenVSwitch, column: colBridges, refTable: tableBridge, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableOpenVSwitch, column: colManagerOptions, refTable: tableManager, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableOpenVSwitch, column: colSSL, refTable: tableSSL, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colPorts, refTable: tablePort, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colController, refTable: tableController, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colMirrors, refTable: tableMirror, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colFlowTables, refTable: tableFlowTable, kind: referenceColumnMapUUID, valueRef: true},
	{table: tableBridge, column: colNetFlow, refTable: tableNetFlow, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colSFlow, refTable: tableSFlow, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colIPFIX, refTable: tableIPFIX, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableBridge, column: colAutoAttach, refTable: tableAutoAttach, kind: referenceColumnSetUUID, keyRef: true},
	{table: tablePort, column: colInterfaces, refTable: tableInterface, kind: referenceColumnSetUUID, keyRef: true},
	{table: tablePort, column: colQoS, refTable: tableQoS, kind: referenceColumnSetUUID, keyRef: true},
	{table: tableQoS, column: colQueues, refTable: tableQueue, kind: referenceColumnMapUUID, valueRef: true},
}

func TestIntegrationV1SchemaReadiness(t *testing.T) {
	requireAnyEnvOptIn(t, "read-only v1.0 schema checks", EnvV1SchemaChecks, EnvV02SchemaChecks)

	cfg := requireIntegrationConfig(t)
	checks := []struct {
		name     string
		address  string
		database string
		required []integrationSchemaCheck
		refs     []integrationSchemaReferenceCheck
	}{
		{name: "Northbound", address: cfg.OVNNBAddr, database: nbDatabase, required: v1NorthboundSchemaPlan, refs: v1NorthboundReferencePlan},
		{name: "Southbound", address: cfg.OVNSBAddr, database: sbDatabase, required: v1SouthboundSchemaPlan, refs: v1SouthboundReferencePlan},
		{name: "Open_vSwitch", address: cfg.OVSAddr, database: ovsDatabase, required: v1OpenVSwitchSchemaPlan, refs: v1OpenVSwitchReferencePlan},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			client := dialOVSDBOrSkip(t, check.address)
			t.Cleanup(func() {
				_ = client.Close()
			})
			schema := getIntegrationSchema(t, client, check.database)
			assertSchemaReadiness(t, schema, check.required)
			assertSchemaReferenceReadiness(t, check.database, schema, check.refs)
		})
	}
}

func TestIntegrationV1MutationScenariosAreEnvGated(t *testing.T) {
	requireAnyEnvOptIn(t, "v1.0 mutation acceptance", EnvV1MutationChecks, EnvV02MutationChecks)

	cfg := requireIntegrationConfig(t)
	sdk := connectSDKOrSkip(t, cfg)
	t.Cleanup(sdk.Close)

	rawNB := dialOVSDBOrSkip(t, cfg.OVNNBAddr)
	t.Cleanup(func() { _ = rawNB.Close() })
	rawOVS := dialOVSDBOrSkip(t, cfg.OVSAddr)
	t.Cleanup(func() { _ = rawOVS.Close() })

	suffix := uniqueSuffix()
	ctx := testContext(t)

	t.Run("northbound L3 policy service lifecycle", func(t *testing.T) {
		prefix := cfg.ResourcePrefix + "v1-" + suffix + "-"
		resources := v1NorthboundResources{
			lrName:             prefix + "lr",
			lsName:             prefix + "ls",
			lrpName:            prefix + "lrp",
			aclMatch:           "outport == \"" + prefix + "vm\"",
			natLogicalIP:       "10.210.0.0/24",
			lbName:             prefix + "lb",
			dhcpCIDR:           "10.210.0.0/24",
			dnsName:            prefix + "dns",
			qosMatch:           "ip4.src == 10.210.0.10",
			meterName:          prefix + "meter",
			meterBandName:      prefix + "meter-band",
			portGroupName:      prefix + "pg",
			addressSetName:     prefix + "as",
			gatewayChassisName: prefix + "gwc",
			haChassisName:      prefix + "hac",
			haGroupName:        prefix + "hag",
			bfdDstIP:           "10.210.0.2",
			suffix:             suffix,
		}

		cleanupV1Northbound(ctx, t, sdk, rawNB, resources)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupV1Northbound(cleanupCtx, t, sdk, rawNB, resources)
		})

		must(t, sdk.OVN().NB().LogicalRouter(resources.lrName).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure logical router")
		must(t, sdk.OVN().NB().LogicalRouter(resources.lrName).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure logical router")
		must(t, sdk.OVN().NB().LogicalSwitch(resources.lsName).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure logical switch")
		must(t, sdk.OVN().NB().LogicalRouterPort(resources.lrpName).Ensure().WithMAC("00:00:5e:00:53:01").WithNetwork("10.210.0.1/24").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure router port")
		must(t, sdk.OVN().NB().ACLByMatch("to-lport", 1001, resources.aclMatch).Ensure().WithAction("allow").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure acl")
		must(t, sdk.OVN().NB().NATByLogicalIP("snat", resources.natLogicalIP).Ensure().AttachToRouter(resources.lrName).WithExternalIP("192.0.2.210").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure nat")
		must(t, sdk.OVN().NB().LoadBalancer(resources.lbName).Ensure().AttachToRouter(resources.lrName).WithVIP("192.0.2.211:80", "10.210.0.10:80").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure load balancer")
		must(t, sdk.OVN().NB().DHCPOptions(resources.dhcpCIDR).Ensure().WithOption("router", "10.210.0.1").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure dhcp options")
		must(t, sdk.OVN().NB().DNS(resources.dnsName).Ensure().WithRecord("vm.ovnflow.test", "10.210.0.10").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure dns")
		must(t, sdk.OVN().NB().QoSByMatch("from-lport", 100, resources.qosMatch).Ensure().AttachToSwitch(resources.lsName).WithRate(1000).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure qos")
		must(t, sdk.OVN().NB().Meter(resources.meterName).Ensure().WithUnit("kbps").WithNamedBand(resources.meterBandName, "drop", 100).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure meter with band")
		meterBand := requireOneRow(t, rawNB, nbDatabase, "Meter_Band", externalIDWhere(dnsNameExternalID, resources.meterBandName), []string{"_uuid", "action", "rate", "external_ids"})
		meterBandUUID := rowUUIDMust(t, meterBand, "_uuid")
		must(t, sdk.OVN().NB().PortGroup(resources.portGroupName).Ensure().WithACL("to-lport", 1001, resources.aclMatch, "allow").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure port group with acl")
		acl := requireOneRow(t, rawNB, nbDatabase, "ACL", []any{
			ovsdbjson.Condition("direction", "==", "to-lport"),
			ovsdbjson.Condition("priority", "==", 1001),
			ovsdbjson.Condition("match", "==", resources.aclMatch),
		}, []string{"_uuid", "direction", "priority", "match", "action", "external_ids"})
		aclUUID := rowUUIDMust(t, acl, "_uuid")
		must(t, sdk.OVN().NB().AddressSet(resources.addressSetName).Ensure().WithAddress("10.210.0.10").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure address set")
		must(t, sdk.OVN().NB().LogicalRouterPort(resources.lrpName).Ensure().
			AttachToRouter(resources.lrName).
			WithMAC("00:00:5e:00:53:01").
			WithNetwork("10.210.0.1/24").
			WithGatewayChassis(resources.gatewayChassisName, "gw-"+suffix, 20).
			WithHAChassisGroup(resources.haGroupName).
			WithHAChassis(resources.haChassisName, 30).
			WithExternalID(testMarkerKey, testMarkerValue).
			Execute(ctx), "ensure router port gateway and ha chain")
		gatewayChassis := requireOneRow(t, rawNB, nbDatabase, "Gateway_Chassis", nameWhere(resources.gatewayChassisName), []string{"_uuid", "name", "chassis_name", "priority", "external_ids"})
		gatewayChassisUUID := rowUUIDMust(t, gatewayChassis, "_uuid")
		haChassis := requireOneRow(t, rawNB, nbDatabase, "HA_Chassis", []any{ovsdbjson.Condition("chassis_name", "==", resources.haChassisName)}, []string{"_uuid", "chassis_name", "priority", "external_ids"})
		haChassisUUID := rowUUIDMust(t, haChassis, "_uuid")
		haGroup := requireOneRow(t, rawNB, nbDatabase, "HA_Chassis_Group", nameWhere(resources.haGroupName), []string{"_uuid", "name", "ha_chassis", "external_ids"})
		haGroupUUID := rowUUIDMust(t, haGroup, "_uuid")
		must(t, sdk.OVN().NB().BFD(resources.lrpName, resources.bfdDstIP).Ensure().WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure bfd")

		if _, err := sdk.OVN().NB().GetLogicalRouter(ctx, resources.lrName); err != nil {
			t.Fatalf("get logical router: %v", err)
		}
		assertV1NorthboundReadback(t, rawNB, resources, meterBandUUID, aclUUID, gatewayChassisUUID, haChassisUUID, haGroupUUID)
		must(t, sdk.OVN().NB().Meter(resources.meterName).Ensure().WithUnit("kbps").WithNamedBand(resources.meterBandName, "drop", 100).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure meter with band")
		must(t, sdk.OVN().NB().PortGroup(resources.portGroupName).Ensure().WithACL("to-lport", 1001, resources.aclMatch, "allow").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure port group with acl")
		must(t, sdk.OVN().NB().LogicalRouterPort(resources.lrpName).Ensure().
			AttachToRouter(resources.lrName).
			WithMAC("00:00:5e:00:53:01").
			WithNetwork("10.210.0.1/24").
			WithGatewayChassis(resources.gatewayChassisName, "gw-"+suffix, 20).
			WithHAChassisGroup(resources.haGroupName).
			WithHAChassis(resources.haChassisName, 30).
			WithExternalID(testMarkerKey, testMarkerValue).
			Execute(ctx), "repeat ensure router port gateway and ha chain")
		must(t, sdk.OVN().NB().NATByLogicalIP("snat", resources.natLogicalIP).Ensure().AttachToRouter(resources.lrName).WithExternalIP("192.0.2.210").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure nat")
		must(t, sdk.OVN().NB().LoadBalancer(resources.lbName).Ensure().AttachToRouter(resources.lrName).WithVIP("192.0.2.211:80", "10.210.0.10:80").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure load balancer")
		must(t, sdk.OVN().NB().QoSByMatch("from-lport", 100, resources.qosMatch).Ensure().AttachToSwitch(resources.lsName).WithRate(1000).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "repeat ensure qos")
		assertV1NorthboundReadback(t, rawNB, resources, meterBandUUID, aclUUID, gatewayChassisUUID, haChassisUUID, haGroupUUID)
		cleanupV1Northbound(ctx, t, sdk, rawNB, resources)
		assertV1NorthboundCleanup(t, rawNB, resources)
	})

	t.Run("southbound typed reads and watch cancel", func(t *testing.T) {
		if _, err := sdk.OVN().SB().ListChassis(ctx); err != nil {
			t.Fatalf("list chassis: %v", err)
		}
		if _, err := sdk.OVN().SB().ListPortBindings(ctx); err != nil {
			t.Fatalf("list port bindings: %v", err)
		}
		if _, err := sdk.OVN().SB().ListLogicalFlows(ctx); err != nil {
			t.Fatalf("list logical flows: %v", err)
		}
		watchCtx, cancel := context.WithCancel(context.Background())
		events, errs := sdk.OVN().SB().WatchPortBindings(watchCtx)
		cancel()
		select {
		case <-events:
		case err := <-errs:
			if err != nil && !IsKind(err, ErrorCanceled) {
				t.Fatalf("watch error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watch did not stop after cancel")
		}
	})

	t.Run("ovs extended table lifecycle", func(t *testing.T) {
		prefix := cfg.ResourcePrefix + "ovs-" + suffix + "-"
		resources := v1OVSResources{
			bridgeName:    prefix + "bridge",
			managerTarget: "ptcp:" + suffix + ":127.0.0.1",
			qosName:       prefix + "qos",
			queueName:     prefix + "queue",
			mirrorName:    prefix + "mirror",
			flowTableName: prefix + "flow-table",
			netFlowName:   prefix + "netflow",
			sFlowName:     prefix + "sflow",
			ipfixName:     prefix + "ipfix",
			autoAttach:    prefix + "auto-attach",
			suffix:        suffix,
		}
		cleanupV1OVS(ctx, t, sdk, rawOVS, resources)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupV1OVS(cleanupCtx, t, sdk, rawOVS, resources)
		})

		must(t, sdk.LocalOVS().Manager(resources.managerTarget).Ensure().WithTarget(resources.managerTarget).WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure manager")
		must(t, sdk.LocalOVS().QoS(resources.qosName).Ensure().WithQoSType("linux-htb").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure qos")
		must(t, sdk.LocalOVS().Queue(resources.queueName).Ensure().WithQueueOtherConfig("max-rate", "1000000").WithExternalID(testMarkerKey, testMarkerValue).Execute(ctx), "ensure queue")
		must(t, ensureV1OVSBridgeConfigs(ctx, sdk, resources), "ensure bridge advanced configs")

		assertV1OVSReadback(t, rawOVS, resources)
		must(t, ensureV1OVSBridgeConfigs(ctx, sdk, resources), "repeat ensure bridge advanced configs")
		assertV1OVSReadback(t, rawOVS, resources)
		cleanupV1OVS(ctx, t, sdk, rawOVS, resources)
		assertV1OVSCleanup(t, rawOVS, resources)
	})
}

func ensureV1OVSBridgeConfigs(ctx context.Context, sdk *Client, resources v1OVSResources) error {
	return sdk.LocalOVS().Bridge(resources.bridgeName).Ensure().
		WithExternalID(testMarkerKey, testMarkerValue).
		WithMirror(resources.mirrorName, func(mirror *TableBuilder) {
			mirror.WithMirrorSelectAll().WithExternalID(testMarkerKey, testMarkerValue)
		}).
		WithFlowTable(0, resources.flowTableName, func(flowTable *TableBuilder) {
			flowTable.WithExternalID(testMarkerKey, testMarkerValue)
		}).
		WithNetFlow(resources.netFlowName, func(netflow *TableBuilder) {
			netflow.WithSamplingTarget("127.0.0.1:2055").
				WithColumn("engine_type", 1).
				WithColumn("engine_id", 7).
				WithColumn("active_timeout", 30).
				WithExternalID(testMarkerKey, testMarkerValue)
		}).
		WithSFlow(resources.sFlowName, func(sflow *TableBuilder) {
			sflow.WithSamplingTarget("127.0.0.1:6343").
				WithColumn("agent", "lo").
				WithColumn("header", 128).
				WithColumn("sampling", 64).
				WithColumn("polling", 10).
				WithExternalID(testMarkerKey, testMarkerValue)
		}).
		WithIPFIX(resources.ipfixName, func(ipfix *TableBuilder) {
			ipfix.WithSamplingTarget("127.0.0.1:4739").
				WithColumn("sampling", 256).
				WithExternalID(testMarkerKey, testMarkerValue)
		}).
		WithAutoAttach(resources.autoAttach, func(autoAttach *TableBuilder) {
			autoAttach.WithColumn("system_description", "ovnflow integration auto attach").
				WithColumn("mappings", ovsIntMap(map[int]int{100: 200}))
		}).
		Execute(ctx)
}

func requireEnvOptIn(t *testing.T, name, purpose string) {
	t.Helper()
	if !parseEnvBool(os.Getenv(name)) {
		t.Skipf("%s disabled; set %s=1 to run it", purpose, name)
	}
}

func requireAnyEnvOptIn(t *testing.T, purpose string, names ...string) {
	t.Helper()
	for _, name := range names {
		if parseEnvBool(os.Getenv(name)) {
			return
		}
	}
	t.Skipf("%s disabled; set one of %v to 1", purpose, names)
}

func getIntegrationSchema(t *testing.T, client *ovsdbjson.Client, database string) libovsdb.DatabaseSchema {
	t.Helper()
	var schema libovsdb.DatabaseSchema
	if err := client.Call(testContext(t), "get_schema", []any{database}, &schema); err != nil {
		t.Fatalf("get_schema %s: %v", database, err)
	}
	return schema
}

func assertSchemaReadiness(t *testing.T, schema libovsdb.DatabaseSchema, required []integrationSchemaCheck) {
	t.Helper()
	for _, required := range required {
		t.Run(required.table, func(t *testing.T) {
			table := schema.Table(required.table)
			if table == nil {
				t.Fatalf("schema %s is missing table %s", schema.Name, required.table)
			}
			for _, column := range required.columns {
				if table.Column(column) == nil {
					t.Fatalf("schema %s is missing column %s.%s", schema.Name, required.table, column)
				}
			}
		})
	}
}

func assertSchemaReferenceReadiness(t *testing.T, database string, schema libovsdb.DatabaseSchema, required []integrationSchemaReferenceCheck) {
	t.Helper()
	registry := newSchemaRegistry(database, schema)
	for _, required := range required {
		t.Run(required.table+"."+required.column, func(t *testing.T) {
			infos := registry.ReferenceColumnInfos(required.table, required.refTable)
			for _, info := range infos {
				if info.Name != required.column {
					continue
				}
				if info.Kind != required.kind {
					t.Fatalf("%s.%s reference kind = %s, want %s", required.table, required.column, info.Kind, required.kind)
				}
				if info.KeyRef != required.keyRef || info.ValueRef != required.valueRef {
					t.Fatalf("%s.%s key/value refs = %t/%t, want %t/%t", required.table, required.column, info.KeyRef, info.ValueRef, required.keyRef, required.valueRef)
				}
				return
			}
			t.Fatalf("%s.%s does not reference %s", required.table, required.column, required.refTable)
		})
	}
}

func cleanupV1Northbound(ctx context.Context, t *testing.T, sdk *Client, raw *ovsdbjson.Client, resources v1NorthboundResources) {
	t.Helper()
	_ = sdk.OVN().NB().LogicalRouter(resources.lrName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LogicalSwitch(resources.lsName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LogicalRouterPort(resources.lrpName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().ACLByMatch("to-lport", 1001, resources.aclMatch).Delete().Execute(ctx)
	_ = sdk.OVN().NB().NATByLogicalIP("snat", resources.natLogicalIP).Delete().Execute(ctx)
	_ = sdk.OVN().NB().LoadBalancer(resources.lbName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DHCPOptions(resources.dhcpCIDR).Delete().Execute(ctx)
	_ = sdk.OVN().NB().DNS(resources.dnsName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().QoSByMatch("from-lport", 100, resources.qosMatch).Delete().Execute(ctx)
	_ = sdk.OVN().NB().Meter(resources.meterName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().MeterBand(resources.meterBandName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().PortGroup(resources.portGroupName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().AddressSet(resources.addressSetName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().GatewayChassis(resources.gatewayChassisName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().HAChassis(resources.haChassisName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().HAChassisGroup(resources.haGroupName).Delete().Execute(ctx)
	_ = sdk.OVN().NB().BFD(resources.lrpName, resources.bfdDstIP).Delete().Execute(ctx)
	_, err := raw.Transact(ctx, nbDatabase,
		map[string]any{"op": "delete", "table": "Logical_Router", "where": nameWhere(resources.lrName)},
		map[string]any{"op": "delete", "table": "Logical_Switch", "where": nameWhere(resources.lsName)},
		map[string]any{"op": "delete", "table": "Logical_Router_Port", "where": nameWhere(resources.lrpName)},
		map[string]any{"op": "delete", "table": "Load_Balancer", "where": nameWhere(resources.lbName)},
		map[string]any{"op": "delete", "table": "Address_Set", "where": nameWhere(resources.addressSetName)},
		map[string]any{"op": "delete", "table": "Meter", "where": nameWhere(resources.meterName)},
		map[string]any{"op": "delete", "table": "Meter_Band", "where": externalIDWhere(dnsNameExternalID, resources.meterBandName)},
		map[string]any{"op": "delete", "table": "Port_Group", "where": nameWhere(resources.portGroupName)},
		map[string]any{"op": "delete", "table": "Gateway_Chassis", "where": nameWhere(resources.gatewayChassisName)},
		map[string]any{"op": "delete", "table": "HA_Chassis", "where": []any{ovsdbjson.Condition("chassis_name", "==", resources.haChassisName)}},
		map[string]any{"op": "delete", "table": "HA_Chassis_Group", "where": nameWhere(resources.haGroupName)},
		map[string]any{"op": "delete", "table": "BFD", "where": []any{
			ovsdbjson.Condition("logical_port", "==", resources.lrpName),
			ovsdbjson.Condition("dst_ip", "==", resources.bfdDstIP),
		}},
	)
	if err != nil {
		t.Logf("fallback cleanup v1.0 northbound: %v", err)
	}
}

func assertV1NorthboundCleanup(t *testing.T, raw *ovsdbjson.Client, resources v1NorthboundResources) {
	t.Helper()
	checks := []struct {
		table   string
		where   []any
		columns []string
	}{
		{table: "Logical_Router", where: nameWhere(resources.lrName), columns: []string{"name"}},
		{table: "Logical_Switch", where: nameWhere(resources.lsName), columns: []string{"name"}},
		{table: "Logical_Router_Port", where: nameWhere(resources.lrpName), columns: []string{"name"}},
		{table: "ACL", where: []any{
			ovsdbjson.Condition("direction", "==", "to-lport"),
			ovsdbjson.Condition("priority", "==", 1001),
			ovsdbjson.Condition("match", "==", resources.aclMatch),
		}, columns: []string{"direction", "priority", "match"}},
		{table: "NAT", where: []any{
			ovsdbjson.Condition("type", "==", "snat"),
			ovsdbjson.Condition("logical_ip", "==", resources.natLogicalIP),
		}, columns: []string{"type", "logical_ip"}},
		{table: "Load_Balancer", where: nameWhere(resources.lbName), columns: []string{"name"}},
		{table: "DHCP_Options", where: []any{ovsdbjson.Condition("cidr", "==", resources.dhcpCIDR)}, columns: []string{"cidr"}},
		{table: "DNS", where: externalIDWhere(dnsNameExternalID, resources.dnsName), columns: []string{"external_ids"}},
		{table: "QoS", where: []any{
			ovsdbjson.Condition("direction", "==", "from-lport"),
			ovsdbjson.Condition("priority", "==", 100),
			ovsdbjson.Condition("match", "==", resources.qosMatch),
		}, columns: []string{"direction", "priority", "match"}},
		{table: "Meter", where: nameWhere(resources.meterName), columns: []string{"name"}},
		{table: "Meter_Band", where: externalIDWhere(dnsNameExternalID, resources.meterBandName), columns: []string{"external_ids"}},
		{table: "Port_Group", where: nameWhere(resources.portGroupName), columns: []string{"name"}},
		{table: "Address_Set", where: nameWhere(resources.addressSetName), columns: []string{"name"}},
		{table: "Gateway_Chassis", where: nameWhere(resources.gatewayChassisName), columns: []string{"name"}},
		{table: "HA_Chassis", where: []any{ovsdbjson.Condition("chassis_name", "==", resources.haChassisName)}, columns: []string{"chassis_name"}},
		{table: "HA_Chassis_Group", where: nameWhere(resources.haGroupName), columns: []string{"name"}},
		{table: "BFD", where: []any{
			ovsdbjson.Condition("logical_port", "==", resources.lrpName),
			ovsdbjson.Condition("dst_ip", "==", resources.bfdDstIP),
		}, columns: []string{"logical_port", "dst_ip"}},
	}
	for _, check := range checks {
		rows := selectRows(t, raw, nbDatabase, check.table, check.where, check.columns)
		if len(rows) != 0 {
			t.Fatalf("%s rows after cleanup = %d, want 0", check.table, len(rows))
		}
	}
}

func cleanupV1OVS(ctx context.Context, t *testing.T, sdk *Client, raw *ovsdbjson.Client, resources v1OVSResources) {
	t.Helper()
	_ = sdk.LocalOVS().Bridge(resources.bridgeName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().Manager(resources.managerTarget).Delete().Execute(ctx)
	_ = sdk.LocalOVS().QoS(resources.qosName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().Queue(resources.queueName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().Mirror(resources.mirrorName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().FlowTable(resources.flowTableName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().NetFlow(resources.netFlowName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().SFlow(resources.sFlowName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().IPFIX(resources.ipfixName).Delete().Execute(ctx)
	_ = sdk.LocalOVS().AutoAttach(resources.autoAttach).Delete().Execute(ctx)
	_, err := raw.Transact(ctx, ovsDatabase,
		map[string]any{"op": "delete", "table": "Bridge", "where": nameWhere(resources.bridgeName)},
		map[string]any{"op": "delete", "table": "Manager", "where": []any{ovsdbjson.Condition("target", "==", resources.managerTarget)}},
		map[string]any{"op": "delete", "table": "QoS", "where": []any{ovsdbjson.Condition("external_ids", "includes", ovsdbjson.Map(map[string]string{"name": resources.qosName}))}},
		map[string]any{"op": "delete", "table": "Queue", "where": []any{ovsdbjson.Condition("external_ids", "includes", ovsdbjson.Map(map[string]string{"name": resources.queueName}))}},
		map[string]any{"op": "delete", "table": "Mirror", "where": nameWhere(resources.mirrorName)},
		map[string]any{"op": "delete", "table": "Flow_Table", "where": nameWhere(resources.flowTableName)},
		map[string]any{"op": "delete", "table": "NetFlow", "where": externalIDWhere("name", resources.netFlowName)},
		map[string]any{"op": "delete", "table": "sFlow", "where": externalIDWhere("name", resources.sFlowName)},
		map[string]any{"op": "delete", "table": "IPFIX", "where": externalIDWhere("name", resources.ipfixName)},
		map[string]any{"op": "delete", "table": "AutoAttach", "where": []any{ovsdbjson.Condition("system_name", "==", resources.autoAttach)}},
	)
	if err != nil {
		t.Logf("fallback cleanup v1.0 OVS: %v", err)
	}
}

func assertV1OVSCleanup(t *testing.T, raw *ovsdbjson.Client, resources v1OVSResources) {
	t.Helper()
	checks := []struct {
		table   string
		where   []any
		columns []string
	}{
		{table: "Bridge", where: nameWhere(resources.bridgeName), columns: []string{"name"}},
		{table: "Manager", where: []any{ovsdbjson.Condition("target", "==", resources.managerTarget)}, columns: []string{"target"}},
		{table: "QoS", where: externalIDWhere("name", resources.qosName), columns: []string{"external_ids"}},
		{table: "Queue", where: externalIDWhere("name", resources.queueName), columns: []string{"external_ids"}},
		{table: "Mirror", where: nameWhere(resources.mirrorName), columns: []string{"name"}},
		{table: "Flow_Table", where: nameWhere(resources.flowTableName), columns: []string{"name"}},
		{table: "NetFlow", where: externalIDWhere("name", resources.netFlowName), columns: []string{"external_ids"}},
		{table: "sFlow", where: externalIDWhere("name", resources.sFlowName), columns: []string{"external_ids"}},
		{table: "IPFIX", where: externalIDWhere("name", resources.ipfixName), columns: []string{"external_ids"}},
		{table: "AutoAttach", where: []any{ovsdbjson.Condition("system_name", "==", resources.autoAttach)}, columns: []string{"system_name"}},
	}
	for _, check := range checks {
		rows := selectRows(t, raw, ovsDatabase, check.table, check.where, check.columns)
		if len(rows) != 0 {
			t.Fatalf("%s rows after cleanup = %d, want 0", check.table, len(rows))
		}
	}
}

func must(t *testing.T, err error, action string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

func assertV1NorthboundReadback(t *testing.T, raw *ovsdbjson.Client, resources v1NorthboundResources, meterBandUUID, aclUUID, gatewayChassisUUID, haChassisUUID, haGroupUUID string) {
	t.Helper()

	lr := requireOneRow(t, raw, nbDatabase, "Logical_Router", nameWhere(resources.lrName), []string{"name", "external_ids"})
	requireString(t, lr, "name", resources.lrName)
	requireStringMapValue(t, lr, "external_ids", testMarkerKey, testMarkerValue)

	ls := requireOneRow(t, raw, nbDatabase, "Logical_Switch", nameWhere(resources.lsName), []string{"name", "external_ids"})
	requireString(t, ls, "name", resources.lsName)
	requireStringMapValue(t, ls, "external_ids", testMarkerKey, testMarkerValue)

	lrp := requireOneRow(t, raw, nbDatabase, "Logical_Router_Port", nameWhere(resources.lrpName), []string{"_uuid", "name", "mac", "networks", "external_ids"})
	lrpUUID := rowUUIDMust(t, lrp, "_uuid")
	requireString(t, lrp, "mac", "00:00:5e:00:53:01")
	requireStringSetContains(t, lrp, "networks", "10.210.0.1/24")
	requireStringMapValue(t, lrp, "external_ids", testMarkerKey, testMarkerValue)
	lrRefs := requireOneRow(t, raw, nbDatabase, "Logical_Router", nameWhere(resources.lrName), []string{"ports"})
	if !rowUUIDSetContains(t, lrRefs, "ports", lrpUUID) {
		t.Fatalf("Logical_Router ports does not reference Logical_Router_Port")
	}

	acl := requireOneRow(t, raw, nbDatabase, "ACL", []any{
		ovsdbjson.Condition("direction", "==", "to-lport"),
		ovsdbjson.Condition("priority", "==", 1001),
		ovsdbjson.Condition("match", "==", resources.aclMatch),
	}, []string{"direction", "priority", "match", "action", "external_ids"})
	requireString(t, acl, "action", "allow")
	requireStringMapValue(t, acl, "external_ids", testMarkerKey, testMarkerValue)

	nat := requireOneRow(t, raw, nbDatabase, "NAT", []any{
		ovsdbjson.Condition("type", "==", "snat"),
		ovsdbjson.Condition("logical_ip", "==", resources.natLogicalIP),
	}, []string{"_uuid", "type", "logical_ip", "external_ip", "external_ids"})
	natUUID := rowUUIDMust(t, nat, "_uuid")
	requireString(t, nat, "external_ip", "192.0.2.210")
	requireStringMapValue(t, nat, "external_ids", testMarkerKey, testMarkerValue)

	lb := requireOneRow(t, raw, nbDatabase, "Load_Balancer", nameWhere(resources.lbName), []string{"_uuid", "name", "vips", "external_ids"})
	lbUUID := rowUUIDMust(t, lb, "_uuid")
	requireStringMapValue(t, lb, "vips", "192.0.2.211:80", "10.210.0.10:80")
	requireStringMapValue(t, lb, "external_ids", testMarkerKey, testMarkerValue)
	lrServiceRefs := requireOneRow(t, raw, nbDatabase, "Logical_Router", nameWhere(resources.lrName), []string{"nat", "load_balancer"})
	if !rowUUIDSetContains(t, lrServiceRefs, "nat", natUUID) {
		t.Fatalf("Logical_Router nat does not reference created NAT")
	}
	if !rowUUIDSetContains(t, lrServiceRefs, "load_balancer", lbUUID) {
		t.Fatalf("Logical_Router load_balancer does not reference created Load_Balancer")
	}

	dhcp := requireOneRow(t, raw, nbDatabase, "DHCP_Options", []any{ovsdbjson.Condition("cidr", "==", resources.dhcpCIDR)}, []string{"cidr", "options", "external_ids"})
	requireStringMapValue(t, dhcp, "options", "router", "10.210.0.1")
	requireStringMapValue(t, dhcp, "external_ids", testMarkerKey, testMarkerValue)

	dns := requireOneRow(t, raw, nbDatabase, "DNS", externalIDWhere(dnsNameExternalID, resources.dnsName), []string{"records", "external_ids"})
	requireStringMapValue(t, dns, "records", "vm.ovnflow.test", "10.210.0.10")
	requireStringMapValue(t, dns, "external_ids", testMarkerKey, testMarkerValue)

	qos := requireOneRow(t, raw, nbDatabase, "QoS", []any{
		ovsdbjson.Condition("direction", "==", "from-lport"),
		ovsdbjson.Condition("priority", "==", 100),
		ovsdbjson.Condition("match", "==", resources.qosMatch),
	}, []string{"_uuid", "direction", "priority", "match", "bandwidth", "external_ids"})
	qosUUID := rowUUIDMust(t, qos, "_uuid")
	requireIntMapValue(t, qos, "bandwidth", "rate", 1000)
	requireStringMapValue(t, qos, "external_ids", testMarkerKey, testMarkerValue)
	lsRefs := requireOneRow(t, raw, nbDatabase, "Logical_Switch", nameWhere(resources.lsName), []string{"qos_rules"})
	if !rowUUIDSetContains(t, lsRefs, "qos_rules", qosUUID) {
		t.Fatalf("Logical_Switch qos_rules does not reference created QoS")
	}

	meter := requireOneRow(t, raw, nbDatabase, "Meter", nameWhere(resources.meterName), []string{"name", "unit", "bands", "external_ids"})
	requireString(t, meter, "unit", "kbps")
	if !rowUUIDSetContains(t, meter, "bands", meterBandUUID) {
		t.Fatalf("Meter bands does not reference created Meter_Band")
	}
	requireStringMapValue(t, meter, "external_ids", testMarkerKey, testMarkerValue)

	meterBand := requireOneRow(t, raw, nbDatabase, "Meter_Band", externalIDWhere(dnsNameExternalID, resources.meterBandName), []string{"action", "rate", "external_ids"})
	requireString(t, meterBand, "action", "drop")
	requireInt(t, meterBand, "rate", 100)
	requireStringMapValue(t, meterBand, "external_ids", testMarkerKey, testMarkerValue)

	portGroup := requireOneRow(t, raw, nbDatabase, "Port_Group", nameWhere(resources.portGroupName), []string{"name", "acls", "external_ids"})
	if !rowUUIDSetContains(t, portGroup, "acls", aclUUID) {
		t.Fatalf("Port_Group ACLs does not reference created ACL")
	}
	requireStringMapValue(t, portGroup, "external_ids", testMarkerKey, testMarkerValue)

	addressSet := requireOneRow(t, raw, nbDatabase, "Address_Set", nameWhere(resources.addressSetName), []string{"name", "addresses", "external_ids"})
	requireStringSetContains(t, addressSet, "addresses", "10.210.0.10")
	requireStringMapValue(t, addressSet, "external_ids", testMarkerKey, testMarkerValue)

	gatewayChassis := requireOneRow(t, raw, nbDatabase, "Gateway_Chassis", nameWhere(resources.gatewayChassisName), []string{"name", "chassis_name", "priority", "external_ids"})
	requireString(t, gatewayChassis, "chassis_name", "gw-"+resources.suffix)
	requireInt(t, gatewayChassis, "priority", 20)
	requireStringMapValue(t, gatewayChassis, "external_ids", testMarkerKey, testMarkerValue)

	haChassis := requireOneRow(t, raw, nbDatabase, "HA_Chassis", []any{ovsdbjson.Condition("chassis_name", "==", resources.haChassisName)}, []string{"chassis_name", "priority", "external_ids"})
	requireInt(t, haChassis, "priority", 30)
	requireStringMapValue(t, haChassis, "external_ids", testMarkerKey, testMarkerValue)

	haGroup := requireOneRow(t, raw, nbDatabase, "HA_Chassis_Group", nameWhere(resources.haGroupName), []string{"name", "ha_chassis", "external_ids"})
	if !rowUUIDSetContains(t, haGroup, "ha_chassis", haChassisUUID) {
		t.Fatalf("HA_Chassis_Group does not reference created HA_Chassis")
	}
	requireStringMapValue(t, haGroup, "external_ids", testMarkerKey, testMarkerValue)

	lrpRefs := requireOneRow(t, raw, nbDatabase, "Logical_Router_Port", nameWhere(resources.lrpName), []string{"gateway_chassis", "ha_chassis_group"})
	if !rowUUIDSetContains(t, lrpRefs, "gateway_chassis", gatewayChassisUUID) {
		t.Fatalf("Logical_Router_Port gateway_chassis does not reference created Gateway_Chassis")
	}
	if got := rowUUIDOptional(t, lrpRefs, "ha_chassis_group"); got != haGroupUUID {
		t.Fatalf("Logical_Router_Port ha_chassis_group = %q, want %q", got, haGroupUUID)
	}

	bfd := requireOneRow(t, raw, nbDatabase, "BFD", []any{
		ovsdbjson.Condition("logical_port", "==", resources.lrpName),
		ovsdbjson.Condition("dst_ip", "==", resources.bfdDstIP),
	}, []string{"logical_port", "dst_ip", "external_ids"})
	requireStringMapValue(t, bfd, "external_ids", testMarkerKey, testMarkerValue)
}

func assertV1OVSReadback(t *testing.T, raw *ovsdbjson.Client, resources v1OVSResources) {
	t.Helper()

	manager := requireOneRow(t, raw, ovsDatabase, "Manager", []any{ovsdbjson.Condition("target", "==", resources.managerTarget)}, []string{"target", "external_ids"})
	requireString(t, manager, "target", resources.managerTarget)
	requireStringMapValue(t, manager, "external_ids", testMarkerKey, testMarkerValue)

	qos := requireOneRow(t, raw, ovsDatabase, "QoS", externalIDWhere("name", resources.qosName), []string{"type", "external_ids"})
	requireString(t, qos, "type", "linux-htb")
	requireStringMapValue(t, qos, "external_ids", "name", resources.qosName)
	requireStringMapValue(t, qos, "external_ids", testMarkerKey, testMarkerValue)

	queue := requireOneRow(t, raw, ovsDatabase, "Queue", externalIDWhere("name", resources.queueName), []string{"other_config", "external_ids"})
	requireStringMapValue(t, queue, "other_config", "max-rate", "1000000")
	requireStringMapValue(t, queue, "external_ids", "name", resources.queueName)
	requireStringMapValue(t, queue, "external_ids", testMarkerKey, testMarkerValue)

	bridge := requireOneRow(t, raw, ovsDatabase, "Bridge", nameWhere(resources.bridgeName), []string{"mirrors", "flow_tables", "netflow", "sflow", "ipfix", "auto_attach", "external_ids"})
	requireStringMapValue(t, bridge, "external_ids", testMarkerKey, testMarkerValue)

	mirror := requireOneRow(t, raw, ovsDatabase, "Mirror", nameWhere(resources.mirrorName), []string{"_uuid", "name", "select_all", "external_ids"})
	mirrorUUID := rowUUIDMust(t, mirror, "_uuid")
	requireString(t, mirror, "name", resources.mirrorName)
	requireBool(t, mirror, "select_all", true)
	requireStringMapValue(t, mirror, "external_ids", testMarkerKey, testMarkerValue)
	if !rowUUIDSetContains(t, bridge, "mirrors", mirrorUUID) {
		t.Fatalf("Bridge mirrors does not reference created Mirror")
	}

	flowTable := requireOneRow(t, raw, ovsDatabase, "Flow_Table", nameWhere(resources.flowTableName), []string{"_uuid", "name", "external_ids"})
	flowTableUUID := rowUUIDMust(t, flowTable, "_uuid")
	requireString(t, flowTable, "name", resources.flowTableName)
	requireStringMapValue(t, flowTable, "external_ids", testMarkerKey, testMarkerValue)
	requireUUIDIntMapValue(t, bridge, "flow_tables", 0, flowTableUUID)

	netflow := requireOneRow(t, raw, ovsDatabase, "NetFlow", externalIDWhere("name", resources.netFlowName), []string{"_uuid", "targets", "engine_type", "engine_id", "active_timeout", "external_ids"})
	netflowUUID := rowUUIDMust(t, netflow, "_uuid")
	requireStringSetContains(t, netflow, "targets", "127.0.0.1:2055")
	requireInt(t, netflow, "engine_type", 1)
	requireInt(t, netflow, "engine_id", 7)
	requireInt(t, netflow, "active_timeout", 30)
	requireStringMapValue(t, netflow, "external_ids", "name", resources.netFlowName)
	requireStringMapValue(t, netflow, "external_ids", testMarkerKey, testMarkerValue)
	if got := rowUUIDOptional(t, bridge, "netflow"); got != netflowUUID {
		t.Fatalf("Bridge netflow = %q, want %q", got, netflowUUID)
	}

	sflow := requireOneRow(t, raw, ovsDatabase, "sFlow", externalIDWhere("name", resources.sFlowName), []string{"_uuid", "agent", "targets", "header", "sampling", "polling", "external_ids"})
	sflowUUID := rowUUIDMust(t, sflow, "_uuid")
	requireString(t, sflow, "agent", "lo")
	requireStringSetContains(t, sflow, "targets", "127.0.0.1:6343")
	requireInt(t, sflow, "header", 128)
	requireInt(t, sflow, "sampling", 64)
	requireInt(t, sflow, "polling", 10)
	requireStringMapValue(t, sflow, "external_ids", "name", resources.sFlowName)
	requireStringMapValue(t, sflow, "external_ids", testMarkerKey, testMarkerValue)
	if got := rowUUIDOptional(t, bridge, "sflow"); got != sflowUUID {
		t.Fatalf("Bridge sflow = %q, want %q", got, sflowUUID)
	}

	ipfix := requireOneRow(t, raw, ovsDatabase, "IPFIX", externalIDWhere("name", resources.ipfixName), []string{"_uuid", "targets", "sampling", "external_ids"})
	ipfixUUID := rowUUIDMust(t, ipfix, "_uuid")
	requireStringSetContains(t, ipfix, "targets", "127.0.0.1:4739")
	requireInt(t, ipfix, "sampling", 256)
	requireStringMapValue(t, ipfix, "external_ids", "name", resources.ipfixName)
	requireStringMapValue(t, ipfix, "external_ids", testMarkerKey, testMarkerValue)
	if got := rowUUIDOptional(t, bridge, "ipfix"); got != ipfixUUID {
		t.Fatalf("Bridge ipfix = %q, want %q", got, ipfixUUID)
	}

	autoAttach := requireOneRow(t, raw, ovsDatabase, "AutoAttach", []any{ovsdbjson.Condition("system_name", "==", resources.autoAttach)}, []string{"_uuid", "system_name", "system_description", "mappings"})
	autoAttachUUID := rowUUIDMust(t, autoAttach, "_uuid")
	requireString(t, autoAttach, "system_name", resources.autoAttach)
	requireString(t, autoAttach, "system_description", "ovnflow integration auto attach")
	requireIntegerMapValue(t, autoAttach, "mappings", 100, 200)
	if got := rowUUIDOptional(t, bridge, "auto_attach"); got != autoAttachUUID {
		t.Fatalf("Bridge auto_attach = %q, want %q", got, autoAttachUUID)
	}
}

func requireOneRow(t *testing.T, client *ovsdbjson.Client, database, table string, where []any, columns []string) map[string]json.RawMessage {
	t.Helper()
	rows := selectRows(t, client, database, table, where, columns)
	if len(rows) != 1 {
		t.Fatalf("%s rows = %d, want 1", table, len(rows))
	}
	return rows[0]
}

func externalIDWhere(key, value string) []any {
	return []any{ovsdbjson.Condition("external_ids", "includes", ovsdbjson.Map(map[string]string{key: value}))}
}

func requireString(t *testing.T, row map[string]json.RawMessage, column, want string) {
	t.Helper()
	if got := rowString(t, row, column); got != want {
		t.Fatalf("%s = %q, want %q", column, got, want)
	}
}

func requireStringMapValue(t *testing.T, row map[string]json.RawMessage, column, key, want string) {
	t.Helper()
	if got := rowStringMap(t, row, column)[key]; got != want {
		t.Fatalf("%s[%q] = %q, want %q", column, key, got, want)
	}
}

func requireBool(t *testing.T, row map[string]json.RawMessage, column string, want bool) {
	t.Helper()
	if got := rowBool(t, row, column); got != want {
		t.Fatalf("%s = %t, want %t", column, got, want)
	}
}

func requireInt(t *testing.T, row map[string]json.RawMessage, column string, want int) {
	t.Helper()
	if got := rowInt(t, row, column); got != want {
		t.Fatalf("%s = %d, want %d", column, got, want)
	}
}

func requireIntMapValue(t *testing.T, row map[string]json.RawMessage, column, key string, want int) {
	t.Helper()
	if got := rowIntMap(t, row, column)[key]; got != want {
		t.Fatalf("%s[%q] = %d, want %d", column, key, got, want)
	}
}

func requireIntegerMapValue(t *testing.T, row map[string]json.RawMessage, column string, key, want int) {
	t.Helper()
	if got := rowIntegerMap(t, row, column)[key]; got != want {
		t.Fatalf("%s[%d] = %d, want %d", column, key, got, want)
	}
}

func requireUUIDIntMapValue(t *testing.T, row map[string]json.RawMessage, column string, key int, want string) {
	t.Helper()
	if got := rowIntUUIDMap(t, row, column)[key]; got != want {
		t.Fatalf("%s[%d] = %q, want %q", column, key, got, want)
	}
}

func requireStringSetContains(t *testing.T, row map[string]json.RawMessage, column, want string) {
	t.Helper()
	for _, got := range rowStringSetValuesFromColumn(t, row, column) {
		if got == want {
			return
		}
	}
	t.Fatalf("%s does not contain %q", column, want)
}

func rowBool(t *testing.T, row map[string]json.RawMessage, column string) bool {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		t.Fatalf("row is missing column %q", column)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s bool: %v", column, err)
	}
	return value
}

func rowIntegerMap(t *testing.T, row map[string]json.RawMessage, column string) map[int]int {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return map[int]int{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s map: %v", column, err)
	}
	result := map[int]int{}
	outer, ok := value.([]any)
	if !ok || len(outer) != 2 || outer[0] != "map" {
		t.Fatalf("column %s is not an OVSDB map: %s", column, string(raw))
	}
	pairs, ok := outer[1].([]any)
	if !ok {
		t.Fatalf("column %s has invalid OVSDB map pairs: %s", column, string(raw))
	}
	for _, pairValue := range pairs {
		pair, ok := pairValue.([]any)
		if !ok || len(pair) != 2 {
			t.Fatalf("column %s has invalid OVSDB map pair: %v", column, pairValue)
		}
		key, keyOK := jsonNumberAsInt(pair[0])
		val, valOK := jsonNumberAsInt(pair[1])
		if !keyOK || !valOK {
			t.Fatalf("column %s map pair is not int:int: %v", column, pair)
		}
		result[key] = val
	}
	return result
}

func rowIntUUIDMap(t *testing.T, row map[string]json.RawMessage, column string) map[int]string {
	t.Helper()
	raw, ok := row[column]
	if !ok {
		return map[int]string{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s map: %v", column, err)
	}
	result := map[int]string{}
	outer, ok := value.([]any)
	if !ok || len(outer) != 2 || outer[0] != "map" {
		t.Fatalf("column %s is not an OVSDB map: %s", column, string(raw))
	}
	pairs, ok := outer[1].([]any)
	if !ok {
		t.Fatalf("column %s has invalid OVSDB map pairs: %s", column, string(raw))
	}
	for _, pairValue := range pairs {
		pair, ok := pairValue.([]any)
		if !ok || len(pair) != 2 {
			t.Fatalf("column %s has invalid OVSDB map pair: %v", column, pairValue)
		}
		key, keyOK := jsonNumberAsInt(pair[0])
		val, valOK := jsonUUIDAsString(pair[1])
		if !keyOK || !valOK {
			t.Fatalf("column %s map pair is not int:uuid: %v", column, pair)
		}
		result[key] = val
	}
	return result
}

func jsonNumberAsInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func jsonUUIDAsString(value any) (string, bool) {
	typed, ok := value.([]any)
	if !ok || len(typed) != 2 {
		return "", false
	}
	if marker, ok := typed[0].(string); !ok || (marker != "uuid" && marker != "named-uuid") {
		return "", false
	}
	id, ok := typed[1].(string)
	return id, ok && id != ""
}
