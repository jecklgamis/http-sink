package sink

import (
	"math"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// Response times are recorded in microseconds: the sink usually answers in
// well under a millisecond, so whole milliseconds would flatten everything to 0.
const (
	histMinUs   = 1
	histMaxUs   = int64(time.Hour / time.Microsecond)
	histSigFigs = 2 // 1% value precision
)

func newHistogram() *hdrhistogram.Histogram {
	return hdrhistogram.New(histMinUs, histMaxUs, histSigFigs)
}

// record adds d to h, clamping to the trackable range.
func record(h *hdrhistogram.Histogram, d time.Duration) {
	us := d.Microseconds()
	if us < histMinUs {
		us = histMinUs
	}
	if us > histMaxUs {
		us = histMaxUs
	}
	_ = h.RecordValue(us)
}

// LatencySummary holds response-time percentiles, in milliseconds.
type LatencySummary struct {
	Count  int64   `json:"count"`
	MinMs  float64 `json:"minMs"`
	MeanMs float64 `json:"meanMs"`
	P50Ms  float64 `json:"p50Ms"`
	P90Ms  float64 `json:"p90Ms"`
	P99Ms  float64 `json:"p99Ms"`
	P999Ms float64 `json:"p999Ms"`
	MaxMs  float64 `json:"maxMs"`
}

func usToMs(us float64) float64 { return math.Round(us) / 1000 }

func summarize(h *hdrhistogram.Histogram) LatencySummary {
	if h.TotalCount() == 0 {
		return LatencySummary{}
	}
	return LatencySummary{
		Count:  h.TotalCount(),
		MinMs:  usToMs(float64(h.Min())),
		MeanMs: usToMs(h.Mean()),
		P50Ms:  usToMs(float64(h.ValueAtQuantile(50))),
		P90Ms:  usToMs(float64(h.ValueAtQuantile(90))),
		P99Ms:  usToMs(float64(h.ValueAtQuantile(99))),
		P999Ms: usToMs(float64(h.ValueAtQuantile(99.9))),
		MaxMs:  usToMs(float64(h.Max())),
	}
}

// LatencyStats is the response-time summary over the trailing window.
type LatencyStats struct {
	WindowSeconds int                       `json:"windowSeconds"`
	All           LatencySummary            `json:"all"`
	ByPath        map[string]LatencySummary `json:"byPath"` // busiest paths only
}
