package main

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// FieldKind describes how a configuration field should be rendered in the UI.
type FieldKind string

const (
	KindGroup      FieldKind = "group"
	KindString     FieldKind = "string"
	KindPassword   FieldKind = "password"
	KindInt        FieldKind = "int"
	KindBool       FieldKind = "bool"
	KindTriBool    FieldKind = "triBool" // nullable boolean (nil = use frp default)
	KindSelect     FieldKind = "select"
	KindStringList FieldKind = "stringList"
	KindStringMap  FieldKind = "stringMap"
	KindBoolMap    FieldKind = "boolMap"
	KindPortsRange FieldKind = "portsRange" // "1000-2000,3000"
	KindBandwidth  FieldKind = "bandwidth"  // "1MB", "512KB"
	KindPlugin     FieldKind = "plugin"     // typed plugin options (client or visitor)
	KindRaw        FieldKind = "raw"        // free-form JSON
)

// Option is a single selectable value for KindSelect fields.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field is a node in the generated configuration schema tree.
type Field struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Kind        FieldKind `json:"kind"`
	Description string    `json:"description,omitempty"`
	Options     []Option  `json:"options,omitempty"`
	Children    []*Field  `json:"children,omitempty"`

	// For KindPlugin fields: which plugin family this belongs to and the
	// available plugin type identifiers.
	PluginKind  string   `json:"pluginKind,omitempty"`
	PluginTypes []string `json:"pluginTypes,omitempty"`
}

// SchemaResponse bundles a schema tree with the completed default values so the
// frontend can render placeholders without a round-trip per field.
type SchemaResponse struct {
	Schema   *Field         `json:"schema"`
	Defaults map[string]any `json:"defaults"`
}

// enumOptions maps a dotted JSON path to its allowed values.
func enumOptions(path string) []string {
	switch path {
	case "auth.method":
		return []string{"token", "oidc"}
	case "log.level":
		return []string{"trace", "debug", "info", "warn", "error"}
	case "transport.protocol":
		return []string{"tcp", "kcp", "quic", "websocket", "wss"}
	case "transport.wireProtocol":
		return []string{"v1", "v2"}
	case "transport.bandwidthLimitMode":
		return []string{"client", "server"}
	case "transport.proxyProtocolVersion":
		return []string{"", "v1", "v2"}
	case "healthCheck.type":
		return []string{"", "tcp", "http"}
	}
	return nil
}

func isSecret(key string) bool {
	lk := strings.ToLower(key)
	if strings.Contains(lk, "password") || strings.Contains(lk, "secret") {
		return true
	}
	return lk == "token" || lk == "secretkey"
}

func isPluginStruct(t reflect.Type) (pluginKind string, ok bool) {
	if t == reflect.TypeFor[v1.TypedClientPluginOptions]() {
		return "client", true
	}
	if t == reflect.TypeFor[v1.TypedVisitorPluginOptions]() {
		return "visitor", true
	}
	return "", false
}

func kindForField(t reflect.Type, key string) FieldKind {
	//nolint:govet // "reflect.Ptr should be inlined" is analyzer noise
	if t.Kind() == reflect.Ptr {
		elem := t.Elem()
		if elem.Kind() == reflect.Bool {
			return KindTriBool
		}
		t = elem
	}
	if t.Kind() == reflect.String {
		if t.Name() == "AuthMethod" || t.Name() == "AuthScope" {
			return KindSelect
		}
		if isSecret(key) {
			return KindPassword
		}
		return KindString
	}
	switch t.Kind() {
	case reflect.Bool:
		return KindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return KindInt
	case reflect.Slice:
		elem := t.Elem()
		if elem.Kind() == reflect.String {
			return KindStringList
		}
		if elem == reflect.TypeFor[types.PortsRange]() {
			return KindPortsRange
		}
		return KindRaw
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			switch t.Elem().Kind() {
			case reflect.String:
				return KindStringMap
			case reflect.Bool:
				return KindBoolMap
			}
		}
		return KindRaw
	case reflect.Struct:
		if t == reflect.TypeFor[types.BandwidthQuantity]() {
			return KindBandwidth
		}
		if _, ok := isPluginStruct(t); ok {
			return KindPlugin
		}
		return KindGroup
	case reflect.Interface:
		return KindRaw
	}
	return KindRaw
}

func jsonKey(sf reflect.StructField) (string, bool) {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return "", false
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return "-", true
	}
	return name, true
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

