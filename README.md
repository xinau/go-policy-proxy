# go-safe-proxy

A reverse proxy for providing access to backends based on custom security
policies.

All requests are evaluated against a user defined policies based on various
request aspects, only if a policy is evaluated successfuly the request is
forwarded to the backend and otherwise declined.

## LICENSE

This project is under [MIT license](./LICENSE).

