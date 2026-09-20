package rulescore_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

const (
	quickstartMarker = "<!-- rulescore-example:quickstart -->"
	outputMarker     = "<!-- rulescore-example:output -->"
)

func TestREADMEQuickstartMatchesExample(t *testing.T) {
	readme := readFile(t, "README.md")
	exampleSource := readFile(t, "example_test.go")

	readmeCode := markedFence(t, readme, quickstartMarker, "go")
	readmeOutput := markedFence(t, readme, outputMarker, "")
	exampleCode, exampleOutput := exampleParts(t, exampleSource)

	compareLines(t, "README quickstart code does not match Example body", readmeCode, exampleCode)
	compareLines(t, "README quickstart output does not match Example Output comment", readmeOutput, exampleOutput)
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func markedFence(t *testing.T, readme []byte, marker, language string) string {
	t.Helper()

	if count := bytes.Count(readme, []byte(marker)); count != 1 {
		t.Fatalf("README marker %q occurs %d times, want exactly once", marker, count)
	}

	after, _ := bytes.CutPrefix(readme[bytes.Index(readme, []byte(marker)):], []byte(marker))
	lines := strings.Split(string(after), "\n")
	if lines[0] != "" {
		t.Fatalf("README marker %q must be on a line by itself", marker)
	}
	opening := "```" + language
	line := 1
	for line < len(lines) && strings.TrimSpace(lines[line]) == "" {
		line++
	}
	if line == len(lines) || lines[line] != opening {
		t.Fatalf("README marker %q must be followed by %q", marker, opening)
	}

	start := line + 1
	for line = start; line < len(lines); line++ {
		if lines[line] == "```" {
			return strings.Join(lines[start:line], "\n")
		}
	}
	t.Fatalf("README fence after marker %q is not closed", marker)
	return ""
}

func exampleParts(t *testing.T, source []byte) (string, string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example_test.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var example *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "Example" {
			if example != nil {
				t.Fatal("example_test.go contains more than one Example function")
			}
			example = fn
		}
	}
	if example == nil {
		t.Fatal("example_test.go does not contain Example")
	}

	var output *ast.CommentGroup
	for _, comments := range file.Comments {
		if comments.Pos() <= example.Body.Lbrace || comments.End() >= example.Body.Rbrace {
			continue
		}
		if len(comments.List) > 0 && comments.List[0].Text == "// Output:" {
			if output != nil {
				t.Fatal("Example contains more than one Output comment")
			}
			output = comments
		}
	}
	if output == nil {
		t.Fatal("Example does not contain an Output comment")
	}

	bodyStart := fset.PositionFor(example.Body.Lbrace, false).Offset + 1
	outputStart := fset.PositionFor(output.Pos(), false).Offset
	lineStart := bytes.LastIndexByte(source[:outputStart], '\n') + 1
	code := dedentExampleBody(t, string(source[bodyStart:lineStart]))

	outputLines := make([]string, 0, len(output.List)-1)
	for _, comment := range output.List[1:] {
		line := strings.TrimPrefix(comment.Text, "//")
		line = strings.TrimPrefix(line, " ")
		outputLines = append(outputLines, line)
	}
	return code, strings.Join(outputLines, "\n")
}

func dedentExampleBody(t *testing.T, body string) string {
	t.Helper()

	if !strings.HasPrefix(body, "\n") {
		t.Fatal("Example body must start on the line after its opening brace")
	}
	body = strings.TrimPrefix(body, "\n")
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatal("Example body must have one blank line before its Output comment")
	}
	body = strings.TrimSuffix(body, "\n\n")

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "\t") {
			lines[i] = strings.TrimPrefix(line, "\t")
		}
	}
	return strings.Join(lines, "\n")
}

func compareLines(t *testing.T, message, readme, example string) {
	t.Helper()

	if readme == example {
		return
	}
	readmeLines := strings.Split(readme, "\n")
	exampleLines := strings.Split(example, "\n")
	for i := 0; i < len(readmeLines) || i < len(exampleLines); i++ {
		if i >= len(readmeLines) {
			t.Errorf("%s at line %d\nREADME: <missing>\nExample: %q", message, i+1, exampleLines[i])
			return
		}
		if i >= len(exampleLines) {
			t.Errorf("%s at line %d\nREADME: %q\nExample: <missing>", message, i+1, readmeLines[i])
			return
		}
		if readmeLines[i] != exampleLines[i] {
			t.Errorf("%s at line %d\nREADME: %q\nExample: %q", message, i+1, readmeLines[i], exampleLines[i])
			return
		}
	}
}
