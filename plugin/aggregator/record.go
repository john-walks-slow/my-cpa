package aggregator

import "time"

type Sample struct {
	Provider        string
	Model           string
	Alias           string
	AuthID          string
	Source          string
	RequestedAt     time.Time
	Latency         time.Duration
	TTFT            time.Duration
	Failed          bool
	StatusCode      int
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
}

func (s Sample) SeriesKey() string {
	return escape(s.Provider) + "|" + escape(s.Model) + "|" + escape(s.Alias) + "|" + escape(s.AuthID)
}

// SplitSeriesKey parses a series key back into its 4 components.
func SplitSeriesKey(key string) [4]string {
	var parts [4]string
	idx := 0
	start := 0
	for i := 0; i < len(key) && idx < 3; i++ {
		if key[i] == '|' && (i == 0 || key[i-1] != '\\') {
			parts[idx] = unescape(key[start:i])
			idx++
			start = i + 1
		}
	}
	parts[idx] = unescape(key[start:])
	return parts
}

func unescape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		out = append(out, s[i])
	}
	return string(out)
}

func escape(v string) string {
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		if v[i] == '|' || v[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, v[i])
	}
	return string(out)
}

func (s Sample) StreamRate() (float64, bool) {
	if s.OutputTokens <= 0 {
		return 0, false
	}
	var elapsed time.Duration
	if s.TTFT > 0 {
		elapsed = s.Latency - s.TTFT
	} else {
		elapsed = s.Latency
	}
	if elapsed <= 0 {
		return 0, false
	}
	return float64(s.OutputTokens) / elapsed.Seconds(), true
}
