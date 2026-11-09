package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

var ErrSubscription = errors.New("SourceStream.subscriptionSyntax")

type scriptSubscription struct {
	Alias    string
	Topic    string
	QoS      int
	Handler  string
	Variable string
}

// 只解析声明，不运行用户代码。一个 Stream 对应一个订阅，回调每条消息使用独立 VM。
// 支持 return sources.get("alias").subscribe("topic", handler, {qos: 1})。
func parseSubscription(code string) (*scriptSubscription, error) {
	wrapped := "async function __stream(input, context) {\n" + code + "\n}"
	program, err := parser.ParseFile(nil, "stream.js", wrapped, 0)
	if err != nil {
		return nil, streamexec.ErrSyntax
	}
	count := subscriptionCalls(reflect.ValueOf(program))
	if count == 0 {
		return nil, nil
	}
	body := program.Body[0].(*ast.FunctionDeclaration).Function.Body.List
	if count != 1 || (len(body) != 1 && len(body) != 2) {
		return nil, ErrSubscription
	}
	var variable string
	var receiver ast.Expression
	if len(body) == 2 {
		declaration, ok := body[0].(*ast.LexicalDeclaration)
		if !ok || len(declaration.List) != 1 {
			return nil, ErrSubscription
		}
		name, ok := declaration.List[0].Target.(*ast.Identifier)
		if !ok {
			return nil, ErrSubscription
		}
		variable, receiver = name.Name.String(), declaration.List[0].Initializer
	}
	var expression ast.Expression
	switch statement := body[len(body)-1].(type) {
	case *ast.ReturnStatement:
		expression = statement.Argument
	case *ast.ExpressionStatement:
		expression = statement.Expression
	default:
		return nil, ErrSubscription
	}
	call, ok := expression.(*ast.CallExpression)
	if !ok || len(call.ArgumentList) < 2 || len(call.ArgumentList) > 3 {
		return nil, ErrSubscription
	}
	method, ok := call.Callee.(*ast.DotExpression)
	if !ok || method.Identifier.Name.String() != "subscribe" {
		return nil, ErrSubscription
	}
	if variable != "" {
		name, ok := method.Left.(*ast.Identifier)
		if !ok || name.Name.String() != variable {
			return nil, ErrSubscription
		}
	} else {
		receiver = method.Left
	}
	get, ok := receiver.(*ast.CallExpression)
	if !ok || len(get.ArgumentList) != 1 {
		return nil, ErrSubscription
	}
	accessor, ok := get.Callee.(*ast.DotExpression)
	if !ok || accessor.Identifier.Name.String() != "get" {
		return nil, ErrSubscription
	}
	root, ok := accessor.Left.(*ast.Identifier)
	if !ok || root.Name.String() != "sources" {
		return nil, ErrSubscription
	}
	alias, ok := get.ArgumentList[0].(*ast.StringLiteral)
	if !ok {
		return nil, ErrSubscription
	}
	topic, ok := call.ArgumentList[0].(*ast.StringLiteral)
	if !ok || !validMQTTTopic(topic.Value.String(), true) {
		return nil, ErrSubscription
	}
	handler := call.ArgumentList[1]
	switch f := handler.(type) {
	case *ast.FunctionLiteral:
		if f.Generator {
			return nil, ErrSubscription
		}
	case *ast.ArrowFunctionLiteral:
	default:
		return nil, ErrSubscription
	}
	sub := &scriptSubscription{Variable: variable, Alias: alias.Value.String(), Topic: topic.Value.String(), Handler: wrapped[int(handler.Idx0())-1 : int(handler.Idx1())-1]}
	if len(call.ArgumentList) == 3 {
		options, ok := call.ArgumentList[2].(*ast.ObjectLiteral)
		if !ok || len(options.Value) != 1 {
			return nil, ErrSubscription
		}
		property, ok := options.Value[0].(*ast.PropertyKeyed)
		if !ok || property.Computed || property.Kind != ast.PropertyKindValue {
			return nil, ErrSubscription
		}
		key := ""
		switch k := property.Key.(type) {
		case *ast.StringLiteral:
			key = k.Value.String()
		case *ast.Identifier:
			key = k.Name.String()
		}
		if key != "qos" {
			return nil, ErrSubscription
		}
		n, ok := property.Value.(*ast.NumberLiteral)
		if !ok {
			return nil, ErrSubscription
		}
		switch n.Value {
		case int64(0):
			sub.QoS = 0
		case int64(1):
			sub.QoS = 1
		case int64(2):
			sub.QoS = 2
		default:
			return nil, ErrSubscription
		}
	}
	return sub, nil
}

