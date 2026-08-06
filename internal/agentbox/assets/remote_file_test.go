package assets

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var publicTestAddress = net.IPAddr{IP: net.ParseIP("93.184.216.34")}

type sequenceResolver struct {
	mutex   sync.Mutex
	answers map[string][][]net.IPAddr
	calls   map[string]int
}

func (r *sequenceResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	rHost := strings.TrimSuffix(host, ".")
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	sequences := r.answers[rHost]
	if len(sequences) == 0 {
		return nil, fmt.Errorf("unexpected lookup for %s", host)
	}
	index := r.calls[rHost]
	r.calls[rHost]++
	if index >= len(sequences) {
		index = len(sequences) - 1
	}
	return append([]net.IPAddr(nil), sequences[index]...), nil
}

func staticAnswers(hosts ...string) map[string][][]net.IPAddr {
	answers := map[string][][]net.IPAddr{}
	for _, host := range hosts {
		answers[host] = [][]net.IPAddr{{publicTestAddress}}
	}
	return answers
}

func testFetcher(server *httptest.Server, resolver ipResolver) (*SecureRemoteFileFetcher, *[]string) {
	dials := []string{}
	dialer := &net.Dialer{Timeout: time.Second}
	return &SecureRemoteFileFetcher{
		resolver: resolver,
		dialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dials = append(dials, address)
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
		overallTimeout:        time.Second,
		connectTimeout:        time.Second,
		tlsHandshakeTimeout:   time.Second,
		responseHeaderTimeout: time.Second,
		maxRedirects:          5,
	}, &dials
}

func remoteURL(server *httptest.Server, host string, path string) string {
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	return "http://" + net.JoinHostPort(host, port) + path
}

func TestSecureRemoteFileFetcherPinsValidatedPublicAddress(t *testing.T) {
	var receivedHost string
	var receivedAuthorization string
	var receivedCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		receivedAuthorization = r.Header.Get("authorization")
		receivedCookie = r.Header.Get("cookie")
		if r.URL.Query().Get("token") != "signed-value" {
			t.Fatalf("signed query was not preserved: %s", r.URL.String())
		}
		_, _ = w.Write([]byte("artifact-bytes"))
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, dials := testFetcher(server, resolver)
	contents, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/artifact?token=signed-value"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "artifact-bytes" {
		t.Fatalf("contents = %q", contents)
	}
	if len(*dials) != 1 || !strings.HasPrefix((*dials)[0], "93.184.216.34:") {
		t.Fatalf("dial destinations = %#v", *dials)
	}
	if !strings.HasPrefix(receivedHost, "files.example:") || receivedAuthorization != "" || receivedCookie != "" {
		t.Fatalf("host=%q authorization=%q cookie=%q", receivedHost, receivedAuthorization, receivedCookie)
	}
}

func TestSecureRemoteFileFetcherRejectsNonPublicAndAmbiguousDestinations(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/file",
		"http://10.0.0.1/file",
		"http://100.100.100.200/file",
		"http://169.254.169.254/latest/meta-data",
		"http://192.0.2.10/file",
		"http://198.18.0.1/file",
		"http://[::1]/file",
		"http://[fc00::1]/file",
		"http://[fe80::1]/file",
		"http://[2001:db8::1]/file",
		"http://localhost/file",
		"http://service/file",
		"http://127.1/file",
		"http://2130706433/file",
		"http://0x7f000001/file",
		"http://metadata.google.internal/file",
		"file:///tmp/artifact",
		"sandbox:/mnt/data/artifact",
		"https://user:secret@files.example/file",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			dials := 0
			fetcher := &SecureRemoteFileFetcher{
				resolver: &sequenceResolver{answers: staticAnswers("files.example")},
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					dials++
					return nil, errorsForTest("unexpected dial")
				},
				overallTimeout: time.Second,
			}
			if _, err := fetcher.Fetch(t.Context(), rawURL, 1024); err == nil || !strings.Contains(err.Error(), "rejected") {
				t.Fatalf("Fetch(%q) error = %v", rawURL, err)
			}
			if dials != 0 {
				t.Fatalf("Fetch(%q) dialed %d times", rawURL, dials)
			}
		})
	}

	mixed := &SecureRemoteFileFetcher{
		resolver: &sequenceResolver{answers: map[string][][]net.IPAddr{
			"files.example": {{publicTestAddress, {IP: net.ParseIP("10.0.0.8")}}},
		}},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("mixed public/private DNS answer reached dial")
			return nil, nil
		},
		overallTimeout: time.Second,
	}
	if _, err := mixed.Fetch(t.Context(), "https://files.example/file", 1024); err == nil {
		t.Fatal("mixed public/private DNS answer was accepted")
	}
}

