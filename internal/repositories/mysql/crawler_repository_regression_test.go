package mysql

import "testing"

// TestCrawlerDiscoveryStatusValidation guards against a regression where the
// CrawlerDiscoveryRepository.UpdateStatus allow-list was {pending, approved,
// rejected} while the crawler module writes {pending, added, ignored}. That
// mismatch made every ApproveDiscovery ("added") and IgnoreDiscovery
// ("ignored") call fail with "invalid crawler discovery status", silently
// breaking discovery review.
func TestCrawlerDiscoveryStatusValidation(t *testing.T) {
	// Must accept the statuses the crawler module actually writes.
	for _, status := range []string{"pending", "added", "ignored"} {
		if !isValidCrawlerDiscoveryStatus(status) {
			t.Errorf("status %q must be accepted by UpdateStatus (crawler module writes it)", status)
		}
	}

	// Must reject the old (wrong) values and anything unexpected.
	for _, status := range []string{"approved", "rejected", "bogus", ""} {
		if isValidCrawlerDiscoveryStatus(status) {
			t.Errorf("status %q must be rejected by UpdateStatus", status)
		}
	}
}
