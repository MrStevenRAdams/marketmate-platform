// ============================================================================
// MarketMate Egress Proxy
// ============================================================================
// Forwards HTTP requests to whitelisted external APIs (Temu, Evri) from a
// fixed us-central1 Cloud Run service. The egress IP is stable via Cloud NAT,
// matching what was whitelisted with Temu and Evri.
//
// Protocol:
//   POST /forward
//   Headers:
//     X-Proxy-Secret: <secret>        — must match PROXY_SECRET env var
//   Body (JSON):
//     {
//       "url":     "https://openapi-b-eu.temu.com/openapi/router",
//       "method":  "POST",                         // default POST
//       "headers": { "Content-Type": "application/json", ... },
//       "body":    "<raw request body string>"
//     }
//   Response: raw upstream response (status + body forwarded as-is)
//
// Health check: GET /healthz → 200 OK
// ============================================================================

package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"encoding/json"
)

// allowedHosts — only these domains can be proxied
var allowedHosts = []string{
	"openapi-b-eu.temu.com",
	"openapi-b.temu.com",
	// Amazon SP-API regional endpoints
	"sellingpartnerapi-eu.amazon.com",
	"sellingpartnerapi-na.amazon.com",
	"sellingpartnerapi-fe.amazon.com",
	"sandbox.sellingpartnerapi-na.amazon.com",
	// Amazon LWA (Login with Amazon) — token endpoint
	"api.amazon.com",
	// Evri Routing Web Service v4 (real endpoints — replaces old api.hermes.evri.com stubs)
	"sit.hermes-europe.co.uk",   // SIT/test
	"www.hermes-europe.co.uk",   // production
}

var (
	proxySecret = os.Getenv("PROXY_SECRET")
	port        = os.Getenv("PORT")
)

type forwardRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
}

func isAllowed(rawURL string) bool {
	for _, host := range allowedHosts {
		if strings.Contains(rawURL, host) {
			return true
		}
	}
	return false
}

func handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check
	if proxySecret != "" && r.Header.Get("X-Proxy-Secret") != proxySecret {
		log.Printf("[proxy] Rejected: bad or missing X-Proxy-Secret from %s", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req forwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	if !isAllowed(req.URL) {
		log.Printf("[proxy] Blocked disallowed URL: %s", req.URL)
		http.Error(w, "target URL not in allowlist", http.StatusForbidden)
		return
	}

	method := req.Method
	if method == "" {
		method = http.MethodPost
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	upstream, err := http.NewRequest(method, req.URL, bodyReader)
	if err != nil {
		http.Error(w, "failed to build upstream request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for k, v := range req.Headers {
		upstream.Header.Set(k, v)
	}

	log.Printf("[proxy] → %s %s", method, req.URL)
	resp, err := httpClient.Do(upstream)
	if err != nil {
		log.Printf("[proxy] upstream error: %v", err)
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response headers and status
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	log.Printf("[proxy] ← %d from %s", resp.StatusCode, req.URL)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func main() {
	if port == "" {
		port = "8080"
	}

	if proxySecret == "" {
		log.Println("[proxy] WARNING: PROXY_SECRET not set — proxy is unauthenticated")
	}

	http.HandleFunc("/forward", handleForward)
	http.HandleFunc("/healthz", handleHealth)

	log.Printf("[proxy] Listening on :%s — allowed hosts: %v", port, allowedHosts)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
