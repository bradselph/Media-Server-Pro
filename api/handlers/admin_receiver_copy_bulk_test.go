package handlers

import (
	"context"
	"testing"
)

func TestReceiverBulkCopyJobBookkeeping(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := newReceiverBulkCopyJob(3, cancel)

	if st := j.snapshot(); !st.Running || st.Total != 3 || st.Done != 0 || st.StartedAt == nil {
		t.Fatalf("initial snapshot = %+v", st)
	}

	j.setCurrent("Clip A")
	j.record(receiverBulkCopyResult{ID: "a", Name: "Clip A", Status: receiverCopyStatusCopied})
	j.record(receiverBulkCopyResult{ID: "b", Name: "Clip B", Status: receiverCopyStatusSkipped, Detail: "already in the local library"})
	j.record(receiverBulkCopyResult{ID: "c", Name: "Clip C", Status: receiverCopyStatusFailed, Detail: "transfer from peer failed"})

	st := j.snapshot()
	if st.Done != 3 || st.Copied != 1 || st.Skipped != 1 || st.Failed != 1 {
		t.Errorf("counters: done=%d copied=%d skipped=%d failed=%d, want 3/1/1/1",
			st.Done, st.Copied, st.Skipped, st.Failed)
	}
	if st.Current != "Clip A" {
		t.Errorf("current = %q, want Clip A", st.Current)
	}
	if len(st.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(st.Results))
	}

	// snapshot must return a copy — mutating it can't corrupt the job.
	st.Results[0].Status = "mutated"
	if j.snapshot().Results[0].Status != receiverCopyStatusCopied {
		t.Error("snapshot leaked the internal results slice")
	}

	j.finish(false)
	end := j.snapshot()
	if end.Running || end.Canceled {
		t.Errorf("after finish: running=%v canceled=%v, want false/false", end.Running, end.Canceled)
	}
	if end.Current != "" {
		t.Error("current should clear on finish")
	}
	if end.FinishedAt == nil {
		t.Error("finished_at should be set after finish")
	}
}

func TestReceiverBulkCopyJobCancelSticky(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	j := newReceiverBulkCopyJob(2, cancel)

	j.markCanceled()
	if ctx.Err() == nil {
		t.Fatal("markCanceled must cancel the job context")
	}
	if !j.isRunning() {
		t.Error("the cancel flag alone must not mark the job finished — the worker does that")
	}

	// The worker observes ctx and finishes; canceled must stick through it.
	j.finish(ctx.Err() != nil)
	st := j.snapshot()
	if st.Running {
		t.Error("finished job still reports running")
	}
	if !st.Canceled {
		t.Error("canceled must stick through finish")
	}
}

func TestBeginEndReceiverCopy(t *testing.T) {
	h := &Handler{}
	if !h.beginReceiverCopy("x") {
		t.Fatal("first begin should win")
	}
	if h.beginReceiverCopy("x") {
		t.Error("second begin for the same in-flight id must lose")
	}
	if !h.beginReceiverCopy("y") {
		t.Error("a different id must not be blocked")
	}
	h.endReceiverCopy("x")
	if !h.beginReceiverCopy("x") {
		t.Error("begin after end should win again")
	}
}
