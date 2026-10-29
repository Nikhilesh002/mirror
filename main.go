package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	version     = "1.0.0"
	maxRequests = 1000
)

type MirrorMetadata struct {
	ID          string    `json:"id"`
	ReceivedAt  time.Time `json:"received_at"`
	Stored      bool      `json:"stored"`
	Version     string    `json:"version"`
	RequestSize int64     `json:"request_size"`
}

type RequestData struct {
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	URL           string              `json:"url"`
	Host          string              `json:"host"`
	Proto         string              `json:"proto"`
	RemoteAddr    string              `json:"remote_addr"`
	ContentLength int64               `json:"content_length"`
	Headers       map[string][]string `json:"headers"`
	Query         map[string][]string `json:"query"`
	Body          string              `json:"body"`
}

type MirrorResponse struct {
	Mirror  MirrorMetadata `json:"mirror"`
	Request RequestData    `json:"request"`
}

var (
	mu      sync.RWMutex
	records []MirrorResponse
)

func main() {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("./static")))
	mux.HandleFunc("/api/health", health)
	mux.HandleFunc("/api/mirror", mirror)
	mux.HandleFunc("/api/store", store)
	mux.HandleFunc("/api/requests", list)
	mux.HandleFunc("/api/request/", get)

	mux.HandleFunc("/api/exp/conn-reset", connReset)
	mux.HandleFunc("/api/exp/wait300sec", wait300sec)

	log.Println("Mirror running on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func connReset(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking unsupported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hj.Hijack()
	if err != nil {
		log.Printf("Hijack failed: %v", err)
		return
	}

	// A TLS listener normally returns *tls.Conn after hijacking.
	// Retrieve the underlying TCP connection.
	type netConner interface {
		NetConn() net.Conn
	}

	rawConn := conn
	if tc, ok := conn.(netConner); ok {
		rawConn = tc.NetConn()
	}

	tcpConn, ok := rawConn.(*net.TCPConn)
	if !ok {
		log.Printf("Not a TCP connection: %T", rawConn)
		_ = conn.Close()
		return
	}

	// Linux: closing a TCP socket with SO_LINGER=0
	// requests an abortive close (RST).
	if err := tcpConn.SetLinger(0); err != nil {
		log.Printf("SetLinger failed: %v", err)
		_ = tcpConn.Close()
		return
	}

	log.Printf(
		"Forcing TCP reset for %s %s from %s",
		r.Method, r.URL.Path, r.RemoteAddr,
	)

	// Close the underlying TCP socket directly. Do not write
	// an HTTP status code or send a response body.
	if err := tcpConn.Close(); err != nil {
		log.Printf("TCP close: %v", err)
	}
}

func wait300sec(w http.ResponseWriter, r *http.Request) {
    log.Printf("Started 300-second wait: %s %s", r.Method, r.URL.Path)

    timer := time.NewTimer(300 * time.Second)
    defer timer.Stop()

    select {
    case <-timer.C:
        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("OK - response sent after 300 seconds\n"))
        log.Println("Completed 300-second wait")
    case <-r.Context().Done():
        // Client disconnected or cancelled the request.
        log.Println("Client disconnected during 300-second wait")
        return
    }
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func mirror(w http.ResponseWriter, r *http.Request) {
	req, err := capture(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := MirrorResponse{
		Mirror: MirrorMetadata{
			ID: randomID(), ReceivedAt: time.Now(), Stored: false,
			Version: version, RequestSize: int64(len(req.Body)),
		},
		Request: req,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func store(w http.ResponseWriter, r *http.Request) {
	req, err := capture(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := MirrorResponse{
		Mirror: MirrorMetadata{
			ID: randomID(), ReceivedAt: time.Now(), Stored: true,
			Version: version, RequestSize: int64(len(req.Body)),
		},
		Request: req,
	}

	mu.Lock()
	records = append(records, resp)
	sort.Slice(records, func(i, j int) bool {
		return records[i].Mirror.ReceivedAt.After(records[j].Mirror.ReceivedAt)
	})
	if len(records) > maxRequests {
		records = records[:maxRequests]
	}
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func list(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests for this endpoint
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	defer mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

func get(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests for this endpoint
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/request/")
	mu.RLock()
	defer mu.RUnlock()

	for _, v := range records {
		if v.Mirror.ID == id {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(v)
			return
		}
	}
	http.NotFound(w, r)
}

func capture(r *http.Request) (RequestData, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return RequestData{}, err
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	h := map[string][]string{}
	for k, v := range r.Header {
		h[k] = append([]string{}, v...)
	}

	q := map[string][]string{}
	for k, v := range r.URL.Query() {
		q[k] = append([]string{}, v...)
	}

	return RequestData{
		Method:        r.Method,
		Path:          r.URL.Path,
		URL:           r.URL.String(),
		Host:          r.Host,
		Proto:         r.Proto,
		RemoteAddr:    r.RemoteAddr,
		ContentLength: r.ContentLength,
		Headers:       h,
		Query:         q,
		Body:          string(body),
	}, nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
