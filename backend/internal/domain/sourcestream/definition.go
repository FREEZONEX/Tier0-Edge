// Package sourcestream defines protocol connections and independent UNS mappings.
// It does not reuse Node-RED flows or expose a separate driver registry.
package sourcestream

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"backend/internal/domain/common"
	"backend/internal/domain/unsmeta"
	"backend/internal/sourceresource"
	"backend/internal/streamexec"
)

type Protocol string
type AccessModel = sourceresource.AccessModel
type ConnectionRole string

const (
	ClientRole ConnectionRole = "client"
	ServerRole ConnectionRole = "server"
)

const (
	HTTP               Protocol    = "http-api"
	OAuth              Protocol    = "oauth2"
	MQTT               Protocol    = "mqtt"
	OPCUA              Protocol    = "opcua"
	ModbusTCP          Protocol    = "modbus-tcp"
	USBHID             Protocol    = "usb-hid"
	HIDTextResource                = "usb-hid:text"
	Publish            AccessModel = sourceresource.Publish
	Subscribe          AccessModel = sourceresource.Subscribe
	RequestReply       AccessModel = sourceresource.RequestReply
	ProgramAccess      AccessModel = "program"
	MaxScriptBytes                 = 128 * 1024
	MaxScriptTimeoutMs             = 30000
)

// ValidationError uses stable field/code values; HTTP and localization belong
// to the API boundary. Never include connection values or credentials in errors.
type ValidationError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

func invalid(field, code string) error { return &ValidationError{Field: field, Code: code} }

type MQTTOptions struct {
	CACertificate         string          `json:"caCertificate,omitempty"`
	PacketEnvelope        bool            `json:"packetEnvelope,omitempty"`
	Ephemeral             bool            `json:"-"`
	ClientID              string          `json:"clientId"`
	QoS                   int             `json:"qos,omitempty"`
	Subscriptions         map[string]byte `json:"subscriptions,omitempty"`
	TopicFilters          []string        `json:"topicFilters,omitempty"`
	RequestTopic          string          `json:"requestTopic,omitempty"`
	ReplyTopic            string          `json:"replyTopic,omitempty"`
	ReplyTopicField       string          `json:"replyTopicField,omitempty"`
	CorrelationField      string          `json:"correlationField,omitempty"`
	ServerSubscribeTopics []string        `json:"serverSubscribeTopics,omitempty"`
}
type OPCUAOptions struct {
	ServerCertificate  string            `json:"serverCertificate,omitempty"`
	SecurityMode       string            `json:"securityMode"`
	SecurityPolicy     string            `json:"securityPolicy"`
	NodeIDs            []string          `json:"nodeIds,omitempty"`
	SamplingIntervalMs int               `json:"samplingIntervalMs,omitempty"`
	ServerNodes        []OPCUAServerNode `json:"serverNodes,omitempty"`
}
type OPCUAServerNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	NodeClass string `json:"nodeClass"`
	DataType  string `json:"dataType"`
	Direction string `json:"direction"`
}
type ModbusRange = sourceresource.ModbusRange
type ModbusOptions = sourceresource.ModbusOptions

// SourceDefinition is the user-owned connection document. Authentication uses
// an opaque reference; secret material is never part of exported configuration.
type SourceDefinition struct {
	tlsConfig       *tls.Config
	Name            string                     `json:"name"`
	Description     string                     `json:"description"`
	Protocol        Protocol                   `json:"protocol"`
	SourceType      string                     `json:"sourceType,omitempty"`
	Role            ConnectionRole             `json:"role,omitempty"`
	Endpoint        string                     `json:"endpoint"`
	SQL             *SQLConnection             `json:"sql,omitempty"`
	CredentialRef   string                     `json:"credentialRef,omitempty"`
	RuntimeLocation string                     `json:"runtimeLocation"`
	HTTP            *HTTPConnection            `json:"http,omitempty"`
	MQTT            *MQTTConnection            `json:"mqtt,omitempty"`
	OPCUA           *OPCUAConnection           `json:"opcua,omitempty"`
	Modbus          *ModbusConnection          `json:"modbus,omitempty"`
	Extensions      map[string]json.RawMessage `json:"extensions,omitempty"`
	File            *FileSourceOptions         `json:"file,omitempty"`
	OpenProtocol    *OpenProtocolOptions       `json:"openProtocol,omitempty"`
}

