package streamexec

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

var httpRouteLiteral = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
var httpRouteParameter = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateHTTPRoute describes relative incoming routes, never upstream URLs.
func ValidateHTTPRoute(method, path string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return false
	}
	if path == "/" {
		return true
	}
	if len(path) == 0 || len(path) > 2048 || path[0] != '/' {
		return false
	}
	parameters := map[string]bool{}
	for _, part := range strings.Split(path[1:], "/") {
		if strings.HasPrefix(part, ":") {
			name := strings.TrimPrefix(part, ":")
			if !httpRouteParameter.MatchString(name) || parameters[name] {
				return false
			}
			parameters[name] = true
		} else if part == "." || part == ".." || !httpRouteLiteral.MatchString(part) {
			return false
		}
	}
	return true
}

func allowsProgramHTTPCall(manifest ProgramManifest, call ProgramCall) bool {
	if call.SourceID == "" || call.ResourceRef != "" || call.TopicID != "" || len(call.Input) != 0 {
		return false
	}
	var options map[string]json.RawMessage
	if len(call.Options) > 0 && (json.Unmarshal(call.Options, &options) != nil || options == nil) {
		return false
	}
	switch call.Kind {
	case "source.parameters", "source.secrets":
		if len(options) != 0 {
			return false
		}
	case "source.request":
		if options == nil {
			return false
		}
	default:
		return false
	}
	for _, source := range manifest.Sources {
		if source.ID == call.SourceID {
			return true
		}
	}
	return false
}

func (p *programVM) installHTTPSource(handle *goja.Object, sourceID string) {
	for _, name := range []string{"parameters", "secrets", "request"} {
		name := name
		_ = handle.Set(name, func(call goja.FunctionCall) goja.Value {
			if p.registering {
				p.fail("Source I/O is not available during registration")
			}
			if len(call.Arguments) > 1 {
				p.fail("invalid Source call arguments")
			}
			options := json.RawMessage(`{}`)
			if len(call.Arguments) > 0 {
				object, ok := call.Argument(0).(*goja.Object)
				if !ok || object.ClassName() != "Object" {
					p.fail("Source options must be an object")
				}
				var err error
				options, err = p.encode(object)
				if err != nil {
					p.fail("invalid Source options")
				}
			} else if name == "request" {
				p.fail("Source request requires options")
			}
			request := ProgramCall{Kind: "source." + name, SourceID: sourceID, Options: options}
			if !allowsProgramHTTPCall(p.result.Manifest, request) {
				p.fail("invalid Source options or declaration")
			}
			if name != "request" {
				request.Options = nil
			}
			return p.dispatch(request)
		})
	}
}
