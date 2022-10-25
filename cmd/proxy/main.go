package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

type policy struct {
	Path string `json:"path"`
}

func getHandleFunc(p policy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!\n"))
	}
}

var (
	listenAddrF   = flag.String("listen-addr", ":8080", "address to listen")
	policiesFileF = flag.String("policies-file", "", "filepath of security policies")
)

func main() {
	flag.Parse()

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
		fn := getHandleFunc(p)
		rtr.HandleFunc(p.Path, fn)
	}

	log.Printf("info: listening for requests on %s", *listenAddrF)
	if err := http.ListenAndServe(*listenAddrF, rtr); err != nil {
		log.Fatalf("fatal: listening for requests %s:", err)
	}
}