func (d *SourceDefinition) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return invalid("document", "objectRequired")
	}
	for _, field := range []string{"sdkVersion", "configVersion", "supportedVersions"} {
		if _, exists := fields[field]; exists {
			return invalid(field, "unsupported")
		}
	}
	type connection SourceDefinition
	var value connection
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*d = SourceDefinition(value)
	return nil
}

// Optional collection fields preserve existing Source documents. New mappings
// own their collection configuration and override these values at execution.
type MQTTConnection = MQTTOptions
type OPCUAConnection = OPCUAOptions
type ModbusConnection = ModbusOptions

// RuntimeSource is assembled for a driver operation, never persisted as a Source.
type RuntimeSource = SourceDefinition

// Connection contains only endpoint and authentication/session settings.
func (d SourceDefinition) Connection() RuntimeSource {
	r := d
	if d.MQTT != nil {
		value := *d.MQTT
		r.MQTT = &value
	}
	if d.OPCUA != nil {
		value := *d.OPCUA
		r.OPCUA = &value
	}
	if d.Modbus != nil {
		value := *d.Modbus
		r.Modbus = &value
	}
	return r
}

func (d SourceDefinition) Validate() error {
	if _, err := d.tlsReferences(); err != nil {
		return err
	}
	for _, field := range []string{"ingress"} {
		if len(d.Extensions[field]) > 0 {
			return invalid("extensions."+field, "unsupportedCapability")
		}
	}
	return d.Connection().validate(false)
}
func (d SourceDefinition) ConnectionRole() ConnectionRole {
	if d.Role == "" {
		return ClientRole
	}
	return d.Role
}

