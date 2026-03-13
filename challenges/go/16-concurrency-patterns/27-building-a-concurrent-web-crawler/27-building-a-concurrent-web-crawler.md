# 27. Building a Concurrent Web Crawler

<!--
difficulty: insane
concepts: [web-crawler, bounded-concurrency, url-deduplication, depth-limiting, politeness, robots-txt, sitemap]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [goroutines, channels, sync-primitives, worker-pool-pattern, bounded-parallelism, context, http-client]
-->

## The Challenge

Build a concurrent web crawler that discovers and fetches pages from a website. The crawler must respect concurrency limits, avoid revisiting URLs, obey depth constraints, and handle the real-world messiness of HTTP (redirects, timeouts, malformed HTML, relative URLs). This exercise synthesizes almost every concurrency pattern you have learned: worker pools, bounded parallelism, fan-out/fan-in, graceful shutdown, and error handling.

The core difficulty is coordination: a URL discovered by worker A must not be re-crawled by worker B. New URLs are discovered during crawling, so the work set grows dynamically. The crawler must detect when all work is done (no pending URLs, no in-flight fetches) and terminate cleanly.

## Requirements

### Core Crawler

1. Start from a seed URL and discover links by parsing HTML `<a href="...">` tags
2. Crawl only URLs within the same domain as the seed (no cross-domain crawling)
3. Configurable maximum depth from the seed URL
4. Configurable maximum number of pages to crawl
5. Configurable concurrency (number of simultaneous fetches)

### URL Management

6. Normalize URLs before deduplication (remove fragments, normalize scheme/host case, resolve relative URLs)
7. Track visited URLs in a thread-safe set
8. Maintain a work queue of URLs to crawl with their current depth

### HTTP Handling

9. Configurable request timeout per fetch
10. Follow redirects (up to a limit) but record the final URL
11. Parse only `text/html` responses; skip binary content
12. Set a proper `User-Agent` header
13. Respect `robots.txt` (parse and check disallowed paths)

### Concurrency Control

14. A worker pool of N goroutines pulls URLs from the work queue
15. The crawler detects termination: when the work queue is empty and all workers are idle, stop
16. Graceful shutdown via context cancellation: stop accepting new URLs, wait for in-flight fetches to complete

### Output

17. Print a sitemap of discovered URLs with their depth and HTTP status code
18. Report statistics: pages crawled, errors, time elapsed, pages/second

## Hints

<details>
<summary>Hint 1: Termination detection</summary>

The hardest part of a concurrent crawler is knowing when to stop. Use an atomic counter of in-flight work:

```go
var inFlight atomic.Int32

// When dispatching work:
inFlight.Add(1)

// When work completes:
if inFlight.Add(-1) == 0 && len(queue) == 0 {
    // All done -- signal completion
    close(done)
}
```

Alternatively, use `sync.WaitGroup` where you `Add(1)` for each enqueued URL and `Done()` when it is processed.
</details>

<details>
<summary>Hint 2: URL normalization and deduplication</summary>

```go
func normalizeURL(base *url.URL, href string) (string, error) {
    u, err := url.Parse(href)
    if err != nil {
        return "", err
    }
    resolved := base.ResolveReference(u)
    resolved.Fragment = ""                    // remove fragment
    resolved.Host = strings.ToLower(resolved.Host)
    resolved.Path = path.Clean(resolved.Path) // normalize path
    return resolved.String(), nil
}

type URLSet struct {
    mu   sync.Mutex
    seen map[string]struct{}
}

func (s *URLSet) Add(url string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    if _, ok := s.seen[url]; ok {
        return false // already seen
    }
    s.seen[url] = struct{}{}
    return true
}
```
</details>

<details>
<summary>Hint 3: Link extraction with html.Tokenizer</summary>

```go
import "golang.org/x/net/html"

func extractLinks(body io.Reader) []string {
    var links []string
    tokenizer := html.NewTokenizer(body)
    for {
        tt := tokenizer.Next()
        if tt == html.ErrorToken {
            break
        }
        if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
            t := tokenizer.Token()
            if t.Data == "a" {
                for _, attr := range t.Attr {
                    if attr.Key == "href" {
                        links = append(links, attr.Val)
                    }
                }
            }
        }
    }
    return links
}
```

Install with: `go get golang.org/x/net/html`
</details>

<details>
<summary>Hint 4: Worker pool with dynamic work</summary>

```go
func crawl(ctx context.Context, seed string, workers int, maxDepth int) {
    queue := make(chan CrawlTask, 1000)
    var wg sync.WaitGroup
    visited := &URLSet{seen: make(map[string]struct{})}

    // Seed the queue
    visited.Add(seed)
    wg.Add(1)
    queue <- CrawlTask{URL: seed, Depth: 0}

    // Start workers
    for i := 0; i < workers; i++ {
        go func() {
            for task := range queue {
                links := fetch(ctx, task)
                for _, link := range links {
                    if task.Depth+1 <= maxDepth && visited.Add(link) {
                        wg.Add(1)
                        queue <- CrawlTask{URL: link, Depth: task.Depth + 1}
                    }
                }
                wg.Done()
            }
        }()
    }

    // Wait for all work then close queue
    wg.Wait()
    close(queue)
}
```

Note: this simplified version can deadlock if the queue is full and all workers are trying to enqueue. A production version uses a separate dispatcher goroutine or an unbounded internal queue.
</details>

## Success Criteria

- [ ] Crawls all reachable same-domain pages from a seed URL
- [ ] Respects maximum depth and maximum page count limits
- [ ] No URL is crawled twice (deduplication with normalized URLs)
- [ ] Concurrency is bounded to the configured number of workers
- [ ] Relative URLs are correctly resolved against the page's base URL
- [ ] Non-HTML responses are skipped
- [ ] The crawler terminates cleanly when all reachable URLs have been processed
- [ ] Context cancellation stops the crawler gracefully (in-flight fetches complete, no goroutine leaks)
- [ ] A sitemap is printed with URL, depth, and HTTP status
- [ ] Statistics are reported: pages crawled, errors, elapsed time, throughput
- [ ] No data races (`go run -race`)
- [ ] The program works against a real website or a local test server

## Research Resources

- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines) -- pipeline and cancellation patterns
- [The Go Tour: Web Crawler exercise](https://go.dev/tour/concurrency/10) -- introductory crawler exercise
- [golang.org/x/net/html](https://pkg.go.dev/golang.org/x/net/html) -- HTML tokenizer and parser
- [net/url](https://pkg.go.dev/net/url) -- URL parsing and resolution
- [Mercator: A Scalable Web Crawler (paper)](https://courses.cs.washington.edu/courses/cse454/10wi/papers/mercator.pdf) -- production crawler architecture
- [Colly](https://github.com/gocolly/colly) -- popular Go scraping framework (for reference, do not use as a dependency)
