package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// errNoModel means the body is well-formed JSON but has no top-level
// "model" string — a request omni has no routing opinion about.
var errNoModel = errors.New("no top-level model field")

// modelField finds the byte range of the top-level "model" string value in
// an Anthropic Messages request body, along with its decoded value.
//
// It returns byte offsets rather than a decoded document on purpose. The
// alternative — unmarshal to a map, mutate, re-marshal — would reorder keys
// and renormalize every number and escape sequence in a body that routinely
// runs to 100KB+ of system prompt, tool schemas, and cache_control markers.
// internal-docs/05-constraints.md §1 is emphatic that omni must not perturb
// bytes it has no reason to touch; splicing one string value is the smallest
// edit that does the job, and everything outside [start,end) survives
// verbatim.
func modelField(body []byte) (start, end int, model string, err error) {
	// Validate the whole document before trusting any part of it.
	// json.Decoder streams, so it will happily hand back a "model" from
	// {"model":"x" with no closing brace — and a truncated request that gets
	// its credential swapped and forwarded to a third party is the worst
	// outcome available here. One O(n) scan buys the guarantee that what we
	// route is what the agent actually finished writing.
	if !json.Valid(body) {
		return 0, 0, "", errors.New("body is not valid JSON")
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return 0, 0, "", fmt.Errorf("decode: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return 0, 0, "", errors.New("body is not a JSON object")
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, 0, "", fmt.Errorf("decode key: %w", err)
		}
		key, _ := keyTok.(string)
		// InputOffset is now just past the key string, before the colon.
		afterKey := int(dec.InputOffset())

		valTok, err := dec.Token()
		if err != nil {
			return 0, 0, "", fmt.Errorf("decode value for %q: %w", key, err)
		}

		if d, ok := valTok.(json.Delim); ok && (d == '{' || d == '[') {
			// Token() descends into composites; consume to the matching
			// close so the next iteration sees the following top-level key.
			if err := skipComposite(dec); err != nil {
				return 0, 0, "", fmt.Errorf("skip %q: %w", key, err)
			}
			continue
		}

		if key != "model" {
			continue
		}
		s, ok := valTok.(string)
		if !ok {
			return 0, 0, "", fmt.Errorf("model is %T, want string", valTok)
		}
		// Walk forward from the key to the opening quote of the value,
		// stepping over the colon and any whitespace around it.
		vs := afterKey
		for vs < len(body) && isJSONSpace(body[vs]) {
			vs++
		}
		if vs >= len(body) || body[vs] != ':' {
			return 0, 0, "", errors.New("malformed object: no colon after \"model\"")
		}
		vs++
		for vs < len(body) && isJSONSpace(body[vs]) {
			vs++
		}
		if vs >= len(body) || body[vs] != '"' {
			return 0, 0, "", errors.New("malformed object: model value is not a string literal")
		}
		return vs, int(dec.InputOffset()), s, nil
	}
	return 0, 0, "", errNoModel
}

// skipComposite consumes tokens until the object or array whose opening
// delimiter was just read is closed.
func skipComposite(dec *json.Decoder) error {
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// replaceModel returns body with its top-level "model" value replaced by
// newModel, leaving every other byte untouched. The returned slice is
// always newly allocated; body is never modified in place.
func replaceModel(body []byte, newModel string) ([]byte, error) {
	start, end, _, err := modelField(body)
	if err != nil {
		return nil, err
	}
	quoted, err := json.Marshal(newModel)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(body)-(end-start)+len(quoted))
	out = append(out, body[:start]...)
	out = append(out, quoted...)
	out = append(out, body[end:]...)
	return out, nil
}
