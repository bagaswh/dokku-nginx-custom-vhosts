package main

import (
	"strings"
	"testing"
	"text/template"

	"github.com/gliderlabs/sigil"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestMd5sum(t *testing.T) {
	// echo -n "Hello world!" | md5sum
	got := md5sum("Hello world!")
	want := "86fb269d190d2c85f6e0468ceca42a20"
	if got != want {
		t.Fatalf("md5sum: got %q, want %q", got, want)
	}
}

func TestMergeTemplateFuncsIncludesSprigAndMd5(t *testing.T) {
	funcs := mergeTemplateFuncs(template.FuncMap{
		"nginx_add_header": func(header string, value string) string {
			return header + ":" + value
		},
	})

	for _, name := range []string{"sha1sum", "sha256sum", "sha512sum", "md5sum", "nginx_add_header", "trunc"} {
		if _, ok := funcs[name]; !ok {
			t.Fatalf("expected function %q in merged FuncMap", name)
		}
	}

	if _, ok := funcs["upper"]; ok {
		t.Fatal("sprig upper must not overwrite sigil builtin (should be deleted from merge)")
	}

	sigil.Register(funcs)

	cases := []struct {
		name string
		tpl  string
		want string
	}{
		{
			name: "sha256sum",
			tpl:  `{{ sha256sum "Hello world!" }}`,
			// echo -n "Hello world!" | sha256sum
			want: "c0535e4be2b79ffd93291305436bf889314e4a3faec05ecffcbb7df31ad9e51a",
		},
		{
			name: "sha1sum",
			tpl:   `{{ sha1sum "Hello world!" }}`,
			want:  "d3486ae9136e7856bc42212385ea797094475802",
		},
		{
			name: "md5sum",
			tpl:   `{{ md5sum "Hello world!" }}`,
			want:  "86fb269d190d2c85f6e0468ceca42a20",
		},
		{
			name: "sprig trunc",
			tpl:   `{{ trunc 5 "hello world" }}`,
			want:  "hello",
		},
		{
			name: "sigil upper still works",
			tpl:   `{{ upper "hi" }}`,
			want:  "HI",
		},
		{
			name: "custom nginx_add_header wins",
			tpl:   `{{ nginx_add_header "X-A" "1" }}`,
			want:  "X-A:1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := sigil.Execute([]byte(tc.tpl), map[string]any{}, "tmpl_func_"+tc.name)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			got := strings.TrimSpace(out.String())
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
