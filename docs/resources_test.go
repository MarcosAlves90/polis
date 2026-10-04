package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCommandHelpEntriesAreCanonicalAndIndependent(t *testing.T) {
	entries := CommandHelpEntries()
	if len(entries) != 18 {
		t.Fatalf("command count=%d want=18", len(entries))
	}
	guide := strings.ReplaceAll(usageGuide, "\r\n", "\n")
	for _, entry := range entries {
		if !strings.Contains(guide, entry.Instructions) || !strings.Contains(entry.Instructions, entry.Usage) || !strings.Contains(entry.Instructions, entry.Summary) {
			t.Errorf("entry %s does not derive from the canonical usage guide", entry.Name)
		}
	}
	entries[0].Instructions = "mutated"
	if CommandHelpEntries()[0].Instructions == "mutated" {
		t.Fatal("caller changed embedded command help")
	}
}

func TestCommandHelpNormalizesCRLF(t *testing.T) {
	guide := strings.ReplaceAll(usageGuide, "\r\n", "\n")
	want, err := parseCommandHelp(guide)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseCommandHelp(strings.ReplaceAll(guide, "\n", "\r\n"))
	if err != nil {
		t.Fatalf("CRLF checkout rejected: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("CRLF command count=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CRLF help differs for %s", want[i].Name)
		}
	}
}

func TestParseCommandHelpRejectsInvalidSource(t *testing.T) {
	entry := CommandHelpEntries()[0]
	block := helpStart + entry.Name + " -->\n```text\n" + entry.Instructions + helpEnd
	cases := map[string]string{
		"empty":         "no command blocks",
		"duplicate":     block + block,
		"empty name":    strings.Replace(block, "help -->", " -->", 1),
		"invalid name":  strings.Replace(block, "help -->", "bad name -->", 1),
		"bad fence":     strings.Replace(block, "```text", "```json", 1),
		"missing end":   strings.Replace(block, helpEnd, "", 1),
		"nested":        strings.Replace(block, helpEnd, block+helpEnd, 1),
		"missing field": strings.Replace(block, "Prerequisites:\n  ", "", 1),
		"empty field":   strings.Replace(block, "Purpose:\n  "+entry.Summary, "Purpose:\n  ", 1),
		"wrong usage":   strings.Replace(block, entry.Usage, "polis unknown [command]", 1),
	}
	for name, guide := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCommandHelp(guide); err == nil {
				t.Fatal("invalid canonical source accepted")
			}
		})
	}
}

// Compare the canonical guide with the actual CLI dispatch and flag declarations,
// so adding a command or option without operational guidance fails local/CI tests.
func TestCommandHelpCoversDispatchAndOptions(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "cmd", "polis", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		if decl, ok := node.(*ast.ValueSpec); ok && len(decl.Names) == 1 && len(decl.Values) == 1 {
			if lit, ok := decl.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				constants[decl.Names[0].Name], _ = strconv.Unquote(lit.Value)
			}
		}
		return true
	})
	stringValue := func(expr ast.Expr) string {
		switch expr := expr.(type) {
		case *ast.BasicLit:
			if expr.Kind == token.STRING {
				value, _ := strconv.Unquote(expr.Value)
				return value
			}
		case *ast.Ident:
			return constants[expr.Name]
		}
		return ""
	}
	entries := make(map[string]CommandHelpEntry)
	for _, entry := range CommandHelpEntries() {
		entries[entry.Name] = entry
	}
	dispatched := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name == "run" {
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if clause, ok := node.(*ast.CaseClause); ok {
					for _, expr := range clause.List {
						name := stringValue(expr)
						if name != "" && !strings.HasPrefix(name, "-") {
							dispatched[name] = true
						}
					}
				}
				return true
			})
		}
		command := ""
		var options []string
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "signatureFlags" {
				options = append(options, "signature", "trusted-key")
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if receiver.Name == "flag" && selector.Sel.Name == "NewFlagSet" {
				command = stringValue(call.Args[0])
			}
			if receiver.Name == "fs" {
				switch selector.Sel.Name {
				case "String", "Bool", "Float64":
					options = append(options, stringValue(call.Args[0]))
				case "Var":
					options = append(options, stringValue(call.Args[1]))
				}
			}
			return true
		})
		if command != "" {
			entry, ok := entries[command]
			if !ok {
				t.Errorf("flag parser %s lacks canonical help", command)
			}
			for _, option := range options {
				if option == "" || !strings.Contains(entry.Instructions, "--"+option) {
					t.Errorf("%s help lacks declared option --%s", command, option)
				}
			}
		}
	}
	for name := range dispatched {
		if _, ok := entries[name]; !ok {
			t.Errorf("dispatched command %s lacks help", name)
		}
	}
	for name := range entries {
		if !dispatched[name] {
			t.Errorf("documented command %s lacks dispatch", name)
		}
	}
}
