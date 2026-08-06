package mysql

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Indexed narrowing for the Hub category filter.
//
// Categories are stored as one ';'-joined string per row, so the exact-match
// predicate has to wrap the column and lead with a wildcard:
//
//	CONCAT(';', categories, ';') LIKE '%;Big Tits;%'
//
// No index can satisfy that, so MySQL reads every row — and hub_embeds rows carry
// thumb_url TEXT plus preview_urls MEDIUMTEXT, so a "full scan" of a few million
// rows is gigabytes of I/O. Search() ran it twice per request (count, then page),
// which is where the reported minute-long category load came from.
//
// The fix is to put an indexed predicate in front of it. ft_hub_embeds_categories
// is a FULLTEXT index on the categories column, so a boolean-mode MATCH narrows
// to the rows that contain the category's words before the LIKE runs. The LIKE
// stays exactly as it was, as a residual filter over that much smaller set, which
// is what keeps the result set identical: MATCH can only ever return a superset
// of the LIKE's matches (it ignores word order, adjacency and the ';' delimiters,
// so "Big Natural Tits" matches the MATCH but is then rejected by the LIKE).
//
// Every case where that superset property might not hold falls back to the old
// LIKE-only plan. Returning the right answer slowly beats returning a wrong one
// quickly.

// defaultInnodbMinTokenSize is the shipped default of innodb_ft_min_token_size.
// Used only when the live value cannot be read; the real value is queried at
// runtime because an operator can raise it, and a token below the server's
// actual minimum is not in the index, so requiring one would match nothing.
const defaultInnodbMinTokenSize = 3

// innodbStopwords is InnoDB's built-in default stopword list, restricted to the
// entries at or above innodbMinTokenSize (shorter ones are already excluded by
// length). A stopword is not in the index, so requiring one matches nothing.
var innodbStopwords = map[string]bool{
	"about": true, "are": true, "com": true, "for": true, "from": true,
	"how": true, "that": true, "the": true, "this": true, "und": true,
	"was": true, "what": true, "when": true, "where": true, "who": true,
	"will": true, "with": true, "www": true,
}

// hubCategoryMatchExpr builds the boolean-mode expression that narrows to rows
// whose categories column contains every indexable word of category, or ok=false
// when no safe expression exists and the caller must use the LIKE alone.
//
// Tokenisation deliberately mirrors InnoDB's default parser (split on
// non-alphanumerics, lowercase) and bails out whenever it might not: a token this
// function invents but InnoDB never indexed would silently return an empty
// result set, which is a far worse failure than a slow query.
func hubCategoryMatchExpr(category string, minTokenSize int) (string, bool) {
	if minTokenSize <= 0 {
		minTokenSize = defaultInnodbMinTokenSize
	}
	category = strings.TrimSpace(category)
	if category == "" {
		return "", false
	}
	// Non-ASCII is the main place our tokenisation could diverge from InnoDB's
	// (it treats accented letters as word characters; the split below would cut
	// the word in half and require a fragment that was never indexed).
	for i := 0; i < len(category); i++ {
		if category[i] >= 0x80 {
			return "", false
		}
	}

	var tokens []string
	for _, tok := range strings.FieldsFunc(category, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		tok = strings.ToLower(tok)
		if len(tok) < minTokenSize || innodbStopwords[tok] {
			continue
		}
		tokens = append(tokens, "+"+tok)
	}
	if len(tokens) == 0 {
		// e.g. a category like "3D": every word is below the index's minimum
		// token size, so there is nothing indexed to match on.
		return "", false
	}
	return strings.Join(tokens, " "), true
}

// categoryIndexTTL bounds how long the presence of the FULLTEXT index is
// remembered. It is re-checked rather than resolved once at construction because
// the index is created by the schema migration at startup and, on a large
// catalog, may still be building when the first queries arrive.
const categoryIndexTTL = 2 * time.Minute

// categoryIndexInfo is what the query planner side needs to know about the
// server's full-text configuration before it can safely emit a MATCH.
type categoryIndexInfo struct {
	// usable is false whenever the MATCH path must not be taken at all.
	usable bool
	// minTokenSize is the server's live innodb_ft_min_token_size.
	minTokenSize int
}

// categoryIndexReady reports whether the MATCH narrowing can be used, and under
// what tokenisation rules.
//
// Three things have to hold, and all three are checked against the live server
// rather than assumed:
//
//   - ft_hub_embeds_categories exists. Issuing a MATCH against a column with no
//     FULLTEXT index is a hard error in InnoDB, not a slow query — this gate is
//     what lets the feature ship while the index is still building in the
//     background, and what keeps the catalog browsable if it is dropped by hand.
//   - innodb_ft_min_token_size is known, because a token shorter than the
//     server's minimum was never indexed and requiring it would match nothing.
//   - no custom stopword table is configured. The built-in stopword list is
//     known and compiled in above, but a custom one is arbitrary: any word in it
//     is absent from the index, so we cannot tell which tokens are safe to
//     require. Rather than guess, drop back to the LIKE-only plan.
func (r *HubEmbedRepository) categoryIndexReady(ctx context.Context) categoryIndexInfo {
	r.ftMu.Lock()
	defer r.ftMu.Unlock()
	if r.ftChecked && time.Since(r.ftCheckedAt) < categoryIndexTTL {
		return r.ftInfo
	}
	r.ftInfo = r.probeCategoryIndex(ctx)
	r.ftChecked = true
	r.ftCheckedAt = time.Now()
	return r.ftInfo
}

// probeCategoryIndex reads the live state. Any read failure yields an unusable
// result: the LIKE-only plan is always correct, just slower, so failing closed
// costs performance while failing open would cost correctness.
func (r *HubEmbedRepository) probeCategoryIndex(ctx context.Context) categoryIndexInfo {
	db := r.db.WithContext(ctx)

	var present bool
	if err := db.Raw(
		`SELECT COUNT(*) > 0 FROM information_schema.STATISTICS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'hub_embeds'
		   AND INDEX_NAME = 'ft_hub_embeds_categories'`).Scan(&present).Error; err != nil || !present {
		return categoryIndexInfo{}
	}

	// A custom stopword table makes the indexed vocabulary unknowable to us.
	var stopwordTables []string
	if err := db.Raw(
		`SELECT VARIABLE_VALUE FROM information_schema.GLOBAL_VARIABLES
		 WHERE VARIABLE_NAME IN ('innodb_ft_server_stopword_table','innodb_ft_user_stopword_table')`).
		Scan(&stopwordTables).Error; err != nil {
		return categoryIndexInfo{}
	}
	for _, v := range stopwordTables {
		if strings.TrimSpace(v) != "" {
			return categoryIndexInfo{}
		}
	}

	var minToken int
	if err := db.Raw(
		`SELECT VARIABLE_VALUE FROM information_schema.GLOBAL_VARIABLES
		 WHERE VARIABLE_NAME = 'innodb_ft_min_token_size'`).Scan(&minToken).Error; err != nil || minToken <= 0 {
		return categoryIndexInfo{}
	}

	return categoryIndexInfo{usable: true, minTokenSize: minToken}
}

// ftIndexState is embedded in HubEmbedRepository; kept here so the whole
// optimisation reads as one unit.
type ftIndexState struct {
	ftMu        sync.Mutex
	ftInfo      categoryIndexInfo
	ftChecked   bool
	ftCheckedAt time.Time
}
