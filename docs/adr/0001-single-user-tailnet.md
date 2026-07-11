# ADR 0001: single reader, tailnet transport

**status:** accepted

Raven v0.1 serves one private reader. The Go service is reached through Tailscale HTTPS and authenticates API calls with a revocable bearer token.

We are not building user accounts, sharing, external access, or authorization roles. This removes a large amount of security and migration surface while Raven proves its actual reading loop. Stable UUIDs and clean service boundaries preserve an eventual path to multiple accounts without pretending that path already exists.
