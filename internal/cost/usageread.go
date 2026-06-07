package cost

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"time"
)

// usageLogSoftCap bounds the worst-case read: past this size only the tail is
// parsed (the newest records), so a long-lived log never stalls a render.
const usageLogSoftCap = 16 << 20 // 16 MiB

// UsageWindow is the rollup for one time horizon.
type UsageWindow struct {
	Total      Summary
	ByProvider map[string]Summary
	ByModel    map[string]Summary
	Series     []float64 // per-bucket token totals (oldest→newest) for a sparkline
	Estimated  bool      // true if any included record's cost was a static estimate
}

// ReadUsage loads and parses the whole usage log once. Returns (nil, nil) when
// the file is absent so callers degrade gracefully. Unparseable lines (e.g. a
// partial trailing line from a crash) are skipped. Past the soft cap only the
// tail is read.
func ReadUsage() ([]UsageRecord, error) {
	path := usagePath()
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if fi, statErr := f.Stat(); statErr == nil && fi.Size() > usageLogSoftCap {
		if _, seekErr := f.Seek(fi.Size()-usageLogSoftCap, io.SeekStart); seekErr == nil {
			br := bufio.NewReader(f)
			_, _ = br.ReadString('\n') // drop the first, likely-partial line
			r = br
		}
	}

	var recs []UsageRecord
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec UsageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		recs = append(recs, rec)
	}
	return recs, sc.Err()
}

// Aggregate rolls records into one window: a record is included when
// window <= 0 (all-time) or its age is within [0, window]. buckets controls
// Series resolution; pass 0 to skip the series.
func Aggregate(recs []UsageRecord, window time.Duration, now time.Time, buckets int) UsageWindow {
	w := UsageWindow{ByProvider: map[string]Summary{}, ByModel: map[string]Summary{}}
	if buckets > 0 {
		w.Series = make([]float64, buckets)
	}
	// for all-time the series spans earliest→now; find the earliest in-window.
	start := now
	for _, r := range recs {
		if inWindow(r.Time, window, now) && r.Time.Before(start) {
			start = r.Time
		}
	}
	for _, r := range recs {
		if !inWindow(r.Time, window, now) {
			continue
		}
		add(&w.Total, r)
		addTo(w.ByProvider, r.Provider, r)
		addTo(w.ByModel, r.Model, r)
		if !r.CostReported {
			w.Estimated = true
		}
		if buckets > 0 {
			w.Series[bucketIndex(r.Time, window, now, start, buckets)] += float64(r.Input + r.Output)
		}
	}
	return w
}

// UsageOverview reads the log once and returns the four standard windows:
// last 24h, last 7d, last 30d, all-time.
func UsageOverview() (day, week, month, all UsageWindow, err error) {
	recs, rerr := ReadUsage()
	if rerr != nil {
		return day, week, month, all, rerr
	}
	now := time.Now().UTC()
	day = Aggregate(recs, 24*time.Hour, now, 24)
	week = Aggregate(recs, 7*24*time.Hour, now, 7)
	month = Aggregate(recs, 30*24*time.Hour, now, 30)
	all = Aggregate(recs, 0, now, 24)
	return day, week, month, all, nil
}

func inWindow(t time.Time, window time.Duration, now time.Time) bool {
	age := now.Sub(t)
	if age < 0 {
		return false // ignore clock-skewed future records
	}
	return window <= 0 || age <= window
}

// bucketIndex maps a record time to a 0..buckets-1 slot, oldest on the left.
// For a fixed window the basis is the window duration; for all-time it is the
// span from the earliest in-window record to now.
func bucketIndex(t time.Time, window time.Duration, now, start time.Time, buckets int) int {
	span := window
	if span <= 0 {
		span = now.Sub(start)
	}
	if span <= 0 {
		return buckets - 1
	}
	age := now.Sub(t)
	idx := buckets - 1 - int(float64(age)/float64(span)*float64(buckets))
	if idx < 0 {
		idx = 0
	}
	if idx >= buckets {
		idx = buckets - 1
	}
	return idx
}

func add(s *Summary, r UsageRecord) {
	s.Calls++
	s.Input += r.Input
	s.Output += r.Output
	s.USD += r.USD
}

func addTo(m map[string]Summary, key string, r UsageRecord) {
	s := m[key]
	add(&s, r)
	m[key] = s
}
