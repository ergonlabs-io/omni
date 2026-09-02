package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplaceModelPreservesEverythingElse(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"simple",
			`{"model":"a","max_tokens":16}`,
			`{"model":"b","max_tokens":16}`,
		},
		{
			"model not first",
			`{"max_tokens":16,"model":"a"}`,
			`{"max_tokens":16,"model":"b"}`,
		},
		{
			// Whitespace, key order, and number formatting must all survive:
			// re-marshalling a decoded map would normalize every one of them.
			"pretty printed",
			"{\n  \"max_tokens\" : 16,\n  \"model\" : \"a\"\n}",
			"{\n  \"max_tokens\" : 16,\n  \"model\" : \"b\"\n}",
		},
		{
			// A nested "model" key must not be mistaken for the top-level one.
			"nested model key is ignored",
			`{"metadata":{"model":"nested"},"model":"a"}`,
			`{"metadata":{"model":"nested"},"model":"b"}`,
		},
		{
			"nested arrays and objects are skipped",
			`{"messages":[{"role":"user","content":[{"type":"text","text":"x"}]}],"model":"a"}`,
			`{"messages":[{"role":"user","content":[{"type":"text","text":"x"}]}],"model":"b"}`,
		},
		{
			"escapes and unicode elsewhere survive byte for byte",
			`{"system":"a \"quoted\" é \\ end","model":"a"}`,
			`{"system":"a \"quoted\" é \\ end","model":"b"}`,
		},
		{
			"cache_control markers are untouched",
			`{"model":"a","system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}`,
			`{"model":"b","system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := replaceModel([]byte(tc.in), "b")
			if err != nil {
				t.Fatalf("replaceModel: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestReplaceModelQuotesTarget(t *testing.T) {
	// A model name needing JSON escaping must not be spliced in raw.
	got, err := replaceModel([]byte(`{"model":"a"}`), `we"ird\`)
	if err != nil {
		t.Fatalf("replaceModel: %v", err)
	}
	var m struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, got)
	}
	if m.Model != `we"ird\` {
		t.Errorf("model = %q, want the exact target", m.Model)
	}
}

func TestReplaceModelDoesNotMutateInput(t *testing.T) {
	in := []byte(`{"model":"aaaa"}`)
	orig := string(in)
	if _, err := replaceModel(in, "b"); err != nil {
		t.Fatalf("replaceModel: %v", err)
	}
	if string(in) != orig {
		t.Errorf("input was mutated: %s", in)
	}
}

func TestModelFieldErrors(t *testing.T) {
	cases := []struct {
		name, in, wantErr string
	}{
		{"no model", `{"max_tokens":16}`, "no top-level model"},
		{"truncated", `{"model":"a"`, "not valid JSON"},
		{"trailing garbage", `{"model":"a"} oops`, "not valid JSON"},
		{"not an object", `["model"]`, "not a JSON object"},
		{"model is a number", `{"model":5}`, "want string"},
		{"empty", ``, "not valid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := modelField([]byte(tc.in))
			if err == nil {
				t.Fatalf("want an error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestModelFieldReadsValue(t *testing.T) {
	_, _, model, err := modelField([]byte(`{"max_tokens":1,"model":"claude-haiku-4-5-20251001"}`))
	if err != nil {
		t.Fatalf("modelField: %v", err)
	}
	if model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q", model)
	}
}
