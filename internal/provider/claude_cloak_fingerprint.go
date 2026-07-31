package provider

import (
	"bytes"
	"encoding/json"
)

// OmniRoute CLI_FINGERPRINTS.claude bodyFieldOrder / headerOrder.
var claudeBodyFieldOrder = []string{
	"model", "messages", "system", "tools", "tool_choice", "metadata",
	"max_tokens", "temperature", "thinking", "context_management",
	"output_config", "stream",
}

var claudeHeaderOrder = []string{
	"Accept", "Authorization", "Content-Type", "User-Agent",
	"X-Claude-Code-Session-Id",
	"X-Stainless-Arch", "X-Stainless-Lang", "X-Stainless-OS",
	"X-Stainless-Package-Version", "X-Stainless-Retry-Count",
	"X-Stainless-Runtime", "X-Stainless-Runtime-Version", "X-Stainless-Timeout",
	"anthropic-beta", "anthropic-dangerous-direct-browser-access", "anthropic-version",
	"x-app", "x-client-request-id",
	"Connection", "Host", "Accept-Encoding", "Content-Length",
}

// orderFields returns a new map with keys ordered for JSON encoding.
func orderFields(obj map[string]interface{}, fieldOrder []string) *orderedMap {
	om := &orderedMap{keys: make([]string, 0, len(obj)), values: obj}
	seen := map[string]struct{}{}
	for _, k := range fieldOrder {
		if _, ok := obj[k]; ok {
			om.keys = append(om.keys, k)
			seen[k] = struct{}{}
		}
	}
	for k := range obj {
		if _, ok := seen[k]; ok {
			continue
		}
		if k == "_toolNameMap" {
			continue // internal, never send upstream
		}
		om.keys = append(om.keys, k)
	}
	return om
}

type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func (o *orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, k := range o.keys {
		v, ok := o.values[k]
		if !ok {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalClaudeFingerprintBody(body map[string]interface{}) ([]byte, error) {
	delete(body, "_toolNameMap")
	delete(body, "_claudeCodeRequiresLowercaseToolNames")
	return json.Marshal(orderFields(body, claudeBodyFieldOrder))
}
