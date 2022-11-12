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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tailscale/hujson"
)

var (
	httpRequestsCounterM = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_request_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{},
	)

	httpRequestsDurationM = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Histogram of latencies for HTTP request in seconds.",
			Buckets: []float64{.25, .5, 1, 2.5, 5, 10},
		},
		[]string{},
	)

	httpRequestsDeniedM = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "http_request_denied_total",
			Help: "Total number of denied HTTP requests.",
		},
	)

	httpRequestsInFlightM = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_request_in_flight",
			Help: "Number of HTTP requests currently serving.",
		},
	)
)

func GetCounterMetricsMiddleware(c *prometheus.CounterVec, opts ...promhttp.Option) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return promhttp.InstrumentHandlerCounter(c, h, opts...)
	}
}

func GetDurationMetricsMiddleware(obs prometheus.ObserverVec, opts ...promhttp.Option) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return promhttp.InstrumentHandlerDuration(obs, h, opts...)
	}
}

func GetInFlightMetricsMiddleware(g prometheus.Gauge) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return promhttp.InstrumentHandlerInFlight(g, h)
	}
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

type Policy struct {
	Path string `json:"path"`
	Expr string `json:"expr"`

	prog cel.Program
}

func (p *Policy) Compile() error {
	env, err := cel.NewEnv(
		cel.Variable("req.header", cel.MapType(cel.StringType, cel.ListType(cel.StringType))),
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

	prog, err := env.Program(ast, cel.EvalOptions(
		cel.OptOptimize,
	))
	if err != nil {
		return err
	}

	p.prog = prog
	return nil
}

func (p *Policy) Validate(req *http.Request) (bool, error) {
	if p.prog == nil {
		return false, errors.New("policy programm can't be nil")
	}

	val, _, err := p.prog.ContextEval(req.Context(), map[string]interface{}{
		"req.header": req.Header,
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
	listenAddrF   = flag.String("listen-addr", ":8000", "address to listen for proxy requests")
	metricsAddrF  = flag.String("metrics-addr", ":4000", "address to expose /metrics endpoint")
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

	var policies []*Policy
	if err := json.Unmarshal(file, &policies); err != nil {
		log.Fatalf("fatal: decoding policies file: %s", err)
	}

	rtr := chi.NewRouter()
	rtr.Use(
		GetCounterMetricsMiddleware(httpRequestsCounterM),
		GetDurationMetricsMiddleware(httpRequestsDurationM),
		GetInFlightMetricsMiddleware(httpRequestsInFlightM),
	)

	for i, p := range policies {
		if err := p.Compile(); err != nil {
			log.Fatalf("fatal: compiling policy %d: %s", i, err)
		}

		// reassign as local variable due to how go handles for loops and closures
		p := p

		rtr.HandleFunc(p.Path, func(rw http.ResponseWriter, req *http.Request) {
			allowed, err := p.Validate(req)
			if err != nil || !allowed {
				httpRequestsDeniedM.Inc()
				rw.WriteHeader(http.StatusForbidden)
				return
			}

			proxy.ServeHTTP(rw, req)
		})
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		httpRequestsCounterM,
		httpRequestsDurationM,
		httpRequestsDeniedM,
		httpRequestsInFlightM,
	)

	httpRequestsCounterM.WithLabelValues().Add(0)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

		log.Printf("info: metrics endpoint avaiable on %s", *metricsAddrF)
		if err := http.ListenAndServe(*metricsAddrF, mux); err != nil {
			log.Fatalf("fatal: listening for requests %s:", err)
		}
	}()

	log.Printf("info: listening for requests on %s", *listenAddrF)
	if err := http.ListenAndServe(*listenAddrF, rtr); err != nil {
		log.Fatalf("fatal: listening for requests %s:", err)
	}
}
