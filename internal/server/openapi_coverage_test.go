package server

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The OpenAPI document is the integration contract for external clients, so
// every registered route must be documented and every documented operation
// must exist. This test cross-checks the two without a YAML dependency: routes
// are taken from the "METHOD /path" pattern literals in this package's source,
// operations from the spec's path and method keys.

var (
	routeLiteralRe = regexp.MustCompile(`"(GET|POST|PUT|DELETE|PATCH) (/[^"]*)"`)
	specPathRe     = regexp.MustCompile(`^  (/\S*):\s*$`)
	specOpRe       = regexp.MustCompile(`^    (get|post|put|delete|patch):`)
	specTopLevelRe = regexp.MustCompile(`^[A-Za-z]`)
)

// registeredRoutes scans the non-test Go files of this package for route
// pattern literals, normalising the ServeMux exact-match suffix "{$}".
func registeredRoutes(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeLiteralRe.FindAllStringSubmatch(string(src), -1) {
			path := strings.TrimSuffix(m[2], "{$}")
			if path == "" {
				path = "/"
			}
			routes[m[1]+" "+path] = f
		}
	}
	if len(routes) == 0 {
		t.Fatal("no route literals found; the scan pattern no longer matches the router")
	}
	return routes
}

// documentedOperations line-parses the spec's paths section.
func documentedOperations(t *testing.T) map[string]int {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "api", "openapi", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ops := map[string]int{}
	inPaths, current := false, ""
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if specTopLevelRe.MatchString(line) {
			inPaths, current = strings.HasPrefix(line, "paths:"), ""
			continue
		}
		if !inPaths {
			continue
		}
		if m := specPathRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := specOpRe.FindStringSubmatch(line); m != nil && current != "" {
			key := strings.ToUpper(m[1]) + " " + current
			if prev, dup := ops[key]; dup {
				t.Errorf("openapi.yaml:%d: %s documented twice (first at line %d)", n, key, prev)
			}
			ops[key] = n
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatal("no operations found in openapi.yaml; the line parser no longer matches its layout")
	}
	return ops
}

func TestOpenAPICoversAllRoutes(t *testing.T) {
	routes := registeredRoutes(t)
	ops := documentedOperations(t)

	var missing, stale []string
	for r, file := range routes {
		if _, ok := ops[r]; !ok {
			missing = append(missing, r+" (registered in "+file+")")
		}
	}
	for op := range ops {
		if _, ok := routes[op]; !ok {
			stale = append(stale, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("routes missing from api/openapi/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("operations in api/openapi/openapi.yaml that no route registers:\n  %s", strings.Join(stale, "\n  "))
	}
}
