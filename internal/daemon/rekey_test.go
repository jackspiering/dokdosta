package daemon

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackspiering/dokdosta/internal/store"
)

// Every meta row the daemon writes is either re-keyed (MetaKeyed, MetaValues) or holds no state key (metaNoKey): a new
// record keyed by a state key that is registered nowhere fails here, not after the next host-ID change (plan v3.13 item 12).
func TestEveryMetaRowIsRekeyedOrHoldsNoKey(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	consts := map[string]string{}
	var parsed []*ast.File
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, af)
		ast.Inspect(af, func(n ast.Node) bool {
			if vs, ok := n.(*ast.ValueSpec); ok {
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							consts[name.Name], _ = strconv.Unquote(bl.Value)
						}
					}
				}
			}
			return true
		})
	}
	known := slices.Concat(MetaKeyed, slices.Collect(maps.Keys(MetaValues)), metaNoKey)
	calls := 0
	for _, af := range parsed {
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "SetMeta" {
				return true
			}
			arg := call.Args[0]
			for {
				b, ok := arg.(*ast.BinaryExpr)
				if !ok {
					break
				}
				arg = b.X
			}
			id, ok := arg.(*ast.Ident)
			v, isConst := consts[id.Name]
			if !ok || !isConst {
				t.Errorf("%s: SetMeta with a key that does not start with a constant", fset.Position(call.Pos()))
				return true
			}
			calls++
			if !slices.Contains(known, v) {
				t.Errorf("%s: meta prefix %q is neither re-keyed nor listed as holding no key", fset.Position(call.Pos()), v)
			}
			return true
		})
	}
	if calls < 5 {
		t.Fatalf("found %d SetMeta calls: the test does not see the sources", calls)
	}
}

// rekeyMeta is what cmd/dokdosta passes to store.Rekey.
func rekeyMeta() store.RekeyMeta {
	m := store.RekeyMeta{Keyed: MetaKeyed}
	for _, p := range slices.Sorted(maps.Keys(MetaValues)) {
		m.Values = append(m.Values, store.ValueRekey{Prefix: p, Rewrite: MetaValues[p]})
	}
	return m
}

// A tag move the ledger wrote, re-keyed by the store with the daemon's rewrite and read back by the ledger.
func TestRekeyMovesTheKeysOfATagMoveTheLedgerReadsBack(t *testing.T) {
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	newMoveLedger(st).record(tagMove{Ref: "web:1", Key: "old/p/web/1", New: "n", Followers: []mover{{Key: "old/q/api/1"}, {Key: "new/r/job/1"}}})
	if _, err := st.Rekey("new", rekeyMeta()); err != nil {
		t.Fatal(err)
	}
	m, ok := newMoveLedger(st).of("web:1")
	if !ok || m.Key != "new/p/web/1" || len(m.Followers) != 2 || m.Followers[0].Key != "new/q/api/1" || m.Followers[1].Key != "new/r/job/1" {
		t.Fatalf("%+v", m)
	}
}

// The rewrite keeps what it does not know, and moves the followers when the mover's key is already current.
func TestRekeyTagMoveKeepsUnknownFields(t *testing.T) {
	rk := func(k string) string { return strings.Replace(k, "old/", "new/", 1) }
	v, ok := rekeyTagMove(`{"ref":"a:1","key":"new/p/web/1","followers":[{"key":"old/p/side/1","note":"kept"}],"x":1}`, rk)
	var r struct {
		Followers []struct{ Key, Note string } `json:"followers"`
		X         int                          `json:"x"`
	}
	if !ok || json.Unmarshal([]byte(v), &r) != nil || r.Followers[0].Key != "new/p/side/1" || r.Followers[0].Note != "kept" || r.X != 1 {
		t.Fatalf("%v %s", ok, v)
	}
	if _, ok := rekeyTagMove(`{"key":"new/p/web/1"}`, rk); ok {
		t.Error("nothing to move is no change")
	}
	if v, ok := rekeyTagMove(`not json`, rk); ok || v != "not json" {
		t.Error("a value it cannot read stays")
	}
}
