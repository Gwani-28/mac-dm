package downloader

import "testing"

func TestYTDLFragmentSegments(t *testing.T) {
	segs := ytdlFragmentSegments(5, 10, 4)
	if len(segs) != 4 {
		t.Fatalf("len = %d, want 4", len(segs))
	}
	gotDone := int64(0)
	gotTotal := int64(0)
	for _, s := range segs {
		gotDone += s.Done
		gotTotal += s.End - s.Start + 1
	}
	if gotDone != 5 {
		t.Fatalf("done = %d, want 5", gotDone)
	}
	if gotTotal != 10 {
		t.Fatalf("total = %d, want 10", gotTotal)
	}
}