func (d SourceDefinition) validate(collection bool) error {
	if d.OpenProtocol != nil && d.Protocol != OpenProtocol {
		return invalid("openProtocol", "protocolConfigMismatch")
	}
	if d.File != nil && d.Protocol != FileRead {
		return invalid("file", "protocolConfigMismatch")
	}
	if err := validateName(d.Name, d.Description); err != nil {
		return err
	}
	if d.RuntimeLocation != "local" {
		return invalid("runtimeLocation", "unsupported")
	}
	if d.ConnectionRole() != ClientRole && d.ConnectionRole() != ServerRole {
		return invalid("role", "unsupported")
	}
	if d.SourceType != "" {
		allowed := map[string]bool{"plant0-simctl": true, "pi-web-api": true, "plex-mes": true, "wms-rest": true, "sap-odata": true, "yonyou-yonbip": true, "kingdee-jdy": true, "ngp": true, "kingdee-k3cloud": true, "salesforce": true, "teamcenter": true, "hikvision-isapi": true}
		if !allowed[d.SourceType] || (d.Protocol != HTTP && d.Protocol != OAuth) || d.ConnectionRole() != ClientRole {
			return invalid("sourceType", "unsupported")
		}
		if d.SourceType == "plant0-simctl" && (d.Protocol != HTTP || d.CredentialRef != "" || d.HTTP == nil || d.HTTP.Authentication != "none" || len(d.HTTP.Headers) != 0) {
			return invalid("http.authentication", "publicReadOnlySource")
		}
	}
	if d.ConnectionRole() == ServerRole && d.Protocol != MQTT && d.Protocol != OPCUA && d.Protocol != HTTP {
		return invalid("role", "sourceUnsupported")
	}
	u, err := url.Parse(d.Endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return invalid("endpoint", "invalid")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return invalid("endpoint", "invalidPort")
		}
	}
	if d.Protocol != EdgeSQL && d.SQL != nil {
		return invalid("sql", "protocolConfigMismatch")
	}
	if d.Protocol != HTTP && d.Protocol != OAuth && d.HTTP != nil {
		return invalid("http", "protocolConfigMismatch")
	}
	// A Server Source selects a platform listener, never an arbitrary bind address.
	if d.ConnectionRole() == ServerRole && ((d.Protocol == MQTT && d.Endpoint != "mqtt://managed") || (d.Protocol == OPCUA && d.Endpoint != "opc.tcp://managed") || (d.Protocol == HTTP && d.Endpoint != "http://managed")) {
		return invalid("endpoint", "managedServerRequired")
	}
	switch d.Protocol {
	case OpenProtocol:
		return validateOpenProtocol(d)
	case EdgeSQL:
		return validateSQLConnection(d, u)
	case FileRead:
		return d.validateFileSource(u)
	case USBHID:
		_, legacyErr := d.HIDDevice()
		if (!IsHIDDeviceKey(d.Endpoint) && legacyErr != nil) || d.CredentialRef != "" || d.MQTT != nil || d.OPCUA != nil || d.Modbus != nil {
			return invalid("endpoint", "invalid")
		}
	case HTTP, OAuth:
		if d.HTTP == nil || d.MQTT != nil || d.OPCUA != nil || d.Modbus != nil || !validHTTPBase(d.Endpoint) {
			return invalid("http", "invalid")
		}
		if err := ValidateSourceHTTPHeaders(d.HTTP.Headers); err != nil {
			return err
		}
		if d.SourceType == "pi-web-api" {
			if err := validatePIBinding(d); err != nil {
				return err
			}
		}
		if d.SourceType == "teamcenter" {
			if err := validateTeamcenterFiles(d); err != nil {
				return err
			}
		}
		if d.SourceType == "salesforce" {
			if _, err := salesforceQueries(d); err != nil {
				return err
			}
		}
		if d.ConnectionRole() == ServerRole && len(d.HTTP.Headers) > 0 {
			return invalid("http.headers", "clientOnly")
		}
		parameters, err := json.Marshal(d.HTTP.Parameters)
		if err != nil || len(parameters) > 64*1024 || len(d.HTTP.Parameters) > 128 {
			return invalid("http.parameters", "invalid")
		}
		for key := range d.HTTP.Parameters {
			if strings.TrimSpace(key) == "" || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
				return invalid("http.parameters", "invalid")
			}
		}
		if d.HTTP.Port < 0 || d.HTTP.Port > 65535 || d.ConnectionRole() != ServerRole && (d.HTTP.Port != 0 || d.HTTP.Anonymous) {
			return invalid("http.port", "serverOnly")
		}
		if d.ConnectionRole() == ServerRole {
			if d.HTTP.OAuth != nil || d.HTTP.CACertificate != "" || (d.HTTP.Anonymous && d.HTTP.Authentication != "" && d.HTTP.Authentication != "none") || (!d.HTTP.Anonymous && d.HTTP.Authentication != "" && d.HTTP.Authentication != "bearer") {
				return invalid("http.authentication", "invalidServerAuthentication")
			}
			return nil
		}
		if _, err := publicCertificates(d.HTTP.CACertificate, true); err != nil {
			return invalid("http.caCertificate", "invalid")
		}
		if d.Protocol == OAuth {
			if d.HTTP.OAuth == nil || d.HTTP.Authentication != "" {
				return invalid("http.oauth", "invalid")
			}
			return d.HTTP.OAuth.validate()
		}
		if d.HTTP.OAuth != nil {
			return invalid("http.oauth", "unsupported")
		}
		switch d.HTTP.Authentication {
		case "none", "basic", "bearer", "custom", "http":
			return nil
		}
		return invalid("http.authentication", "unsupported")
	case MQTT:
		if d.MQTT == nil || d.OPCUA != nil || d.Modbus != nil {
			return invalid("mqtt", "protocolConfigMismatch")
		}
		if (u.Scheme != "mqtt" && u.Scheme != "mqtts") || (u.Path != "" && u.Path != "/") {
			return invalid("endpoint", "invalidScheme")
		}
		m := d.MQTT
		if m.CACertificate != "" {
			if _, err := publicCertificates(m.CACertificate, true); err != nil || u.Scheme != "mqtts" {
				return invalid("mqtt.caCertificate", "invalid")
			}
		}
		if strings.TrimSpace(m.ClientID) == "" || m.QoS < 0 || m.QoS > 2 {
			return invalid("mqtt", "invalid")
		}
		for filter, qos := range m.Subscriptions {
			if !validMQTTTopic(filter, true) || qos > 2 {
				return invalid("mqtt.subscriptions", "invalid")
			}
		}
		for _, filter := range m.TopicFilters {
			if !validMQTTTopic(filter, true) {
				return invalid("mqtt.topicFilters", "invalid")
			}
		}
		if d.ConnectionRole() == ServerRole {
			if len(m.TopicFilters)+len(m.ServerSubscribeTopics) > 1000 {
				return invalid("mqtt", "tooManyResources")
			}
			for _, topic := range append(append([]string{}, m.TopicFilters...), m.ServerSubscribeTopics...) {
				if !validMQTTTopic(topic, true) || strings.HasPrefix(topic, "$") || strings.HasPrefix(topic, "/") {
					return invalid("mqtt", "invalidServerTopic")
				}
			}
			for _, topic := range m.ServerSubscribeTopics {
				if strings.ContainsAny(topic, "+#") {
					return invalid("mqtt.serverSubscribeTopics", "concreteTopicRequired")
				}
			}
		} else if len(m.ServerSubscribeTopics) > 0 {
			return invalid("mqtt.serverSubscribeTopics", "serverOnly")
		}
		if m.RequestTopic != "" || m.ReplyTopic != "" || m.ReplyTopicField != "" || m.CorrelationField != "" {
			if !validMQTTTopic(m.RequestTopic, false) || !validMQTTTopic(m.ReplyTopic, false) || !safeField(m.CorrelationField) {
				return invalid("mqtt", "incompleteRequestReply")
			}
			hasReplyTemplate := strings.Count(m.ReplyTopic, "{correlationId}") == 1
			if strings.Contains(m.ReplyTopic, "{") || strings.Contains(m.ReplyTopic, "}") {
				if !hasReplyTemplate || strings.ReplaceAll(m.ReplyTopic, "{correlationId}", "") == "" {
					return invalid("mqtt.replyTopic", "invalidTemplate")
				}
			}
			if hasReplyTemplate != (m.ReplyTopicField != "") || m.ReplyTopicField != "" && !safeField(m.ReplyTopicField) {
				return invalid("mqtt.replyTopicField", "templateRequired")
			}
			if m.RequestTopic == m.ReplyTopic {
				return invalid("mqtt.replyTopic", "requestReplyLoop")
			}
			if d.ConnectionRole() == ServerRole && (strings.HasPrefix(m.RequestTopic, "$") || strings.HasPrefix(m.ReplyTopic, "$") || strings.HasPrefix(m.RequestTopic, "/") || strings.HasPrefix(m.ReplyTopic, "/")) {
				return invalid("mqtt", "invalidServerTopic")
			}
		}
	case OPCUA:
		if d.OPCUA == nil || d.MQTT != nil || d.Modbus != nil {
			return invalid("opcua", "protocolConfigMismatch")
		}
		if u.Scheme != "opc.tcp" {
			return invalid("endpoint", "invalidScheme")
		}
		o := d.OPCUA
		if certificates, err := publicCertificates(o.ServerCertificate, false); err != nil || len(certificates) > 1 {
			return invalid("opcua.serverCertificate", "invalid")
		}
		if collection && (o.SamplingIntervalMs < 100 || o.SamplingIntervalMs > 3600000) {
			return invalid("opcua.samplingIntervalMs", "outOfRange")
		}
		if (o.SecurityMode == "None" && o.SecurityPolicy != "None") ||
			(o.SecurityMode != "None" && o.SecurityMode != "Sign" && o.SecurityMode != "SignAndEncrypt") ||
			(o.SecurityMode != "None" && o.SecurityPolicy != "Basic256Sha256" && o.SecurityPolicy != "Aes128_Sha256_RsaOaep" && o.SecurityPolicy != "Aes256_Sha256_RsaPss") {
			return invalid("opcua.securityPolicy", "invalid")
		}
		for _, node := range o.NodeIDs {
			if strings.TrimSpace(node) == "" {
				return invalid("opcua.nodeIds", "invalid")
			}
		}
		if d.ConnectionRole() == ServerRole {
			if len(o.ServerNodes) == 0 || len(o.ServerNodes) > 1000 || len(o.NodeIDs) > 1000 {
				return invalid("opcua.serverNodes", "outOfRange")
			}
			if !((o.SecurityMode == "None" && o.SecurityPolicy == "None") || (o.SecurityMode == "SignAndEncrypt" && o.SecurityPolicy == "Basic256Sha256")) {
				return invalid("opcua.securityPolicy", "unsupportedServerSecurity")
			}
			seen := map[string]bool{}
			for _, node := range o.ServerNodes {
				if node.ID == "" || len(node.ID) > common.NameMaxLen || seen[node.ID] {
					return invalid("opcua.serverNodes.id", "duplicateOrEmpty")
				}
				for i, ch := range node.ID {
					letter := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
					if !letter && (i == 0 || !(ch >= '0' && ch <= '9' || ch == '_' || ch == '-')) {
						return invalid("opcua.serverNodes.id", "invalid")
					}
				}
				seen[node.ID] = true
				if strings.TrimSpace(node.Name) == "" || common.ExceedNameLimit(node.Name) {
					return invalid("opcua.serverNodes.name", "invalid")
				}
				if node.NodeClass != "Variable" && node.NodeClass != "Method" ||
					(node.DataType != "Double" && node.DataType != "Int32" && node.DataType != "Boolean" && node.DataType != "String") ||
					(node.Direction != "ingress" && node.Direction != "egress" && node.Direction != "bidirectional") ||
					(node.NodeClass == "Method" && (node.DataType != "String" || node.Direction != "ingress")) {
					return invalid("opcua.serverNodes", "invalid")
				}
			}
			for _, id := range o.NodeIDs {
				if !seen[id] {
					return invalid("opcua.nodeIds", "outsideDeclaredNodes")
				}
			}
		} else if len(o.ServerNodes) > 0 {
			return invalid("opcua.serverNodes", "serverOnly")
		}
	case ModbusTCP:
		if d.Modbus == nil || d.MQTT != nil || d.OPCUA != nil {
			return invalid("modbus", "protocolConfigMismatch")
		}
		if u.Scheme != "tcp" || (u.Path != "" && u.Path != "/") {
			return invalid("endpoint", "invalidScheme")
		}
		m := d.Modbus
		if m.UnitID < 1 || m.UnitID > 247 {
			return invalid("modbus.unitId", "outOfRange")
		}
		if collection && (m.PollIntervalMs < 100 || m.PollIntervalMs > 3600000) {
			return invalid("modbus.pollIntervalMs", "outOfRange")
		}
		seen := map[string]bool{}
		for i, r := range m.Ranges {
			field := fmt.Sprintf("modbus.ranges[%d]", i)
			if r.UnitID != nil && (*r.UnitID < 1 || *r.UnitID > 247) {
				return invalid(field+".unitId", "outOfRange")
			}
			if r.Writable != nil && *r.Writable && r.Table != "holding-register" && r.Table != "coil" {
				return invalid(field+".writable", "readOnlyTable")
			}
			if r.ID == "" || seen[r.ID] {
				return invalid(field+".id", "duplicateOrEmpty")
			}
			seen[r.ID] = true
			maxQuantity := 125
			switch r.Table {
			case "holding-register", "input-register":
			case "coil", "discrete-input":
				maxQuantity = 2000
			default:
				return invalid(field+".table", "unsupported")
			}
			if r.Address < 0 || r.Address > 65535 || r.Quantity < 1 || r.Quantity > maxQuantity || r.Address+r.Quantity > 65536 {
				return invalid(field, "outOfRange")
			}
		}
	default:
		return invalid("protocol", "unsupported")
	}
	return nil
}

