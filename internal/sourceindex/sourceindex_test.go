package sourceindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSymbolAt(t *testing.T) {
	cases := []struct {
		lang, line, want string
	}{
		{"c", "static int parse_header(struct req *r, const char *buf)", "parse_header"},
		{"c", "int main(int argc, char **argv) {", "main"},
		{"c", "parse_header(struct req *r)", "parse_header"}, // BSD style, type on the previous line
		{"c", "char *strdup_safe(const char *s)", "strdup_safe"},
		{"c", "    foo(a,", ""},                    // indented: a statement
		{"c", "int x = compute(3)", ""},            // initializer
		{"c", "MODULE_LICENSE(\"GPL\");", ""},      // macro call
		{"c", "#define MAX(a, b) ((a) > (b))", ""}, // preprocessor
		{"c", "if (x) {", ""},
		{"cpp", "void Parser::reset()", "Parser::reset"},
		{"go", "func (p *Parser) Reset() error {", "Reset"},
		{"go", "func parse(b []byte) {", "parse"},
		{"python", "    async def handle(self, req):", "handle"},
		{"rust", "pub(crate) unsafe fn copy_into(dst: &mut [u8]) {", "copy_into"},
		{"javascript", "export async function load(path) {", "load"},
		{"java", "    public static byte[] decode(String s) {", "decode"},
		{"java", "    return decode(s);", ""},
		{"kotlin", "    fun parse(input: String): Node {", "parse"},
		{"ruby", "  def self.load!(path)", "load!"},
	}
	for _, c := range cases {
		got, _ := SymbolAt(c.lang, c.line)
		if got != c.want {
			t.Errorf("SymbolAt(%s, %q) = %q, want %q", c.lang, c.line, got, c.want)
		}
	}
}

func TestChunkFile(t *testing.T) {
	src := strings.Split(`#include <stdio.h>

static int a(void)
{
	return 1;
}

int b(int x)
{
	return x;
}`, "\n")
	got := ChunkFile("x.c", "c", src)
	want := []Chunk{{"x.c", "c", "a", 3, 7}, {"x.c", "c", "b", 8, 11}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("chunks = %+v, want %+v", got, want)
	}

	long := make([]string, 300)
	long[0] = "func f() {"
	if c := ChunkFile("y.go", "go", long); len(c) != 1 || c[0].End != MaxChunkLines {
		t.Fatalf("long function chunk = %+v", c)
	}

	header := make([]string, 250)
	if c := ChunkFile("z.h", "c", header); len(c) != 3 || c[2].Start != 241 || c[2].End != 250 || c[0].Symbol != "" {
		t.Fatalf("window chunks = %+v", c)
	}
}

func TestWalkSkipsUnsafeAndIrrelevantFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/a.c", "int a(void)\r\n{\r\n}\r\n")
	write("src/notes.txt", "not code")
	write("src/bin.c", "int\x00x")
	write(".git/hooks/x.c", "int hidden(void)")
	outside := filepath.Join(t.TempDir(), "secret.c")
	if err := os.WriteFile(outside, []byte("int secret(void)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "src", "link.c")); err != nil {
		t.Fatal(err)
	}

	var seen []string
	if _, err := Walk(root, func(f File) error {
		seen = append(seen, f.Path)
		if f.Path == "src/a.c" && (len(f.Lines) != 3 || strings.HasSuffix(f.Lines[0], "\r")) {
			t.Errorf("CRLF not normalised: %q", f.Lines)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, ",") != "src/a.c" {
		t.Fatalf("walked %v, want only src/a.c", seen)
	}
}