// AST 的 Scope/DeclarationList 会重复引用同一节点，仅沿语法节点计数。
func subscriptionCalls(value reflect.Value) int {
	seen := map[uintptr]bool{}
	var visit func(reflect.Value) int
	visit = func(v reflect.Value) int {
		if !v.IsValid() {
			return 0
		}
		if v.Kind() == reflect.Interface {
			return visit(v.Elem())
		}
		if v.Kind() == reflect.Pointer {
			if v.IsNil() || seen[v.Pointer()] {
				return 0
			}
			seen[v.Pointer()] = true
			if v.CanInterface() {
				if dot, ok := v.Interface().(*ast.DotExpression); ok && dot.Identifier.Name.String() == "subscribe" {
					return 1
				}
				if bracket, ok := v.Interface().(*ast.BracketExpression); ok {
					if key, ok := bracket.Member.(*ast.StringLiteral); ok && key.Value.String() == "subscribe" {
						return 1
					}
				}
			}
			return visit(v.Elem())
		}
		total := 0
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).CanInterface() {
					total += visit(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				total += visit(v.Index(i))
			}
		}
		return total
	}
	return visit(value)
}

func (sub *scriptSubscription) acquisition(def StreamDefinition) (StreamDefinition, error) {
	for _, binding := range def.Sources {
		if binding.Alias == sub.Alias {
			return StreamDefinition{SourceID: binding.SourceID, Model: Publish, NativeResourceRef: "mqtt:" + sub.Topic, Collection: &CollectionDefinition{MQTT: &MQTTCollection{QoS: sub.QoS}}}, nil
		}
	}
	return StreamDefinition{}, ErrSubscription
}

func validateSubscription(ctx context.Context, def StreamDefinition) (*scriptSubscription, error) {
	sub, err := parseSubscription(def.Script.Code)
	if err != nil || sub == nil {
		return sub, err
	}
	watch, err := sub.acquisition(def)
	if err != nil {
		return nil, err
	}
	source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, watch.SourceID)
	if err != nil {
		return nil, err
	}
	if source.Protocol != string(MQTT) {
		return nil, ErrSubscription
	}
	return sub, nil
}

func (s *Service) testSubscription(ctx context.Context, def StreamDefinition, sub *scriptSubscription) (map[string]any, error) {
	if err := validateScriptSources(ctx, def, true); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	watch, err := sub.acquisition(def)
	if err != nil {
		return nil, err
	}
	input, err := s.sampleWithin(ctx, watch, 20*time.Second)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, streamexec.ErrSourceReadTimeout
		}
		return nil, err
	}
	result, err := s.transformScript(ctx, def, input.Data, subscriptionContext(watch.SourceID, input, true))
	if err != nil {
		return nil, err
	}
	return map[string]any{"input": input, "result": result, "dryRun": true, "sideEffects": false}, nil
}

func subscriptionContext(sourceID int64, input SourceInput, dryRun bool) map[string]any {
	return map[string]any{"sourceId": sourceID, "topic": strings.TrimPrefix(input.ResourceRef, "mqtt:"), "resourceRef": input.ResourceRef, "receivedAt": input.ReceivedAt, "dryRun": dryRun}
}

func subscriptionMode(document string) string {
	var def StreamDefinition
	if json.Unmarshal([]byte(document), &def) == nil {
		if sub, err := parseSubscription(def.Script.Code); err == nil && sub != nil {
			return "mqtt-subscription"
		}
	}
	return "manual"
}
