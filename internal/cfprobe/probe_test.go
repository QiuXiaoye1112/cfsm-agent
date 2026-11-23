package cfprobe

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSplitProbeTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantHost string
		wantPort int
	}{
		{name: "host default", target: "example.com", wantHost: "example.com", wantPort: defaultMetricsTCPPort},
		{name: "host port", target: "example.com:8443", wantHost: "example.com", wantPort: 8443},
		{name: "ipv6 bracket", target: "[2001:db8::1]:443", wantHost: "2001:db8::1", wantPort: 443},
		{name: "ipv6 default", target: "2001:db8::1", wantHost: "2001:db8::1", wantPort: defaultMetricsTCPPort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := splitProbeTarget(tt.target, defaultMetricsTCPPort)
			if err != nil {
				t.Fatalf("splitProbeTarget returned error: %v", err)
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("got %s:%d, want %s:%d", host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func resetDNSCacheForTest(t *testing.T) {
	t.Helper()
	oldLookup := lookupIP
	dnsCacheMu.Lock()
	dnsCache = map[string]dnsCacheEntry{}
	dnsCacheMu.Unlock()
	t.Cleanup(func() {
		lookupIP = oldLookup
		dnsCacheMu.Lock()
		dnsCache = map[string]dnsCacheEntry{}
		dnsCacheMu.Unlock()
	})
}

func TestResolveFirstIPCachesDNSForThirtyMinutes(t *testing.T) {
	resetDNSCacheForTest(t)
	calls := 0
	lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}, nil
	}

	first, err := resolveFirstIP(context.Background(), "Example.COM")
	if err != nil {
		t.Fatalf("resolveFirstIP returned error: %v", err)
	}
	second, err := resolveFirstIP(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("resolveFirstIP returned error: %v", err)
	}
	if first != "192.0.2.1" || second != "192.0.2.1" {
		t.Fatalf("got %q and %q, want cached 192.0.2.1", first, second)
	}
	if calls != 1 {
		t.Fatalf("lookup calls = %d, want 1", calls)
	}
	dnsCacheMu.RLock()
	entry := dnsCache["example.com"]
	dnsCacheMu.RUnlock()
	if !entry.expiresAt.After(time.Now().Add(29 * time.Minute)) {
		t.Fatalf("cache expires too soon: %s", entry.expiresAt)
	}
}

func TestResolveFirstIPRefreshesExpiredCache(t *testing.T) {
	resetDNSCacheForTest(t)
	dnsCacheMu.Lock()
	dnsCache["example.com"] = dnsCacheEntry{ip: "192.0.2.1", expiresAt: time.Now().Add(-time.Minute)}
	dnsCacheMu.Unlock()
	calls := 0
	lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.2")}}, nil
	}

	got, err := resolveFirstIP(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("resolveFirstIP returned error: %v", err)
	}
	if got != "192.0.2.2" {
		t.Fatalf("got %q, want refreshed 192.0.2.2", got)
	}
	if calls != 1 {
		t.Fatalf("lookup calls = %d, want 1", calls)
	}
}

func TestResolveFirstIPSkipsDNSForIPLiteral(t *testing.T) {
	resetDNSCacheForTest(t)
	calls := 0
	lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		return nil, errors.New("unexpected dns lookup")
	}

	got, err := resolveFirstIP(context.Background(), "192.0.2.10")
	if err != nil {
		t.Fatalf("resolveFirstIP returned error: %v", err)
	}
	if got != "192.0.2.10" {
		t.Fatalf("got %q, want literal IP", got)
	}
	if calls != 0 {
		t.Fatalf("lookup calls = %d, want 0", calls)
	}
}

