package main

import (
	"encoding/json"
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// readTOMLToMap reads a TOML file and returns it as a generic JSON-compatible
// map, using the exact same key convention that frp uses internally
// (TOML -> any -> JSON). This preserves whatever the user has written without
// applying defaults or dropping unknown fields.
func readTOMLToMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}, nil
	}
	return m, nil
}

// normalizeJSONNumbers converts json.Number values (produced by decoding the
// request body with UseNumber) into int64/float64 so that go-toml can marshal
// integers without a decimal point.
func normalizeJSONNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	case map[string]any:
		for k, vv := range t {
			t[k] = normalizeJSONNumbers(vv)
		}
		return t
	case []any:
		for i, vv := range t {
			t[i] = normalizeJSONNumbers(vv)
		}
		return t
	}
	return v
}

// stripNulls removes nil values (and empty maps) from a generic map so that
// "unset" fields are omitted from the generated TOML rather than written as
// null/empty tables.
func stripNulls(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			vv = stripNulls(vv)
			switch x := vv.(type) {
			case nil:
				delete(t, k)
			case map[string]any:
				if len(x) == 0 {
					delete(t, k)
				}
			}
		}
		return t
	case []any:
		out := make([]any, 0, len(t))
		for _, vv := range t {
			vv = stripNulls(vv)
			if vv != nil {
				out = append(out, vv)
			}
		}
		return out
	}
	return v
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// portsRangeSliceToString converts the JSON representation of allowPorts
// ([]any of {start,end,single}) into the human-friendly "1000-2000,3000" form.
func portsRangeSliceToString(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	prs := make([]types.PortsRange, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		pr := types.PortsRange{}
		if s, ok := toInt(m["single"]); ok && s > 0 {
			pr.Single = s
		} else {
			pr.Start, _ = toInt(m["start"])
			pr.End, _ = toInt(m["end"])
		}
		prs = append(prs, pr)
	}
	return types.PortsRangeSlice(prs).String()
}

// parsePortsRangeString converts "1000-2000,3000" into the generic JSON
// representation expected by TOML marshaling: []any of {start,end,single}.
func parsePortsRangeString(s string) []any {
	prs, err := types.NewPortsRangeSliceFromString(s)
	if err != nil {
		return nil
	}
	out := make([]any, 0, len(prs))
	for _, pr := range prs {
		m := map[string]any{}
		if pr.Single > 0 {
			m["single"] = pr.Single
		} else {
			m["start"] = pr.Start
			m["end"] = pr.End
		}
		out = append(out, m)
	}
	return out
}

// convertAllowPortsFromFrontend converts a frontend "allowPorts" string into
// the array-of-tables representation.
func convertAllowPortsFromFrontend(data map[string]any) {
	v, ok := data["allowPorts"]
	if !ok {
		return
	}
	if s, ok := v.(string); ok {
		if s == "" {
			delete(data, "allowPorts")
			return
		}
		data["allowPorts"] = parsePortsRangeString(s)
	}
}

// convertAllowPortsToFrontend converts the array-of-tables representation of
// "allowPorts" into a friendly string for editing.
func convertAllowPortsToFrontend(data map[string]any) {
	if v, ok := data["allowPorts"]; ok {
		if _, isStr := v.(string); !isStr {
			data["allowPorts"] = portsRangeSliceToString(v)
		}
	}
}

// marshalMapToTOML serializes a generic map into TOML after normalizing values.
func marshalMapToTOML(data map[string]any) ([]byte, error) {
	data = normalizeJSONNumbers(data).(map[string]any)
	convertAllowPortsFromFrontend(data)
	stripNulls(data)
	return toml.Marshal(data)
}

// loadServerConfig reads frps.toml and returns its content as a generic map
// ready for the frontend. An empty result means the file is absent/empty.
func loadServerConfig(path string) (map[string]any, error) {
	m, err := readTOMLToMap(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	convertAllowPortsToFrontend(m)
	return m, nil
}

func saveServerConfig(path string, data map[string]any) error {
	b, err := marshalMapToTOML(data)
	if err != nil {
		return err
	}
	if err := validateServerTOML(b); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func loadClientConfig(path string) (map[string]any, error) {
	m, err := readTOMLToMap(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func saveClientConfig(path string, data map[string]any) error {
	b, err := marshalMapToTOML(data)
	if err != nil {
		return err
	}
	if err := validateClientTOML(b); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func validateServerTOML(b []byte) error {
	cfg := &v1.ServerConfig{}
	if err := config.LoadConfigure(b, cfg, false); err != nil {
		return fmt.Errorf("invalid server configuration: %w", err)
	}
	return nil
}

func validateClientTOML(b []byte) error {
	cfg := &v1.ClientConfig{}
	if err := config.LoadConfigure(b, cfg, false); err != nil {
		return fmt.Errorf("invalid client configuration: %w", err)
	}
	return nil
}
