package sourcestream

import "runtime"

// Builtin protocol descriptions are code-owned configuration contracts, not
// installable objects. Endpoint examples are placeholders, never test devices.
func Protocols() map[string]any {
	list := []map[string]any{
		{"id": OpenProtocol, "name": "Atlas Copco Open Protocol", "runtime": "go", "location": "local", "roles": []ConnectionRole{ClientRole}, "models": []AccessModel{}, "credentialKinds": []string{}, "document": SourceDefinition{Protocol: OpenProtocol, RuntimeLocation: "local", OpenProtocol: &OpenProtocolOptions{}}},
		{"id": FileRead, "name": "File reader", "runtime": "go", "location": "local", "roles": []ConnectionRole{ClientRole}, "models": []AccessModel{}, "credentialKinds": []string{"basic", "bearer", "custom"}, "credentialSchemes": map[string][]string{"http": {}, "https": {"basic", "bearer"}, "smb": {"basic"}, "sftp": {"basic", "custom"}}, "document": SourceDefinition{Protocol: FileRead, RuntimeLocation: "local", File: &FileSourceOptions{Resources: []FileSelection{}}}},
		{"id": EdgeSQL, "name": "Read-only SQL queries", "runtime": "go", "location": "local", "roles": []ConnectionRole{ClientRole}, "models": []AccessModel{}, "credentialKinds": []string{"basic"}, "document": SourceDefinition{Protocol: EdgeSQL, RuntimeLocation: "local", SQL: &SQLConnection{}}},
		{"id": HTTP, "name": "HTTP API", "runtime": "go", "location": "local", "roles": []ConnectionRole{ClientRole, ServerRole}, "models": []AccessModel{}, "credentialKinds": []string{"basic", "bearer", "custom", "http"}, "document": SourceDefinition{Protocol: HTTP, RuntimeLocation: "local", HTTP: &HTTPConnection{Authentication: "none"}}},
		{"id": OAuth, "name": "OAuth 2.0", "runtime": "go", "location": "local", "models": []AccessModel{}, "credentialKinds": []string{"oauth2"}, "document": SourceDefinition{Protocol: OAuth, RuntimeLocation: "local", HTTP: &HTTPConnection{OAuth: &OAuthConfig{ClientAuthentication: "client_secret_basic"}}}},
		{"id": MQTT, "name": "MQTT", "runtime": "go", "location": "local", "roles": []ConnectionRole{ClientRole, ServerRole}, "models": []AccessModel{Publish, Subscribe, RequestReply}, "requestReplyRequiresCorrelation": true, "credentialKinds": []string{"basic", "custom"},
			"mutualTLS": map[string]any{"role": ClientRole, "scheme": "mqtts", "credentialKind": "custom", "requiredValueKeys": []string{"clientCertificatePem", "clientPrivateKeyPem", "caCertificatePem"}},
			"document":  SourceDefinition{Protocol: MQTT, RuntimeLocation: "local", MQTT: &MQTTOptions{QoS: 1, TopicFilters: []string{}}}},
		{"id": OPCUA, "name": "OPC UA", "runtime": "node", "location": "local", "roles": []ConnectionRole{ClientRole, ServerRole}, "models": []AccessModel{Publish, Subscribe, RequestReply}, "credentialKinds": []string{"basic"}, "serverSecurityModes": []string{"None", "SignAndEncrypt"}, "serverDataTypes": []string{"Double", "Int32", "Boolean", "String"}, "document": SourceDefinition{Protocol: OPCUA, RuntimeLocation: "local", OPCUA: &OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 1000, NodeIDs: []string{}}}},
		{"id": ModbusTCP, "name": "Modbus TCP", "runtime": "go", "location": "local", "models": []AccessModel{Publish, Subscribe, RequestReply}, "credentialKinds": []string{}, "document": SourceDefinition{Protocol: ModbusTCP, RuntimeLocation: "local", Modbus: &ModbusOptions{UnitID: 1, PollIntervalMs: 1000, Ranges: []ModbusRange{}}}},
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		list = append(list, map[string]any{"id": USBHID, "name": "USB HID text input", "runtime": "native-hid", "location": "local", "roles": []ConnectionRole{ClientRole}, "models": []AccessModel{Publish}, "credentialKinds": []string{}, "document": SourceDefinition{Protocol: USBHID, RuntimeLocation: "local"}})
	}
	return map[string]any{"list": list, "sourceTypes": VendorProfiles(), "schemaRevision": 3, "externalRuntimeSupported": false}
}
