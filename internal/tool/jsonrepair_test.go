package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

type repairCase struct {
	name  string
	in    string
	want  string // expected repaired JSON (compared as decoded values)
	valid bool   // whether repair must succeed
}

var repairCases = []repairCase{
	{"valid-passthrough", `{"file_path":"C:\foo"}`, `{"file_path":"C:\foo"}`, true},
	{"trailing-comma-object", `{"a":1,}`, `{"a":1}`, true},
	{"trailing-comma-array", `{"a":[1,2,]}`, `{"a":[1,2]}`, true},
	{"trailing-comma-nested", `{"a":{"b":1,},}`, `{"a":{"b":1}}`, true},
	{"truncated-object", `{"file_path":"C:\x`, `{"file_path":"C:\\x"}`, true},
	{"truncated-object-win-path", `{"file_path":"C:\Users\2167`, `{"file_path":"C:\\Users\\2167"}`, true},
	{"truncated-string", `{"a":"unterminated`, `{"a":"unterminated"}`, true},
	{"missing-closer-nested", `{"a":{"b":1}`, `{"a":{"b":1}}`, true},
	{"truncated-array", `{"a":[1,2`, `{"a":[1,2]}`, true},
	{"truncated-array-deep", `{"a":[[1,2,{"b":3}]`, `{"a":[[1,2,{"b":3}]]}`, true},
	{"code-fence", "```json\n{\"a\":1}\n```", `{"a":1}`, true},
	{"trailing-prose", `{"a":1} done.`, `{"a":1}`, true},
	{"leading-prose", `The args are {"a":1}`, `{"a":1}`, true},
	{"single-quoted", `{'a':'b'}`, `{"a":"b"}`, true},
	{"single-quoted-inner-dquote", `{'a':'say "hi"'}`, `{"a":"say \"hi\""}`, true},
	{"bare-keys", `{a:1,b:2}`, `{"a":1,"b":2}`, true},
	{"bare-key-spaced", `{ file_path: "x" }`, `{"file_path":"x"}`, true},
	{"nan-value", `{"t":NaN}`, `{"t":null}`, true},
	{"infinity-value", `{"t":Infinity}`, `{"t":null}`, true},
	{"negative-infinity", `{"t":-Infinity}`, `{"t":null}`, true},
	{"curly-quote-delims", "{\u201Ca\u201D:\u201Cb\u201D}", `{"a":"b"}`, true},
	{"single-quote-escape", `{'a':'it\'s'}`, `{"a":"it\"s"}`, true},
	{"empty-object", `{}`, `{}`, true},
	{"whitespace-wrapped", "  \n {\"a\":1} \t ", `{"a":1}`, true},
	{"bom-prefixed", "\uFEFF{\"a\":1}", `{"a":1}`, true},
	{"array-root-truncated", `[1,2`, `[1,2]`, true},
	{"two-json-values", `{"a":1}{"b":2}`, `{"a":1}`, true},
	{"brackets-in-string", `{"a":"x}y",}`, `{"a":"x}y"}`, true},
}

func TestRepairToolArgs(t *testing.T) {
	passed := 0
	for _, c := range repairCases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RepairToolArgs([]byte(c.in))
			if !c.valid {
				if err == nil {
					t.Fatalf("expected repair failure, got %q", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("repair failed: %v", err)
			}
			if !json.Valid(out) {
				t.Fatalf("repaired output is not valid JSON: %q", out)
			}
			var got, want any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal repaired: %v (%q)", err, out)
			}
			if err := json.Unmarshal([]byte(c.want), &want); err != nil {
				t.Fatalf("bad want fixture: %v", err)
			}
			if !jsonEqual(got, want) {
				t.Fatalf("repaired = %q, want %q", out, c.want)
			}
			passed++
		})
	}
	if passed < len(repairCases) {
		t.Fatalf("repaired %d/%d cases", passed, len(repairCases))
	}
	rate := float64(passed) / float64(len(repairCases))
	if rate < 0.8 {
		t.Fatalf("repair success rate %.0f%% below 80%% acceptance", rate*100)
	}
}

func TestRepairToolArgsRejectsUnrepairable(t *testing.T) {
	for _, in := range []string{
		``,
		`not json at all`,
		`{a:` + strings.Repeat("x", 20), // bare key that never reaches ':'
		`{"a":1,,}`,                     // double trailing comma
		`{"a": `,
	} {
		if out, err := RepairToolArgs([]byte(in)); err == nil {
			t.Fatalf("input %q: expected failure, got %q", in, out)
		}
	}
}

func jsonEqual(a, b any) bool {
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(aj) == string(bj)
}
