# go-policy-proxy

A reverse proxy for filtering requests using user defined policies.

Requests are only forwarded to a target if they are allowed by a policy,
therefore matching it's url path and rule. Rules are written in [CEL (Common
Expression Language)][github:google:cel-spec] and can rejected requests based on
request metadata like url parameters and query, request headers, i.e.

```json
[{
    "path": "/v1/users/{firstname}",
    "expr": "url.params[\"firstname\"] == \"grace\" && req.header[\"lastname\"][0] == \"hopper\"",
}]
```


## Usage

The policy proxy can be build using a recent Go toolchain and started by
providing a target url and policies file.

```
go build -o policy-proxy ./cmd/proxy
./policy-proxy --target-url=https://example.com --policies-file=./policies.jwcc
```


## Configuration

The proxy can be configured through the following command-line flags

_--listen-addr_ (default :8000):  
    Address to listen for incoming requests to the proxy.

_--metrics-addr_ (default :4000):
    Address to expose Prometheus /metrics endpoint on. 

_--policies-file_:  
    Path to file containing request policies written in JWCC.

_--target-url_:  
    Base URL of target where requests are being forwarded to. If the URL
    contains a path element it will be prepended to the path inside of a policy.


## Policies

The polices file is written in [JWCC (JSON with Commas and
Comments)][nigeltao:jwcc] using the following format.

```
[{
// url path pattern to match requests against
"path": string,

// cel programm for validating request metadata
"rule": string,
} ... ]
```

A policy's rule is a CEL programm with access to the following request metadata
in it's environment.

_req.header_ (map[string][]string):  
    HTTP headers of request.

_url.params_ (map[string]string):  
    URL parameters by name (defined inside the policy's path).

_url.path_ (string):  
    URL path of the request.

_url.query_ (map[string][]string):  
    URL query of request.


## Metrics

The policy proxy exposes the following metrics in Prometheus formate under a
seperate address under the `/metrics` enpoint.

_http_request_total_ (Counter):  
	Total number of HTTP requests.  

_http_request_duration_seconds_ (Histogram):  
	Histogram of latencies for HTTP request in seconds.

_http_request_denied_total_ (Counter):  
	Total number of denied HTTP requests.

_http_request_in_flight_ (Gauge):  
	Number of HTTP requests currently serving.


## LICENSE

This project is under [MIT license](./LICENSE).


[github:google:cel-spec]: https://github.com/google/cel-spec
[nigeltao:jwcc]: https://nigeltao.github.io/blog/2021/json-with-commas-comments.html
