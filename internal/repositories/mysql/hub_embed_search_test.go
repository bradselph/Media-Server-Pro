package mysql

import (
	"strings"
	"testing"
)

// hubCategoryMatchExpr adds an indexed predicate in front of the exact LIKE. The
// only way that can be wrong is if MATCH excludes a row the LIKE would have
// accepted — the result set would silently shrink, and a category would look
// empty rather than slow. Every test here is about that failure mode.

func TestHubCategoryMatchExpr_RequiresEveryIndexableWord(t *testing.T) {
	expr, ok := hubCategoryMatchExpr("Big Tits", defaultInnodbMinTokenSize)
	if !ok {
		t.Fatal("expected a usable expression for an ordinary category")
	}
	for _, want := range []string{"+big", "+tits"} {
		if !strings.Contains(expr, want) {
			t.Errorf("expression %q missing required term %q", expr, want)
		}
	}
}

func TestHubCategoryMatchExpr_LowercasesAndSplitsOnPunctuation(t *testing.T) {
	expr, ok := hubCategoryMatchExpr("Rough-Sex (HD)", defaultInnodbMinTokenSize)
	if !ok {
		t.Fatal("expected a usable expression")
	}
	for _, want := range []string{"+rough", "+sex"} {
		if !strings.Contains(expr, want) {
			t.Errorf("expression %q missing %q", expr, want)
		}
	}
	// "HD" is two characters, below innodb_ft_min_token_size, so it is not in the
	// index and must not be required.
	if strings.Contains(expr, "+hd") {
		t.Errorf("expression %q requires a sub-minimum token", expr)
	}
}

// Requiring a token InnoDB never indexed matches zero rows. These inputs must
// therefore fall back to the LIKE-only plan rather than produce an expression.
func TestHubCategoryMatchExpr_BailsOutWhenUnsafe(t *testing.T) {
	for _, tc := range []struct{ name, category string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"all tokens below min size", "3D"},
		{"punctuation only", "!!!"},
		{"non-ascii would tokenise differently", "Café"},
		{"non-ascii mixed with ascii", "Anal Café"},
		{"single stopword", "the"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if expr, ok := hubCategoryMatchExpr(tc.category, defaultInnodbMinTokenSize); ok {
				t.Errorf("expected fallback to LIKE-only for %q, got expression %q", tc.category, expr)
			}
		})
	}
}

// A stopword is not in the index, so it must be dropped rather than required —
// but its indexable neighbours should still narrow the scan.
func TestHubCategoryMatchExpr_DropsStopwordsKeepsRest(t *testing.T) {
	expr, ok := hubCategoryMatchExpr("The Office", defaultInnodbMinTokenSize)
	if !ok {
		t.Fatal("expected an expression from the non-stopword term")
	}
	if strings.Contains(expr, "+the") {
		t.Errorf("expression %q requires the stopword 'the'", expr)
	}
	if !strings.Contains(expr, "+office") {
		t.Errorf("expression %q lost the indexable term", expr)
	}
}

// innodb_ft_min_token_size is operator-configurable. Requiring a token shorter
// than the server's live minimum would match nothing, so the threshold must
// follow the server rather than the compiled-in default.
func TestHubCategoryMatchExpr_HonoursConfiguredMinTokenSize(t *testing.T) {
	// At the default of 3, "Ass Play" yields both words.
	if expr, ok := hubCategoryMatchExpr("Ass Play", 3); !ok || !strings.Contains(expr, "+ass") {
		t.Fatalf("at minTokenSize=3 expected +ass, got %q ok=%v", expr, ok)
	}
	// Raised to 4, "ass" is no longer indexed and must not be required.
	expr, ok := hubCategoryMatchExpr("Ass Play", 4)
	if !ok {
		t.Fatal("expected the 4-letter token to still carry the expression")
	}
	if strings.Contains(expr, "+ass") {
		t.Errorf("expression %q requires a token below the server minimum", expr)
	}
	if !strings.Contains(expr, "+play") {
		t.Errorf("expression %q lost the still-indexable token", expr)
	}
	// Raised past every token, there is nothing safe to require.
	if expr, ok := hubCategoryMatchExpr("Ass Play", 8); ok {
		t.Errorf("expected fallback when no token meets the minimum, got %q", expr)
	}
}

// A zero/unknown value must not disable every token; it falls back to the
// shipped default rather than to "nothing is indexable".
func TestHubCategoryMatchExpr_ZeroMinTokenSizeUsesDefault(t *testing.T) {
	expr, ok := hubCategoryMatchExpr("Big Tits", 0)
	if !ok || !strings.Contains(expr, "+big") {
		t.Fatalf("expected default tokenisation, got %q ok=%v", expr, ok)
	}
}

// Digits are word characters to InnoDB, so numeric tokens are indexed normally.
func TestHubCategoryMatchExpr_KeepsNumericTokens(t *testing.T) {
	expr, ok := hubCategoryMatchExpr("60FPS", defaultInnodbMinTokenSize)
	if !ok {
		t.Fatal("expected an expression for an alphanumeric category")
	}
	if !strings.Contains(expr, "+60fps") {
		t.Errorf("expression %q lost the alphanumeric token", expr)
	}
}

// The expression is passed as a bound parameter, but it must still never carry
// boolean-mode operators that would change the query's meaning.
func TestHubCategoryMatchExpr_EmitsOnlyRequiredTerms(t *testing.T) {
	expr, ok := hubCategoryMatchExpr(`Weird" -Category* (x)~`, defaultInnodbMinTokenSize)
	if !ok {
		t.Fatal("expected an expression")
	}
	for _, forbidden := range []string{`"`, "*", "~", "(", ")", "<", ">", "@"} {
		if strings.Contains(expr, forbidden) {
			t.Errorf("expression %q leaked boolean-mode operator %q", expr, forbidden)
		}
	}
	for term := range strings.FieldsSeq(expr) {
		if !strings.HasPrefix(term, "+") {
			t.Errorf("term %q in %q is not a required term", term, expr)
		}
		if strings.Count(term, "+") != 1 {
			t.Errorf("term %q in %q has stray operators", term, expr)
		}
	}
}