// The configured origin is the authority; scripts can only supply paths under
// its prefix. Address validation is repeated at dial time by the HTTP client.
func validHTTPBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Path, "\\") {
		return false
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (d SourceDefinition) HTTPAuthentication() string {
	if d.HTTP == nil {
		return ""
	}
	if d.ConnectionRole() == ServerRole {
		if d.HTTP.Anonymous {
			return "none"
		}
		if d.HTTP.Authentication == "" {
			return "bearer"
		}
	}
	return d.HTTP.Authentication
}

// Protocol-level eligibility is not device capability discovery. Deployment
// must also validate each selected native resource and operation with the driver.
func (d SourceDefinition) Supports(model AccessModel) bool {
	if d.Validate() != nil {
		return false
	}
	if d.Protocol == USBHID {
		return model == Publish
	}
	switch model {
	case Publish, Subscribe:
		return true
	case RequestReply:
		return d.Protocol != MQTT || d.MQTT.RequestTopic != ""
	default:
		return false
	}
}

type ScriptDefinition struct {
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeoutMs"`
}

type SourceBinding struct {
	Alias          string `json:"alias"`
	SourceID       int64  `json:"sourceId"`
	ConfigRevision int64  `json:"configRevision"`
}

const ScriptMode AccessModel = "script"

const MaxScriptSources = 16

type StreamDefinition struct {
	legacyProgram         bool
	ProgramSources        []ProgramSourceBinding     `json:"-"`
	ProgramSourceBindings []ProgramSourceBinding     `json:"-"`
	Sources               []SourceBinding            `json:"sources,omitempty"`
	Collection            *CollectionDefinition      `json:"collection"`
	Name                  string                     `json:"name"`
	Description           string                     `json:"description"`
	SourceID              int64                      `json:"sourceId"`
	Model                 AccessModel                `json:"model"`
	NativeResourceRef     string                     `json:"nativeResourceRef"`
	NativeResourceRefs    []string                   `json:"nativeResourceRefs,omitempty"`
	TopicIDs              []int64                    `json:"topicIds"`
	MetadataTarget        *unsmeta.Mapping           `json:"metadataTarget,omitempty"`
	Script                ScriptDefinition           `json:"script"`
	ResponseScript        *ScriptDefinition          `json:"responseScript,omitempty"`
	Program               *ProgramDefinition         `json:"program,omitempty"`
	Extensions            map[string]json.RawMessage `json:"extensions,omitempty"`
}

// IsCompleteProgram checks the current in-memory representation, never a model version.
func (d StreamDefinition) IsCompleteProgram() bool {
	return (d.Model == "" || d.Model == ProgramAccess) && d.SourceID == 0 && d.Program == nil && d.ResponseScript == nil &&
		d.NativeResourceRef == "" && d.NativeResourceRefs == nil && d.TopicIDs == nil && d.Sources == nil && d.Collection == nil && d.MetadataTarget == nil
}

// Public program documents share one script object and binding contract.
// Persisted historical flat releases use decodeStoredProgram, never this entry.
func (d *StreamDefinition) UnmarshalJSON(raw []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return invalid("document", "objectRequired")
	}
	for _, field := range []string{"sdkVersion", "configVersion", "supportedVersions", "model", "sourceId", "collection", "metadataTarget", "program", "responseScript", "topicIds", "nativeResourceRef", "nativeResourceRefs", "timeoutMs"} {
		if _, exists := object[field]; exists {
			return invalid(field, "unsupported")
		}
	}
	for _, field := range []string{"script", "sources", "sourceBindings"} {
		if value, exists := object[field]; exists && strings.TrimSpace(string(value)) == "null" {
			return invalid(field, "invalid")
		}
	}
	var flat struct {
		Name           string                     `json:"name"`
		Description    string                     `json:"description"`
		Script         ScriptDefinition           `json:"script"`
		Sources        []ProgramSourceBinding     `json:"sources,omitempty"`
		SourceBindings []ProgramSourceBinding     `json:"sourceBindings,omitempty"`
		Extensions     map[string]json.RawMessage `json:"extensions,omitempty"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&flat); err != nil {
		return err
	}
	*d = StreamDefinition{Name: flat.Name, Description: flat.Description, Model: ProgramAccess,
		Script: flat.Script, ProgramSources: flat.Sources, ProgramSourceBindings: flat.SourceBindings, Extensions: flat.Extensions}
	return d.validateProgramBindings()
}

func (d StreamDefinition) MarshalJSON() ([]byte, error) {
	if err := d.validateCompleteProgram(false); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Name           string                     `json:"name"`
		Description    string                     `json:"description"`
		Script         ScriptDefinition           `json:"script"`
		Sources        []ProgramSourceBinding     `json:"sources,omitempty"`
		SourceBindings []ProgramSourceBinding     `json:"sourceBindings,omitempty"`
		Extensions     map[string]json.RawMessage `json:"extensions,omitempty"`
	}{d.Name, d.Description, d.Script, d.ProgramSources, d.ProgramSourceBindings, d.Extensions})
}

func (d StreamDefinition) validateCompleteProgram(required bool) error {
	if err := d.validateProgramBindings(); err != nil {
		return err
	}
	if d.SourceID != 0 || d.Model != "" && d.Model != ProgramAccess || d.Program != nil || d.ResponseScript != nil ||
		d.NativeResourceRef != "" || d.NativeResourceRefs != nil || d.TopicIDs != nil || d.Sources != nil || d.Collection != nil || d.MetadataTarget != nil {
		return invalid("document", "completeProgramOnly")
	}
	maximum := MaxScriptTimeoutMs
	if d.legacyProgram {
		maximum = streamexec.MaxProgramTimeoutMs
	}
	if d.Script.TimeoutMs > maximum {
		return invalid("timeoutMs", "outOfRange")
	}
	return validateScriptWithTimeout(d.Script, "script", required, maximum)
}

// ResourceRefs is the single source of input selection. Request/reply and
// subscription operations remain singular; publication may select many inputs.
func (d StreamDefinition) ResourceRefs() []string {
	if len(d.NativeResourceRefs) > 0 {
		return d.NativeResourceRefs
	}
	if d.NativeResourceRef != "" {
		return []string{d.NativeResourceRef}
	}
	return nil
}

func (d StreamDefinition) MatchesResource(ref string) bool {
	for _, selected := range d.ResourceRefs() {
		if selected == ref {
			return true
		}
		if d.Model == Publish && strings.HasPrefix(selected, "mqtt:") && strings.HasPrefix(ref, "mqtt:") &&
			mqttFilterMatches(strings.TrimPrefix(selected, "mqtt:"), strings.TrimPrefix(ref, "mqtt:")) {
			return true
		}
	}
	return false
}

// MQTT filters select inputs; the event and script context retain the actual
// Topic name. Wildcards never apply to OPC UA/Modbus IDs or downlink operations.
func mqttFilterMatches(filter, topic string) bool {
	if !validMQTTTopic(filter, true) || !validMQTTTopic(topic, false) {
		return false
	}
	if strings.HasPrefix(topic, "$") && (filter[0] == '+' || filter[0] == '#') {
		return false
	}
	levels, names := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, level := range levels {
		if level == "#" {
			return true
		}
		if i >= len(names) || (level != "+" && level != names[i]) {
			return false
		}
	}
	return len(levels) == len(names)
}

// ValidateDraft checks the document shape, not connectivity or deployment.
// Drafts may omit references so users can save unfinished work.
func (d StreamDefinition) ValidateDraft() error {
	if err := validateName(d.Name, d.Description); err != nil {
		return err
	}
	return d.validateCompleteProgram(false)
}

// ValidateDeployment is a pure first gate. The service must additionally check
// source revision/health, native operation capabilities, Topic ACL/schema and
// script compilation. Passing this function alone never means "running".
func (d StreamDefinition) ValidateDeployment(sourceID int64, source SourceDefinition) error {
	if err := d.ValidateDraft(); err != nil {
		return err
	}
	return d.validateCompleteProgram(true)
}

func validateName(name, description string) error {
	if strings.TrimSpace(name) == "" || common.ExceedNameLimit(name) {
		return invalid("name", "invalidLength")
	}
	if common.ExceedDescLimit(description) {
		return invalid("description", "invalidLength")
	}
	return nil
}

func validateScript(script ScriptDefinition, field string, required bool) error {
	return validateScriptWithTimeout(script, field, required, MaxScriptTimeoutMs)
}

func validateScriptWithTimeout(script ScriptDefinition, field string, required bool, maxTimeoutMs int) error {
	if required && strings.TrimSpace(script.Code) == "" {
		return invalid(field+".code", "required")
	}
	if len(script.Code) > MaxScriptBytes {
		return invalid(field+".code", "tooLarge")
	}
	if script.TimeoutMs < 1 || script.TimeoutMs > maxTimeoutMs {
		return invalid(field+".timeoutMs", "outOfRange")
	}
	return nil
}

func safeField(field string) bool {
	for _, part := range strings.Split(field, ".") {
		if part == "" || part == "__proto__" || part == "prototype" || part == "constructor" {
			return false
		}
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
				return false
			}
		}
	}
	return true
}

func validMQTTTopic(topic string, filter bool) bool {
	if topic == "" || len(topic) > 65535 || !utf8.ValidString(topic) || strings.ContainsRune(topic, '\x00') {
		return false
	}
	levels := strings.Split(topic, "/")
	for i, level := range levels {
		if strings.ContainsAny(level, "+#") && (!filter || (level != "+" && !(level == "#" && i == len(levels)-1))) {
			return false
		}
	}
	return true
}
