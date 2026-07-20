package provider

import (
	"encoding/json"
	"sort"
)

func CanonicalizeSchema(raw json.RawMessage) json.RawMessage {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return canonical
}

func SortKeys(raw json.RawMessage) json.RawMessage {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		sorted[k] = obj[k]
	}
	result, _ := json.Marshal(sorted)
	return result
}
