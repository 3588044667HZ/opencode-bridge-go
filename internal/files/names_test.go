package files

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	valid := []string{
		"a",
		"A1",
		"测试.txt",
		"a.b-c_d",
		"COM10",
		"conifer",
		"1file",
		strings.Repeat("a", 128),
	}
	for _, name := range valid {
		if !ValidName(name) {
			t.Fatalf("expected valid name: %q", name)
		}
	}
	invalid := []string{
		"",
		".",
		"..",
		" a",
		"a ",
		"a/b",
		`a\b`,
		"a:b",
		"a*b",
		"a?b",
		"a\"b",
		"a<b",
		"a>b",
		"a|b",
		"a%20",
		"a.",
		"a.part",
		"A.PART",
		"CON",
		"con.txt",
		"NUL.tar.gz",
		"LPT9.log",
		"COM1",
		"_a",
		"-a",
		".a",
		"a[b]",
		"a+b",
		"a b",
		strings.Repeat("a", 129),
	}
	for _, name := range invalid {
		if ValidName(name) {
			t.Fatalf("expected invalid name: %q", name)
		}
	}
}