type testError string

func (e testError) Error() string { return string(e) }

func errorsForTest(message string) error { return testError(message) }

func TestSecureRemoteFileFetcherRevalidatesRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/safe":
			http.Redirect(w, r, remoteURLFromRequest(r, "cdn.example", "/final"), http.StatusFound)
		case "/private":
			http.Redirect(w, r, remoteURLFromRequest(r, "private.example", "/final"), http.StatusFound)
		case "/final":
			_, _ = w.Write([]byte("redirected"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: map[string][][]net.IPAddr{
		"files.example":   {{publicTestAddress}},
		"cdn.example":     {{publicTestAddress}},
		"private.example": {{{IP: net.ParseIP("10.0.0.9")}}},
	}}
	fetcher, dials := testFetcher(server, resolver)
	contents, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/safe"), 1024)
	if err != nil || string(contents) != "redirected" {
		t.Fatalf("safe redirect contents=%q err=%v", contents, err)
	}
	if len(*dials) != 2 {
		t.Fatalf("safe redirect dials = %#v", *dials)
	}

	fetcher, dials = testFetcher(server, resolver)
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/private"), 1024); err == nil || !strings.Contains(err.Error(), "unsafe redirect destination") {
		t.Fatalf("private redirect error = %v", err)
	}
	if len(*dials) != 1 {
		t.Fatalf("private redirect dialed destination: %#v", *dials)
	}
}

func remoteURLFromRequest(request *http.Request, host string, path string) string {
	_, port, _ := net.SplitHostPort(request.Host)
	return "http://" + net.JoinHostPort(host, port) + path
}

func TestSecureRemoteFileFetcherRejectsDNSRebinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("must not arrive"))
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: map[string][][]net.IPAddr{
		"files.example": {
			{publicTestAddress},
			{{IP: net.ParseIP("169.254.169.254")}},
		},
	}}
	fetcher, dials := testFetcher(server, resolver)
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/file"), 1024); err == nil || !strings.Contains(err.Error(), "not public") {
		t.Fatalf("rebinding error = %v", err)
	}
	if len(*dials) != 0 {
		t.Fatalf("rebound destination was dialed: %#v", *dials)
	}

	resolver = &sequenceResolver{answers: map[string][][]net.IPAddr{
		"files.example": {
			{publicTestAddress},
			{{IP: net.ParseIP("1.1.1.1")}},
		},
	}}
	fetcher, dials = testFetcher(server, resolver)
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/file"), 1024); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed-resolution error = %v", err)
	}
	if len(*dials) != 0 {
		t.Fatalf("changed destination was dialed: %#v", *dials)
	}
}

func TestSecureRemoteFileFetcherEnforcesDeclaredAndStreamingSizeLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declared":
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
		case "/streamed":
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = w.Write([]byte("01234567890"))
		}
	}))
	defer server.Close()

	for _, path := range []string{"/declared", "/streamed"} {
		resolver := &sequenceResolver{answers: staticAnswers("files.example")}
		fetcher, _ := testFetcher(server, resolver)
		if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", path), 10); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("%s error = %v", path, err)
		}
	}
}

func TestSecureRemoteFileFetcherEnforcesHeaderAndOverallTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/headers":
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		case "/body":
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		}
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, _ := testFetcher(server, resolver)
	fetcher.responseHeaderTimeout = 20 * time.Millisecond
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/headers"), 1024); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("header timeout error = %v", err)
	}

	resolver = &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, _ = testFetcher(server, resolver)
	fetcher.overallTimeout = 20 * time.Millisecond
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/body"), 1024); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("overall timeout error = %v", err)
	}
}

func TestSecureRemoteFileFetcherEnforcesConnectTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("must not arrive"))
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, _ := testFetcher(server, resolver)
	fetcher.connectTimeout = 20 * time.Millisecond
	fetcher.dialContext = func(ctx context.Context, _ string, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/file"), 1024); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("connect timeout error = %v", err)
	}
}

func TestSecureRemoteFileFetcherRejectsHTTPFailureAndRedirectLoops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/loop":
			http.Redirect(w, r, remoteURLFromRequest(r, "files.example", "/loop"), http.StatusFound)
		}
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, _ := testFetcher(server, resolver)
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/missing"), 1024); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("HTTP failure error = %v", err)
	}

	resolver = &sequenceResolver{answers: staticAnswers("files.example")}
	fetcher, _ = testFetcher(server, resolver)
	fetcher.maxRedirects = 2
	if _, err := fetcher.Fetch(t.Context(), remoteURL(server, "files.example", "/loop"), 1024); err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("redirect loop error = %v", err)
	}
}
