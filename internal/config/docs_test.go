package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The reference pages are checked against the code, in both directions, so that adding a variable or a label without
// documenting it (or documenting one that does not exist) fails the build.

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../docs/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func uniq(re *regexp.Regexp, src string) []string {
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		seen[m[1]] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEveryVariableAndLabelIsDocumented(t *testing.T) {
	code, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	vars := uniq(regexp.MustCompile(`(?:take|dur)\("(DOKDOSTA_[A-Z_]+)"`), string(code))
	if len(vars) < 25 {
		t.Fatalf("found only %d variables in config.go: %v (did the parser move?)", len(vars), vars)
	}
	doc := readDoc(t, "configuration.md")
	for _, v := range vars {
		if !strings.Contains(doc, "`"+v+"`") {
			t.Errorf("docs/configuration.md does not document %s", v)
		}
	}
	// And the other way: a row of the tables names a variable that exists.
	known := map[string]bool{}
	for _, v := range vars {
		known[v] = true
	}
	for _, v := range uniq(regexp.MustCompile("(?m)^\\| `(DOKDOSTA_[A-Z_]+)` \\|"), doc) {
		if !known[v] {
			t.Errorf("docs/configuration.md documents %s, which config.go does not read", v)
		}
	}

	labels, err := os.ReadFile("labels.go")
	if err != nil {
		t.Fatal(err)
	}
	keys := uniq(regexp.MustCompile(`Label[A-Za-z]+\s*=\s*"(dokdosta\.[a-z.-]+)"`), string(labels))
	if len(keys) != 6 {
		t.Fatalf("found %d labels in labels.go: %v", len(keys), keys)
	}
	ldoc := readDoc(t, "labels.md")
	for _, k := range keys {
		if !strings.Contains(ldoc, "`"+k+"`") {
			t.Errorf("docs/labels.md does not document %s", k)
		}
	}
	for _, k := range uniq(regexp.MustCompile("(?m)^\\| `(dokdosta\\.[a-z.-]+)` \\|"), ldoc) {
		found := false
		for _, c := range keys {
			found = found || c == k
		}
		if !found {
			t.Errorf("docs/labels.md documents %s, which labels.go does not read", k)
		}
	}
}

// slug is GitHub's heading anchor: lower case, punctuation dropped (hyphens and underscores kept), spaces to hyphens.
func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	var b strings.Builder
	for _, r := range h {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "#") {
			out[slug(strings.TrimLeft(line, "# "))] = true
		}
	}
	return out
}

// Every relative link in the README, the top-level pages and docs/ points at a file that exists and, if it names a heading,
// at a heading that exists.
func TestMarkdownLinksResolve(t *testing.T) {
	var files []string
	for _, pat := range []string{"../../*.md", "../../docs/*.md", "../../examples/*/*.md"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 10 {
		t.Fatalf("only %d markdown files found", len(files))
	}
	link := regexp.MustCompile(`\]\(([^)\s]+)\)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		body := regexp.MustCompile("(?s)```.*?```").ReplaceAllString(string(b), "")
		for _, m := range link.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, frag, _ := strings.Cut(target, "#")
			dest := f
			if path != "" {
				dest = filepath.Dir(f) + "/" + path
				if _, err := os.Stat(dest); err != nil {
					t.Errorf("%s: link %q: no such file", f, target)
					continue
				}
			}
			if frag != "" && strings.HasSuffix(dest, ".md") && !anchors(t, dest)[frag] {
				t.Errorf("%s: link %q: no heading #%s in %s", f, target, frag, dest)
			}
		}
	}
}
