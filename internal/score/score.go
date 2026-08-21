package score

import "math"

type Metrics struct{ LatencyMS, LossPercent, JitterMS, DownloadMbps, UploadMbps float64 }
type Breakdown struct{ Overall, Latency, Loss, Jitter, Download, Upload float64 }
type point struct{ x, y float64 }

func interp(x float64, ps []point) float64 {
	if x <= ps[0].x {
		return ps[0].y
	}
	for i := 1; i < len(ps); i++ {
		if x <= ps[i].x {
			p, q := ps[i-1], ps[i]
			return p.y + (q.y-p.y)*(x-p.x)/(q.x-p.x)
		}
	}
	return ps[len(ps)-1].y
}
func round(x float64) float64 { return math.Round(math.Max(0, math.Min(100, x))*10) / 10 }
func Calculate(m Metrics) Breakdown {
	b := Breakdown{
		Latency:  interp(m.LatencyMS, []point{{0, 100}, {20, 98}, {50, 85}, {100, 65}, {200, 30}, {500, 0}}),
		Loss:     interp(m.LossPercent, []point{{0, 100}, {.5, 90}, {1, 65}, {2, 25}, {5, 0}}),
		Jitter:   interp(m.JitterMS, []point{{0, 100}, {5, 95}, {15, 75}, {30, 45}, {60, 0}}),
		Download: interp(m.DownloadMbps, []point{{0, 0}, {10, 25}, {50, 55}, {100, 75}, {300, 92}, {500, 98}, {1000, 100}}),
		Upload:   interp(m.UploadMbps, []point{{0, 0}, {5, 25}, {20, 55}, {50, 75}, {100, 90}, {300, 100}}),
	}
	b.Latency = round(b.Latency)
	b.Loss = round(b.Loss)
	b.Jitter = round(b.Jitter)
	b.Download = round(b.Download)
	b.Upload = round(b.Upload)
	b.Overall = round(.30*b.Latency + .25*b.Loss + .15*b.Jitter + .20*b.Download + .10*b.Upload)
	return b
}
