package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
)

type policy struct {
	Path string `json:"path"`
}

var (
	listenAddrF   = flag.String("listen-addr", ":8080", "address to listen")
	policiesFileF = flag.String("policies-file", "", "filepath to security policies")
	targetURLF    = flag.String("target-url", "", "target url to provide access to")
)

func main() {
	flag.Parse()

	target, err := url.Parse(*targetURLF)
	if err != nil {
		log.Fatalf("fatal: parsing target url: %s", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	file, err := os.Open(*policiesFileF)
	if err != nil {
		log.Fatalf("fatal: reading policies: %s", err)
	}

	var policies []policy
	if err := json.NewDecoder(file).Decode(&policies); err != nil {
		log.Fatalf("fatal: decoding policies file: %s", err)
	}

	rtr := chi.NewRouter()
	for _, p := range policies {
		rtr.HandleFunc(p.Path, func(rw http.ResponseWriter, req *http.Request) {
			proxy.ServeHTTP(rw, req)
		})
	}

	log.Printf("info: listening for requests on %s", *listenAddrF)
	if err := http.ListenAndServe(*listenAddrF, rtr); err != nil {
		log.Fatalf("fatal: listening for requests %s:", err)
	}
}
