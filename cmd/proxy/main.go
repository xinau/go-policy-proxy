package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/google/cel-go/cel"
	"github.com/tailscale/hujson"
)

type policy struct {
	Path string `json:"path"`
	Expr string `json:"expr"`

	prog cel.Program
}

func URLParamsFromRequest(req *http.Request) map[string]string {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		return nil
	}

	params := make(map[string]string, len(rctx.URLParams.Keys))
	for i, key := range rctx.URLParams.Keys {
		params[key] = rctx.URLParams.Values[i]
	}

	return params
}

func (p *policy) Compile() error {
	env, err := cel.NewEnv(
		cel.Variable("url.path", cel.StringType),
		cel.Variable("url.params", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("url.query", cel.MapType(cel.StringType, cel.ListType(cel.StringType))),
	)
	if err != nil {
		return err
	}

	ast, issues := env.Compile(p.Expr)
	if issues != nil && issues.Err() != nil {
		return issues.Err()
	}

	if ast.OutputType() != cel.BoolType {
		return errors.New("output type must be boolean")
	}

	prog, err := env.Program(ast)
	if err != nil {
		return err
	}

	p.prog = prog
	return nil
}

func (p *policy) Validate(req *http.Request) (bool, error) {
	if p.prog == nil {
		return false, errors.New("policy programm can't be nil")
	}

	val, _, err := p.prog.ContextEval(req.Context(), map[string]interface{}{
		"url.path":   req.URL.Path,
		"url.params": URLParamsFromRequest(req),
		"url.query":  req.URL.Query(),
	})
	if err != nil {
		return false, err
	}

	return (val.Value()).(bool), nil
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

	file, err := os.ReadFile(*policiesFileF)
	if err != nil {
		log.Fatalf("fatal: reading policies: %s", err)
	}

	file, err = hujson.Standardize(file)
	if err != nil {
		log.Fatalf("fatal: standardizing policies: %s", err)
	}

	var policies []*policy
	if err := json.Unmarshal(file, &policies); err != nil {
		log.Fatalf("fatal: decoding policies file: %s", err)
	}

	rtr := chi.NewRouter()
	for i, p := range policies {
		if err := p.Compile(); err != nil {
			log.Fatalf("fatal: compiling policy %d: %s", i, err)
		}

		// reassign as local variable due to how go handles for loops and closures
		p := p

		rtr.HandleFunc(p.Path, func(rw http.ResponseWriter, req *http.Request) {
			allowed, err := p.Validate(req)
			if err != nil {
				log.Printf("error: validating request: %s", err)
				rw.WriteHeader(http.StatusInternalServerError)
				return
			}

			if !allowed {
				rw.WriteHeader(http.StatusForbidden)
				return
			}

			proxy.ServeHTTP(rw, req)
		})
	}

	log.Printf("info: listening for requests on %s", *listenAddrF)
	if err := http.ListenAndServe(*listenAddrF, rtr); err != nil {
		log.Fatalf("fatal: listening for requests %s:", err)
	}
}
