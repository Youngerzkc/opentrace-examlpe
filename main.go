package main

import (
	"fmt"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"
	"sourcegraph.com/sourcegraph/appdash/traceapp"
	"time"
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`<a href="/home"> Click here to start a request </a>`))
}
func homeHandler(w http.ResponseWriter, r *http.Request) {
	span := opentracing.StartSpan("/home")
	defer span.Finish()
	w.Write([]byte("Request started"))
	// Since we have to inject our span into the HTTP headers, we create a request
	asyncReq, _ := http.NewRequest("GET", "http://localhost:8080/async", nil)
	// Inject the span context into the header
	err := span.Tracer().Inject(span.Context(),
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(asyncReq.Header))
	if err != nil {
		log.Fatalf("Could not inject span context into header: %v", err)
	}
	go func() {
		if _, err := http.DefaultClient.Do(asyncReq); err != nil {
			span.SetTag("error", true)
			span.LogEvent(fmt.Sprintf("GET /async error: %v", err))
		}
	}()
	_, err = http.Get("http://localhost:8080/service")
	if err != nil {
		ext.Error.Set(span, true)
		span.LogEventWithPayload("Get service error ", err)
	}
	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
	w.Write([]byte("Request done!"))
}

// Mocks a service endpoint that makes a DB call
func serviceHandler(w http.ResponseWriter, r *http.Request) {
	// ...
	var sq opentracing.Span
	opName := r.URL.Path

	wireContext, err := opentracing.GlobalTracer().Extract(opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(r.Header))
	if err != nil {
		sq = opentracing.StartSpan(opName)
	} else {
		sq = opentracing.StartSpan(opName, opentracing.ChildOf(wireContext))
	}
	defer sq.Finish()

	http.Get("http://localhost:8080/db")
	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
	// ...
}

// Mocks a DB call
func dbHandler(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
	// here would be the actual call to a DB.
}
func main() {

	store := appdash.NewMemoryStore()

	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		log.Fatal(err)
	}
	collectorPort := l.Addr().(*net.TCPAddr).Port
	collectorAdd := fmt.Sprintf(":%d", collectorPort)

	cs := appdash.NewServer(l, appdash.NewLocalCollector(store))

	go cs.Start()
	appdashPort := 8700
	appdashURLStr := fmt.Sprintf("http://localhost:%d", appdashPort)
	appdashURL, err := url.Parse(appdashURLStr)
	if err != nil {
		log.Fatalf("Error parsing %s: %s", appdashURLStr, err)
	}
	fmt.Printf("To see your traces, go to %s/traces\n", appdashURL)
	tapp, err := traceapp.New(nil, appdashURL)
	if err != nil {
		log.Fatal(err)
	}
	tapp.Store = store
	tapp.Queryer = store
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", appdashPort), tapp))
	}()

	// tracer := appdashot.NewTracer(appdash.NewRemoteCollector(collectorPort))
	tracer := appdashot.NewTracer(appdash.NewRemoteCollector(collectorAdd))
	opentracing.InitGlobalTracer(tracer)

	port := 8080
	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/home", homeHandler)
	mux.HandleFunc("/async", serviceHandler)
	mux.HandleFunc("/service", serviceHandler)
	mux.HandleFunc("/db", dbHandler)
	fmt.Printf("Go to http://localhost:%d/home to start a request!\n", port)
	log.Fatal(http.ListenAndServe(addr, mux))

}
