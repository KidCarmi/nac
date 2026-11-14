package change

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Slice A is review-only. These tests pin the structural guarantees that the
// change-request service cannot change the active policy, a session, a CoA
// or a switch, independently of the behavioural no-side-effect test.

// PolicyReader is the only policy capability the service holds; it must not
// grow a publish, restore or activation method.
func TestPolicyReaderCannotChangePolicy(t *testing.T) {
	typ := reflect.TypeOf((*PolicyReader)(nil)).Elem()
	allowed := map[string]bool{"Active": true, "Version": true, "RecordedCases": true, "PreviewWindow": true, "Draft": true,
		"Environment": true, "ValidateAgainst": true}
	for i := 0; i < typ.NumMethod(); i++ {
		if name := typ.Method(i).Name; !allowed[name] {
			t.Fatalf("PolicyReader gained method %s: review-only change requests must not be able to change policy", name)
		}
	}
}

// The package must not import the code that publishes policy or talks to
// sessions, CoA or switches.
func TestNoImportOfEnforcementPaths(t *testing.T) {
	forbidden := []string{
		"/internal/pilot", "/internal/radius", "/internal/radiusauth", "/internal/storage", "/internal/server", "/internal/access",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if strings.Contains(path, "github.com/kidcarmi/nac"+bad) {
					t.Fatalf("%s imports %s: the change-request service must stay review-only", f, path)
				}
			}
		}
	}
}
