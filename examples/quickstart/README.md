# Quickstart

A ~50-line tour of the ggscale Go SDK.

```sh
make up                                  # in repo root
export GGSCALE_API_KEY=<key from dashboard>
cd sdk-go && go run ./examples/quickstart
```

Expected output: one or more `#1  user=…  score=…` lines.

On a fresh stack, signup needs a manual email-verification round-trip — open MailHog at <http://localhost:8025>, copy the verification token, and POST it to `/v1/auth/verify`. See [`docs/SMOKE_TESTS.md` §3](../../../docs/SMOKE_TESTS.md) for the curl invocation.