// humanize converts a Go field name into a human-readable label.
func humanize(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			if isUpperRune(r) {
				prevLower := isLowerRune(prev) || isDigitRune(prev)
				nextLower := i+1 < len(runes) && isLowerRune(runes[i+1])
				if prevLower || nextLower {
					b.WriteByte(' ')
				}
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUpperRune(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLowerRune(r rune) bool { return r >= 'a' && r <= 'z' }
func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

func selectOptions(path string, t reflect.Type) []Option {
	var vals []string
	switch {
	case t.Name() == "AuthMethod":
		vals = []string{"token", "oidc"}
	default:
		vals = enumOptions(path)
	}
	if len(vals) == 0 {
		return nil
	}
	opts := make([]Option, 0, len(vals))
	for _, v := range vals {
		opts = append(opts, Option{Value: v, Label: v})
	}
	return opts
}

// stringFieldOptions returns dropdown options for a plain string field that has
// well-known allowed values, based on its parent struct type or its path.
func stringFieldOptions(parent reflect.Type, key, path string) []Option {
	var vals []string
	if parent.Name() == "ValueSource" && key == "type" {
		vals = []string{"file", "exec"}
	} else {
		vals = enumOptions(path)
	}
	if len(vals) == 0 {
		return nil
	}
	opts := make([]Option, 0, len(vals))
	for _, v := range vals {
		opts = append(opts, Option{Value: v, Label: v})
	}
	return opts
}

// buildGroup builds a schema group node from a struct type. Anonymous embedded
// structs are flattened into the parent so that promoted fields appear at the
// same level, matching the JSON representation.
func buildGroup(t reflect.Type, path string) *Field {
	//nolint:govet // "reflect.Ptr should be inlined" is analyzer noise
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	g := &Field{Kind: KindGroup, Children: []*Field{}}
	if t.Kind() != reflect.Struct {
		return g
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported
			continue
		}
		key, hasKey := jsonKey(sf)
		if sf.Anonymous && !hasKey {
			child := buildGroup(sf.Type, path)
			g.Children = append(g.Children, child.Children...)
			continue
		}
		if key == "-" || key == "" {
			continue
		}
		// The "version" field is API metadata, not a user-facing setting.
		if key == "version" {
			continue
		}
		f := &Field{
			Key:         key,
			Label:       humanize(sf.Name),
			Kind:        kindForField(sf.Type, key),
			Description: fieldDescriptions[t.Name()+"."+sf.Name],
		}
		fp := joinPath(path, key)

		switch f.Kind {
		case KindSelect:
			f.Options = selectOptions(fp, sf.Type)
		case KindString:
			// Promote plain string fields that have known allowed values into
			// a dropdown so the UI can guide the user.
			if opts := stringFieldOptions(t, key, fp); len(opts) > 0 {
				f.Kind = KindSelect
				f.Options = opts
			}
		case KindGroup:
			f.Children = buildGroup(sf.Type, fp).Children
		case KindPlugin:
			if pluginKind, ok := isPluginStruct(sf.Type); ok {
				f.PluginKind = pluginKind
				if pluginKind == "visitor" {
					f.PluginTypes = visitorPluginTypes
				} else {
					f.PluginTypes = clientPluginTypes
				}
			}
		}

		// A select without any allowed values is just a free-form string.
		if f.Kind == KindSelect && len(f.Options) == 0 {
			f.Kind = KindString
		}
		g.Children = append(g.Children, f)
	}
	return g
}

func jsonDefault(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func serverSchema() SchemaResponse {
	cfg := &v1.ServerConfig{}
	_ = cfg.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeFor[v1.ServerConfig](), ""),
		Defaults: jsonDefault(cfg),
	}
}

func clientCommonSchema() SchemaResponse {
	cfg := &v1.ClientCommonConfig{}
	_ = cfg.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeFor[v1.ClientCommonConfig](), ""),
		Defaults: jsonDefault(cfg),
	}
}

func proxySchema(proxyType string) SchemaResponse {
	p := v1.NewProxyConfigurerByType(v1.ProxyType(proxyType))
	if p == nil {
		return SchemaResponse{Schema: &Field{Kind: KindGroup, Children: []*Field{}}, Defaults: map[string]any{}}
	}
	p.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeOf(p).Elem(), ""),
		Defaults: jsonDefault(p),
	}
}

func visitorSchema(visitorType string) SchemaResponse {
	v := v1.NewVisitorConfigurerByType(v1.VisitorType(visitorType))
	if v == nil {
		return SchemaResponse{Schema: &Field{Kind: KindGroup, Children: []*Field{}}, Defaults: map[string]any{}}
	}
	v.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeOf(v).Elem(), ""),
		Defaults: jsonDefault(v),
	}
}

func emptySchema() SchemaResponse {
	return SchemaResponse{Schema: &Field{Kind: KindGroup, Children: []*Field{}}, Defaults: map[string]any{}}
}

func clientPluginSchema(pluginType string) SchemaResponse {
	jsonStr := `{"type":"` + pluginType + `"}`
	tc, err := v1.DecodeClientPluginOptionsJSON([]byte(jsonStr), v1.DecodeOptions{})
	if err != nil || tc.ClientPluginOptions == nil {
		return emptySchema()
	}
	tc.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeOf(tc.ClientPluginOptions).Elem(), ""),
		Defaults: jsonDefault(tc.ClientPluginOptions),
	}
}

func visitorPluginSchema(pluginType string) SchemaResponse {
	jsonStr := `{"type":"` + pluginType + `"}`
	tc, err := v1.DecodeVisitorPluginOptionsJSON([]byte(jsonStr), v1.DecodeOptions{})
	if err != nil || tc.VisitorPluginOptions == nil {
		return emptySchema()
	}
	tc.Complete()
	return SchemaResponse{
		Schema:   buildGroup(reflect.TypeOf(tc.VisitorPluginOptions).Elem(), ""),
		Defaults: jsonDefault(tc.VisitorPluginOptions),
	}
}

var clientPluginTypes = []string{
	v1.PluginHTTP2HTTPS,
	v1.PluginHTTPProxy,
	v1.PluginHTTPS2HTTP,
	v1.PluginHTTPS2HTTPS,
	v1.PluginHTTP2HTTP,
	v1.PluginSocks5,
	v1.PluginStaticFile,
	v1.PluginUnixDomainSocket,
	v1.PluginTLS2Raw,
	v1.PluginVirtualNet,
}

var visitorPluginTypes = []string{
	v1.VisitorPluginVirtualNet,
}
