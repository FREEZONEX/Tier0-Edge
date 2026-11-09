package unsv2

import (
	"backend/internal/domain/sourcestream"
	"encoding/json"
	"fmt"
)

func sourceProtocolCatalog() map[string]any {
	out := sourcestream.Protocols()
	var contract struct {
		Schemas map[string]any `json:"schemas"`
	}
	if json.Unmarshal([]byte(ContractJSON), &contract) != nil {
		return nil
	}
	schemas := map[string]any{}
	var visit func(string)
	visit = func(name string) {
		if _, ok := schemas[name]; ok {
			return
		}
		schema, ok := contract.Schemas[name]
		if !ok {
			return
		}
		schemas[name] = schema
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if reference, ok := x["$ref"].(string); ok {
					const prefix = "#/components/schemas/"
					if len(reference) > len(prefix) && reference[:len(prefix)] == prefix {
						visit(reference[len(prefix):])
					}
				}
				for _, child := range x {
					walk(child)
				}
			case []any:
				for _, child := range x {
					walk(child)
				}
			}
		}
		walk(schema)
	}
	visit("SourceDefinition")
	visit("SourceCredentials")
	out["schemas"] = schemas
	for _, item := range out["list"].([]map[string]any) {
		protocol := fmt.Sprint(item["id"])
		item["protocol"] = protocol
		item["supported"] = true
		item["stateManagementSupported"] = true
		item["runtimeLocation"] = "local"
		item["documentSchema"] = map[string]string{"$ref": "#/schemas/SourceDefinition"}
		item["credentialsSchema"] = map[string]string{"$ref": "#/schemas/SourceCredentials"}
		// Advertise the protocol's real entry modes. Per-resource grants and
		// writable operations still come from the selected Source catalog.
		switch sourcestream.Protocol(protocol) {
		case sourcestream.HTTP:
			item["operations"] = []string{"health", "request", "receive"}
		case sourcestream.OAuth:
			item["operations"] = []string{"health", "request"}
		case sourcestream.EdgeSQL, sourcestream.FileRead:
			item["operations"] = []string{"health", "read", "scan"}
		case sourcestream.OpenProtocol:
			item["operations"] = []string{"health", "read", "scan", "receive"}
		case sourcestream.USBHID:
			item["operations"] = []string{"health", "receive"}
		default:
			item["operations"] = []string{"health", "read", "receive"}
		}
		item["sourceBindingsAcquisition"] = true
		fields := []string{"resourceRefs"}
		switch sourcestream.Protocol(protocol) {
		case sourcestream.ModbusTCP:
			fields = append(fields, "ranges", "pollIntervalMs")
		case sourcestream.MQTT:
			fields = append(fields, "topicFilters", "qos")
		case sourcestream.OPCUA:
			fields = append(fields, "nodeIds", "samplingIntervalMs")
		}
		item["sourceBindingsAcquisitionFields"] = fields
		item["remoteStreamTarget"] = false
		if protocol == "edge-sql" {
			item["queryKinds"] = []string{"fixed", "named"}
			item["namedQuerySupported"] = true
			item["namedQueryDialects"] = []string{"postgresql", "mysql", "sqlserver"}
			item["namedQueryPreviewDialects"] = []string{"postgresql", "mysql"}
			item["fixedQueryDialects"] = []string{"postgresql", "sqlserver"}
		}
		item["tlsReferencesSupported"] = protocol == "http-api" || protocol == "oauth2" || protocol == "mqtt" || protocol == "opcua" || protocol == "edge-sql" || protocol == "file-read"
		item["tlsReferenceScope"] = "source"
		item["tlsReferenceProvisioning"] = "create.credentials.values"
		item["ingressKinds"] = []string{}
	}
	// Vendor profiles retain their real parameter/credential declarations.
	encoded, _ := json.Marshal(out["sourceTypes"])
	var profiles []map[string]any
	if json.Unmarshal(encoded, &profiles) == nil {
		for _, profile := range profiles {
			profile["documentTemplate"] = profile["document"]
			if config, ok := profile["configSchema"].(map[string]any); ok {
				if properties, ok := config["properties"].(map[string]any); ok {
					if http, ok := properties["http"].(map[string]any); ok {
						if fields, ok := http["properties"].(map[string]any); ok {
							profile["parametersSchema"] = fields["parameters"]
						}
					}
				}
			}
			delete(profile, "configSchema")
			profile["documentSchema"] = map[string]string{"$ref": "#/schemas/SourceDefinition"}
			profile["credentialsSchema"] = map[string]string{"$ref": "#/schemas/SourceCredentials"}
		}
		out["sourceTypes"] = profiles
	}
	return out
}
