package score

import "testing"

func TestAnchors(t *testing.T) {
	b := Calculate(Metrics{LatencyMS: 20, LossPercent: 0, JitterMS: 5, DownloadMbps: 500, UploadMbps: 100})
	if b.Download != 98 || b.Loss != 100 || b.Overall < 90 {
		t.Fatal(b)
	}
}
func TestScoreBounded(t *testing.T) {
	for _, m := range []Metrics{{LatencyMS: -1, DownloadMbps: -1}, {LatencyMS: 9999, LossPercent: 99, JitterMS: 99, DownloadMbps: 9999, UploadMbps: 9999}} {
		b := Calculate(m)
		if b.Overall < 0 || b.Overall > 100 {
			t.Fatal(b)
		}
	}
}
