package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Units
const (
	_        = iota
	KB int64 = 1 << (10 * iota)
	MB
	GB
	TB
)

var cert string
var key string
var port string
var metricsEnabled bool

func init() {
	flag.StringVar(&cert, "cert", "", "give me a certificate")
	flag.StringVar(&key, "key", "", "give me a key")
	flag.StringVar(&port, "port", "80", "give me a port number")
	flag.BoolVar(&metricsEnabled, "metrics", false, "enable collecting metrics")
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func main() {
	flag.Parse()
	if metricsEnabled {
		publishExpvarMetrics()
	}

	http.HandleFunc("/data", wrapHandler(dataHandler))
	http.HandleFunc("/echo", wrapHandler(echoHandler))
	http.HandleFunc("/bench", wrapHandler(benchHandler))
	http.HandleFunc("/", wrapHandler(whoamiHandler))
	http.HandleFunc("/api", wrapHandler(apiHandler))
	http.HandleFunc("/health", wrapHandler(healthHandler))

	fmt.Println("Starting up on port " + port)

	if len(cert) > 0 && len(key) > 0 {
		log.Fatal(http.ListenAndServeTLS(":"+port, cert, key, nil))
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func wrapHandler(handler http.HandlerFunc) http.HandlerFunc {
	if !metricsEnabled {
		return handler
	}
	return metricsMiddleware{nextHandler: handler}.ServeHTTP
}

func benchHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprint(w, "1")
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			return
		}

		printBinary(p)
		err = conn.WriteMessage(messageType, p)
		if err != nil {
			return
		}
	}
}

func printBinary(s []byte) {
	fmt.Printf("Received b:")
	for n := 0; n < len(s); n++ {
		fmt.Printf("%d,", s[n])
	}
	fmt.Printf("\n")
}

// ################################################################################################
// DATA

func dataHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := url.Parse(r.URL.String())
	queryParams := u.Query()

	size, err := strconv.ParseInt(queryParams.Get("size"), 10, 64)
	if err != nil {
		size = 1
	}
	if size < 0 {
		size = 0
	}

	unit := queryParams.Get("unit")
	switch strings.ToLower(unit) {
	case "kb":
		size *= KB
	case "mb":
		size *= MB
	case "gb":
		size *= GB
	case "tb":
		size *= TB
	}

	attachment, err := strconv.ParseBool(queryParams.Get("attachment"))
	if err != nil {
		attachment = false
	}

	content := fillContentPooled(size)
	defer content.Close()

	if attachment {
		w.Header().Add("Content-Disposition", "Attachment")
		http.ServeContent(w, r, "data.txt", time.Now(), bytes.NewReader(content.Bytes()))
		return
	}

	if _, err := io.Copy(w, content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func fillContent(length int64) *bytes.Buffer {
	b := &bytes.Buffer{}
	writeContent(b, length)
	return b
}

func fillContentPooled(length int64) *PooledBuffer {
	b := GetPooledBuffer()
	writeContent(b, length)
	return b
}

func writeContent(w byteRuneWriter, length int64) {
	if length <= 0 {
		return
	}
	_, _ = w.WriteRune('|')
	for i := 1; i < (int(length) - 1); i++ {
		_ = w.WriteByte(CHARSET[i%len(CHARSET)])
	}
	if length > 1 {
		_, _ = w.WriteRune('|')
	}
}

type byteRuneWriter interface {
	io.ByteWriter
	WriteRune(r rune) (n int, err error)
}

const CHARSET = "-ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// ################################################################################################
// WHOAMI

func whoamiHandler(w http.ResponseWriter, req *http.Request) {
	u, _ := url.Parse(req.URL.String())
	wait := u.Query().Get("wait")
	if len(wait) > 0 {
		duration, err := time.ParseDuration(wait)
		if err == nil {
			time.Sleep(duration)
		}
	}

	hostInfoOnce.Do(func() {
		b := strings.Builder{}
		writeHostInfo(&b)
		hostInfo = b.String()
	})
	_, _ = fmt.Fprint(w, hostInfo)

	_, _ = fmt.Fprintln(w, "RemoteAddr:", req.RemoteAddr)
	if err := req.Write(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func writeHostInfo(w io.Writer) {
	hostname, _ := os.Hostname()
	_, _ = fmt.Fprintln(w, "Hostname:", hostname)

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		// handle err
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			_, _ = fmt.Fprintln(w, "IP:", ip)
		}
	}
}

var hostInfo string
var hostInfoOnce = sync.Once{}

// ################################################################################################

func apiHandler(w http.ResponseWriter, req *http.Request) {
	hostname, _ := os.Hostname()

	data := struct {
		Hostname string      `json:"hostname,omitempty"`
		IP       []string    `json:"ip,omitempty"`
		Headers  http.Header `json:"headers,omitempty"`
		URL      string      `json:"url,omitempty"`
		Host     string      `json:"host,omitempty"`
		Method   string      `json:"method,omitempty"`
	}{
		Hostname: hostname,
		IP:       []string{},
		Headers:  req.Header,
		URL:      req.URL.RequestURI(),
		Host:     req.Host,
		Method:   req.Method,
	}

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		// handle err
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				data.IP = append(data.IP, ip.String())
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type healthState struct {
	StatusCode int
}

var currentHealthState = healthState{http.StatusOK}
var mutexHealthState = &sync.RWMutex{}

func healthHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		var statusCode int

		if err := json.NewDecoder(req.Body).Decode(&statusCode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		fmt.Printf("Update health check status code [%d]\n", statusCode)

		mutexHealthState.Lock()
		defer mutexHealthState.Unlock()
		currentHealthState.StatusCode = statusCode
	} else {
		mutexHealthState.RLock()
		defer mutexHealthState.RUnlock()
		w.WriteHeader(currentHealthState.StatusCode)
	}
}
