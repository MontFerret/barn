package registrydist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Decode strictly decodes one distribution document. It rejects duplicate
// object keys, trailing values, and fields unknown to the target wire type.
func Decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	document, err := decodeValue(decoder)
	if err != nil {
		return err
	}

	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("document must contain exactly one JSON value")
		}

		return err
	}

	normalized, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("normalize document: %w", err)
	}

	typed := json.NewDecoder(bytes.NewReader(normalized))
	typed.DisallowUnknownFields()
	if err := typed.Decode(target); err != nil {
		return err
	}

	return nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}

	switch delimiter {
	case '{':
		value := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}

			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key must be a string")
			}

			if _, exists := value[key]; exists {
				return nil, fmt.Errorf("duplicate object key %q", key)
			}

			item, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}

			value[key] = item
		}

		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("unterminated object")
		}

		return value, nil
	case '[':
		value := make([]any, 0)
		for decoder.More() {
			item, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}

			value = append(value, item)
		}

		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("unterminated array")
		}

		return value, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}
