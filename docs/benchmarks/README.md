# phase 0 benchmarks

Raven makes two evidence-based choices before Phase 1 depends on them.

## extraction corpus

Populate `testdata/articles/manifest.json` with 20–50 articles from the feeds Cassie actually reads. Include standard articles, feed-provided full text, newsletter pages, heavy HTML, duplicate coverage, broken metadata, redirects, and extraction failures. Do not commit paywalled/private article bodies without permission.

For each candidate, record:

- title/body usefulness and obvious garbage retained
- author/date/canonical URL/lead-image extraction
- wall-clock latency and failure mode
- deployability and operational cost

The selected adapter needs the best *whole-system* result, not the best performance on one pristine article.

## vector deployment

Phase 0 verifies whether sqlite-vec can be loaded safely in the intended Go binary and deployment target. The fallback decision is recorded before Phase 2: defer vectors, use a static extension, or choose a different local vector path. No embedding schema reaches production until this is proven.