func TestRetryMeasuredPingAcceptsLowLatency(t *testing.T) {
	calls := 0
	got, err := retryMeasuredPing("tcp", func() (int, error) {
		calls++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("retryMeasuredPing returned error: %v", err)
	}
	if got != 42 || calls != 1 {
		t.Fatalf("got latency=%d calls=%d, want latency=42 calls=1", got, calls)
	}
}

func TestRetryMeasuredPingRejectsSuspiciousTCPRetransmission(t *testing.T) {
	values := []int{1200, 100}
	calls := 0
	got, err := retryMeasuredPing("tcp", func() (int, error) {
		v := values[calls]
		calls++
		return v, nil
	})
	if err == nil || !strings.Contains(err.Error(), "suspicious retransmission") {
		t.Fatalf("error = %v, want suspicious retransmission", err)
	}
	if got != -1 || calls != 2 {
		t.Fatalf("got latency=%d calls=%d, want latency=-1 calls=2", got, calls)
	}
}

func TestRetryMeasuredPingAcceptsRecoveredHTTP(t *testing.T) {
	values := []int{1200, 100}
	calls := 0
	got, err := retryMeasuredPing("http", func() (int, error) {
		v := values[calls]
		calls++
		return v, nil
	})
	if err != nil {
		t.Fatalf("retryMeasuredPing returned error: %v", err)
	}
	if got != 100 || calls != 2 {
		t.Fatalf("got latency=%d calls=%d, want latency=100 calls=2", got, calls)
	}
}

func TestRetryMeasuredPingRejectsPersistentHighLatency(t *testing.T) {
	calls := 0
	got, err := retryMeasuredPing("icmp", func() (int, error) {
		calls++
		return 1200, nil
	})
	if err == nil || !strings.Contains(err.Error(), "latency remains high") {
		t.Fatalf("error = %v, want latency remains high", err)
	}
	if got != -1 || calls != 4 {
		t.Fatalf("got latency=%d calls=%d, want latency=-1 calls=4", got, calls)
	}
}

func TestRetryMeasuredPingStopsOnRetryError(t *testing.T) {
	calls := 0
	got, err := retryMeasuredPing("tcp", func() (int, error) {
		calls++
		if calls == 2 {
			return -1, errors.New("dial failed")
		}
		return 1200, nil
	})
	if err == nil || err.Error() != "dial failed" {
		t.Fatalf("error = %v, want dial failed", err)
	}
	if got != -1 || calls != 2 {
		t.Fatalf("got latency=%d calls=%d, want latency=-1 calls=2", got, calls)
	}
}

func TestBuildProbeResultCalculatesLossFromFailedSamples(t *testing.T) {
	got := buildProbeResult(4, []int{40, 10, 20})
	if !got.OK {
		t.Fatal("expected probe result to be OK")
	}
	if got.RTTMs != 20 {
		t.Fatalf("RTTMs = %d, want 20", got.RTTMs)
	}
	if got.Loss != 25 {
		t.Fatalf("Loss = %v, want 25", got.Loss)
	}
}

func TestBuildProbeResultAllSamplesLost(t *testing.T) {
	got := buildProbeResult(4, nil)
	if got.OK {
		t.Fatal("expected probe result to be failed")
	}
	if got.RTTMs != -1 {
		t.Fatalf("RTTMs = %d, want -1", got.RTTMs)
	}
	if got.Loss != 100 {
		t.Fatalf("Loss = %v, want 100", got.Loss)
	}
}

func TestRollingProbeHistoryAggregatesOneMinuteWindow(t *testing.T) {
	if metricsProbeInterval != 10*time.Second || metricsProbeMedianWindow != time.Minute || metricsProbeWindowSampleCount != 6 {
		t.Fatalf("unexpected probe window config: interval=%s window=%s count=%d", metricsProbeInterval, metricsProbeMedianWindow, metricsProbeWindowSampleCount)
	}
	now := time.Unix(1000, 0)
	history := rollingProbeHistory{}
	results := []ProbeResult{
		{RTTMs: 10, Loss: 0, OK: true},
		{RTTMs: 20, Loss: 0, OK: true},
		{RTTMs: -1, Loss: 100, OK: false},
		{RTTMs: 30, Loss: 0, OK: true},
		{RTTMs: 100, Loss: 0, OK: true},
		{RTTMs: -1, Loss: 100, OK: false},
	}
	for i, result := range results {
		history.add(now.Add(time.Duration(i)*metricsProbeInterval), "example.com", result)
	}

	got := history.snapshot(now.Add(5 * metricsProbeInterval))
	if !got.OK {
		t.Fatal("expected rolling probe result to be OK")
	}
	if got.RTTMs != 25 {
		t.Fatalf("RTTMs = %d, want 25", got.RTTMs)
	}
	if got.Loss != 33.33 {
		t.Fatalf("Loss = %v, want 33.33", got.Loss)
	}
	if len(history.samples) != metricsProbeWindowSampleCount {
		t.Fatalf("samples = %d, want %d", len(history.samples), metricsProbeWindowSampleCount)
	}
}

func TestRollingProbeHistoryExcludesStaleSamplesAndRoundsSixth(t *testing.T) {
	now := time.Unix(2000, 0)
	staleHistory := rollingProbeHistory{}
	staleHistory.add(now.Add(-70*time.Second), "example.com", ProbeResult{RTTMs: -1, Loss: 100, OK: false})
	staleHistory.add(now.Add(-20*time.Second), "example.com", ProbeResult{RTTMs: 20, Loss: 0, OK: true})
	staleHistory.add(now.Add(-10*time.Second), "example.com", ProbeResult{RTTMs: 30, Loss: 0, OK: true})
	staleGot := staleHistory.snapshot(now)
	if !staleGot.OK || staleGot.Loss != 0 || staleGot.RTTMs != 25 {
		t.Fatalf("stale exclusion got %+v, want OK=true Loss=0 RTTMs=25", staleGot)
	}

	sixthHistory := rollingProbeHistory{}
	for i := 0; i < 6; i++ {
		res := ProbeResult{RTTMs: 20, Loss: 0, OK: true}
		if i == 3 {
			res = ProbeResult{RTTMs: -1, Loss: 100, OK: false}
		}
		sixthHistory.add(now.Add(time.Duration(i)*10*time.Second), "example.com", res)
	}
	sixthGot := sixthHistory.snapshot(now.Add(50 * time.Second))
	if !sixthGot.OK || sixthGot.Loss != 16.67 {
		t.Fatalf("1/6 loss got %+v, want OK=true Loss=16.67", sixthGot)
	}
}

func TestMeasureProbeFlagsTCPRetransmissionAsPacketLoss(t *testing.T) {
	if defaultPingTimeout != 3000*time.Millisecond {
		t.Fatalf("defaultPingTimeout = %s, want 3s", defaultPingTimeout)
	}
	oldTCPPing := probeTCPPing
	t.Cleanup(func() { probeTCPPing = oldTCPPing })

	latencies := []int{1200, 100}
	calls := 0
	probeTCPPing = func(target string, defaultPort int, timeout time.Duration) (int, error) {
		if timeout != 3000*time.Millisecond {
			t.Fatalf("timeout = %s, want 3s", timeout)
		}
		v := latencies[calls]
		calls++
		return v, nil
	}

	got := measureProbe(pingModeTCP, "example.com", 1, defaultMetricsTCPPort, newLogger(false))
	if got.OK || got.RTTMs != -1 || got.Loss != 100 {
		t.Fatalf("got %+v, want OK=false RTTMs=-1 Loss=100", got)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}
