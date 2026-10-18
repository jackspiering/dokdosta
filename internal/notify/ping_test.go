package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPingGoesOutWhileHealthyAndNotWhenUnhealthy(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/ping/uuid-1234-secret" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		hits.Add(1)
	}))
	defer srv.Close()
	healthy := true
	lb := &logBuf{}
	p := &Pinger{URL: srv.URL + "/ping/uuid-1234-secret", Interval: time.Hour, Healthy: func() bool { return healthy }, Log: lb.logger(), Redact: NewRedactor(srv.URL + "/ping/uuid-1234-secret"), Timeout: time.Second, Client: srv.Client()}
	p.Tick(context.Background())
	healthy = false
	p.Tick(context.Background())
	p.Tick(context.Background())
	st := p.Status()
	if hits.Load() != 1 || st.Sent != 1 || st.Skipped != 2 {
		t.Fatalf("hits=%d %+v", hits.Load(), st)
	}
}

func TestPingFailureIsCountedAndRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 404) }))
	url := srv.URL + "/ping/uuid-1234-secret"
	lb := &logBuf{}
	p := &Pinger{URL: url, Interval: time.Hour, Log: lb.logger(), Redact: NewRedactor(url), Timeout: time.Second}
	p.Tick(context.Background())
	srv.Close() // now the connection is refused, and the error quotes the URL
	p.Tick(context.Background())
	st := p.Status()
	if st.Failed != 2 || st.Sent != 0 {
		t.Fatalf("%+v", st)
	}
	for _, text := range []string{lb.String(), st.LastError} {
		if strings.Contains(text, "uuid-1234-secret") {
			t.Errorf("the ping URL leaked: %s", text)
		}
	}
}
