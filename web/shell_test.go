package web

import (
	"strings"
	"testing"
)

// shellFixture mimics the relevant parts of the generated SPA index.html.
const shellFixture = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">` +
	`<title>Media Server Pro</title>` +
	`<meta name="description" content="default description">` +
	`</head><body><div id="__nuxt"></div></body></html>`

func TestApplyShellMeta_EmptyReturnsSameBytes(t *testing.T) {
	in := []byte(shellFixture)
	out := applyShellMeta(in, ShellMeta{})
	if &out[0] != &in[0] {
		t.Fatal("empty ShellMeta should return the original slice without copying")
	}
}

func TestApplyShellMeta_ReplacesTitleAndDescription(t *testing.T) {
	out := string(applyShellMeta([]byte(shellFixture), ShellMeta{
		Title:       "My Clip",
		Description: "A great clip",
	}))

	if !strings.Contains(out, "<title>My Clip</title>") {
		t.Errorf("title not replaced: %s", out)
	}
	if strings.Contains(out, "<title>Media Server Pro</title>") {
		t.Errorf("original title still present: %s", out)
	}
	if !strings.Contains(out, `<meta name="description" content="A great clip">`) {
		t.Errorf("description not replaced: %s", out)
	}
	if strings.Contains(out, `content="default description"`) {
		t.Errorf("original description still present: %s", out)
	}
	// Exactly one description meta should remain (replaced, not appended).
	if n := strings.Count(out, `name="description"`); n != 1 {
		t.Errorf("expected exactly 1 description meta, got %d", n)
	}
}

func TestApplyShellMeta_SplicesHeadAndNoScript(t *testing.T) {
	out := string(applyShellMeta([]byte(shellFixture), ShellMeta{
		Head:     `<meta property="og:title" content="x">`,
		NoScript: `<h1>Hello</h1>`,
	}))

	if !strings.Contains(out, `<meta property="og:title" content="x"></head>`) {
		t.Errorf("head fragment not spliced before </head>: %s", out)
	}
	if !strings.Contains(out, `<noscript><h1>Hello</h1></noscript></body>`) {
		t.Errorf("noscript not spliced before </body>: %s", out)
	}
}

// A '$' in the title must survive verbatim (literal regexp replacement).
func TestApplyShellMeta_TitleWithDollarSign(t *testing.T) {
	out := string(applyShellMeta([]byte(shellFixture), ShellMeta{Title: "Cost $5 Special"}))
	if !strings.Contains(out, "<title>Cost $5 Special</title>") {
		t.Errorf("dollar sign in title mangled: %s", out)
	}
}
