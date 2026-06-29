package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

func deletionBody(t *testing.T, fields map[string]any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal deletion body: %v", err)
	}
	return bytes.NewReader(b)
}

// TestDeletionRequestApproveFlow exercises the consequential approve path that
// the handler unit tests can't reach (it calls the concrete *auth.Module to
// permanently delete the account): a user submits a request, an admin approves
// it, the response reports "approved", and the request leaves the pending list.
func TestDeletionRequestApproveFlow(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// A normal user submits a data-deletion request.
	ts.Env.CreateTestUser(t, "victim_del", "victim_pass_123")
	victimSession := ts.Env.LoginUser(t, "victim_del", "victim_pass_123")

	submitResp := ts.AuthRequest("POST", "/api/auth/data-deletion-request",
		deletionBody(t, map[string]any{"reason": "please remove my data"}), victimSession)
	submitResp.Body.Close()
	if submitResp.StatusCode != http.StatusOK {
		t.Fatalf("submit deletion request: expected 200, got %d", submitResp.StatusCode)
	}

	// An admin lists pending requests and locates the one just submitted.
	ts.Env.CreateTestAdmin(t, "admin_del", "admin_pass_123")
	adminSession := ts.Env.LoginUser(t, "admin_del", "admin_pass_123")

	listResp := ts.AuthRequest("GET", "/api/admin/data-deletion-requests?status=pending", nil, adminSession)
	listResult := ts.ParseJSON(listResp)
	listResp.Body.Close()
	rows, ok := listResult["data"].([]any)
	if !ok {
		t.Fatalf("list: data is not an array: %v", listResult)
	}
	var requestID string
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if row["username"] == "victim_del" {
			requestID, _ = row["id"].(string)
			break
		}
	}
	if requestID == "" {
		t.Fatalf("list: could not find pending request for victim_del: %v", rows)
	}

	// Approving permanently deletes the account and marks the request approved.
	approveResp := ts.AuthRequest("POST", "/api/admin/data-deletion-requests/"+requestID+"/process",
		deletionBody(t, map[string]any{"action": "approve"}), adminSession)
	approveResult := ts.ParseJSON(approveResp)
	approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d: %v", approveResp.StatusCode, approveResult)
	}
	if data, _ := approveResult["data"].(map[string]any); data["status"] != "approved" {
		t.Errorf("approve: response status = %v, want approved", approveResult["data"])
	}

	// The request must no longer appear in the pending list.
	reListResp := ts.AuthRequest("GET", "/api/admin/data-deletion-requests?status=pending", nil, adminSession)
	reListResult := ts.ParseJSON(reListResp)
	reListResp.Body.Close()
	if rows, ok := reListResult["data"].([]any); ok {
		for _, r := range rows {
			if row, _ := r.(map[string]any); row["id"] == requestID {
				t.Errorf("approve: request %s still present in pending list", requestID)
			}
		}
	}
}
