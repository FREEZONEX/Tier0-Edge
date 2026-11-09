package sourcestream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func managedOPCUASource() SourceDefinition {
	d := opcuaSource()
	d.Role, d.Endpoint, d.CredentialRef = ServerRole, "opc.tcp://managed", "credential-test"
	d.OPCUA.NodeIDs = []string{"temperature", "report"}
	d.OPCUA.ServerNodes = []OPCUAServerNode{
		{ID: "temperature", Name: "温度", NodeClass: "Variable", DataType: "Double", Direction: "ingress"},
		{ID: "report", Name: "上报", NodeClass: "Method", DataType: "String", Direction: "ingress"},
	}
	return d
}

func TestOPCUAServerDefinition(t *testing.T) {
	require.NoError(t, managedOPCUASource().Validate())
	for name, change := range map[string]func(*SourceDefinition){
		"arbitrary listener": func(d *SourceDefinition) { d.Endpoint = "opc.tcp://0.0.0.0:4840" },
		"missing nodes":      func(d *SourceDefinition) { d.OPCUA.ServerNodes = nil },
		"duplicate nodes":    func(d *SourceDefinition) { d.OPCUA.ServerNodes = append(d.OPCUA.ServerNodes, d.OPCUA.ServerNodes[0]) },
		"foreign node":       func(d *SourceDefinition) { d.OPCUA.NodeIDs = []string{"another-source"} },
		"raw address":        func(d *SourceDefinition) { d.OPCUA.ServerNodes[0].ID = "ns=2;s=foreign" },
		"empty display name": func(d *SourceDefinition) { d.OPCUA.ServerNodes[0].Name = " " },
		"unsupported type":   func(d *SourceDefinition) { d.OPCUA.ServerNodes[0].DataType = "ExtensionObject" },
		"unsupported class":  func(d *SourceDefinition) { d.OPCUA.ServerNodes[0].NodeClass = "Object" },
		"downlink method":    func(d *SourceDefinition) { d.OPCUA.ServerNodes[1].Direction = "egress" },
		"non JSON method":    func(d *SourceDefinition) { d.OPCUA.ServerNodes[1].DataType = "Double" },
		"sign only":          func(d *SourceDefinition) { d.OPCUA.SecurityMode = "Sign" },
		"unsupported policy": func(d *SourceDefinition) { d.OPCUA.SecurityPolicy = "Aes256_Sha256_RsaPss" },
		"client declaration": func(d *SourceDefinition) { d.Role = ClientRole; d.Endpoint = "opc.tcp://external:4840" },
	} {
		t.Run(name, func(t *testing.T) { d := managedOPCUASource(); change(&d); require.Error(t, d.Validate()) })
	}
	d := managedOPCUASource()
	d.OPCUA.NodeIDs = nil // Resources may exist without an ingress Stream.
	require.NoError(t, d.Validate())
	d.OPCUA.SecurityMode, d.OPCUA.SecurityPolicy = "None", "None"
	require.NoError(t, d.Validate())
}
