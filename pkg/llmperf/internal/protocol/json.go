package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var (
	ErrMalformed    = errors.New("malformed protocol event")
	ErrUnsupported  = errors.New("unsupported protocol")
	ErrNestingLimit = errors.New("JSON nesting exceeds limit")
)

func decodeJSON(data []byte, maxDepth int, target any) error {
	if err := validateDepth(data, maxDepth); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return errors.Join(ErrMalformed, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return ErrMalformed
	} else if !errors.Is(err, io.EOF) {
		return errors.Join(ErrMalformed, err)
	}
	return nil
}

func validateDepth(data []byte, maxDepth int) error {
	depth := 0
	inString := false
	escaped := false
	for _, b := range data {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch b {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if maxDepth > 0 && depth > maxDepth {
				return ErrNestingLimit
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return ErrMalformed
			}
		}
	}
	if depth != 0 || inString || escaped {
		return ErrMalformed
	}
	return nil
}

func nonEmptyString(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var value string
	return json.Unmarshal(raw, &value) == nil && value != ""
}

func nonNullObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("{}"))
}
