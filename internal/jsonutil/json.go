package jsonutil

import (
	"bytes"
	"encoding/json"
)

func Marshal(value any) ([]byte, error) {
	return encode(value, "")
}

func MarshalIndent(value any) ([]byte, error) {
	return encode(value, "  ")
}

func encode(value any, indent string) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if indent != "" {
		encoder.SetIndent("", indent)
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
