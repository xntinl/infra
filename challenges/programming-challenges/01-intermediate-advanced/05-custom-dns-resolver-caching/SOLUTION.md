# Solution: Custom DNS Resolver with Caching

## Architecture Overview

The resolver has four main components: a **codec** that encodes/decodes DNS wire format messages, a **resolver** that performs iterative resolution by following the delegation chain, a **cache** that stores records with TTL decay, and a **CLI** that presents results.

```
┌──────────┐     ┌──────────┐     ┌─────────────┐
│   CLI    │────>│ Resolver │────>│ Nameservers  │
│          │<────│          │<────│ (UDP :53)    │
└──────────┘     │  ┌──────┐│     └─────────────┘
                 │  │Cache ││
                 │  └──────┘│
                 └──────────┘
```

The resolver starts at a root nameserver and follows NS referrals: root -> TLD (.com) -> authoritative (example.com). At each step, it checks the cache first. Responses are cached per-record with their original TTL, decremented on each cache hit based on elapsed time.

---

## Go Solution

### Project Setup

```bash
mkdir dns-resolver && cd dns-resolver
go mod init dns-resolver
```

### DNS Message Codec

```go
// dns.go
package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
)

const (
	TypeA     uint16 = 1
	TypeAAAA  uint16 = 28
	TypeCNAME uint16 = 5
	TypeMX    uint16 = 15
	TypeNS    uint16 = 2
	ClassIN   uint16 = 1
)

type Header struct {
	ID      uint16
	Flags   uint16
	QDCount uint16
	ANCount uint16
	NSCount uint16
	ARCount uint16
}

type Question struct {
	Name  string
	Type  uint16
	Class uint16
}

type Record struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32
	Data  string
}

type Message struct {
	Header     Header
	Questions  []Question
	Answers    []Record
	Authority  []Record
	Additional []Record
}

func encodeHeader(h Header) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], h.ID)
	binary.BigEndian.PutUint16(buf[2:4], h.Flags)
	binary.BigEndian.PutUint16(buf[4:6], h.QDCount)
	binary.BigEndian.PutUint16(buf[6:8], h.ANCount)
	binary.BigEndian.PutUint16(buf[8:10], h.NSCount)
	binary.BigEndian.PutUint16(buf[10:12], h.ARCount)
	return buf
}

func decodeHeader(data []byte) Header {
	return Header{
		ID:      binary.BigEndian.Uint16(data[0:2]),
		Flags:   binary.BigEndian.Uint16(data[2:4]),
		QDCount: binary.BigEndian.Uint16(data[4:6]),
		ANCount: binary.BigEndian.Uint16(data[6:8]),
		NSCount: binary.BigEndian.Uint16(data[8:10]),
		ARCount: binary.BigEndian.Uint16(data[10:12]),
	}
}

func encodeName(name string) []byte {
	var buf []byte
	parts := strings.Split(name, ".")
	for _, label := range parts {
		if label == "" {
			continue
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00)
	return buf
}

func decodeName(data []byte, offset int) (string, int) {
	var labels []string
	jumped := false
	originalOffset := offset

	for {
		if offset >= len(data) {
			break
		}
		length := int(data[offset])
		if length == 0 {
			if !jumped {
				originalOffset = offset + 1
			}
			break
		}

		if length&0xC0 == 0xC0 {
			if !jumped {
				originalOffset = offset + 2
			}
			ptr := int(binary.BigEndian.Uint16(data[offset:offset+2])) & 0x3FFF
			offset = ptr
			jumped = true
			continue
		}

		offset++
		label := string(data[offset : offset+length])
		labels = append(labels, label)
		offset += length
	}

	if jumped {
		return strings.Join(labels, "."), originalOffset
	}
	return strings.Join(labels, "."), originalOffset
}

func encodeQuestion(q Question) []byte {
	buf := encodeName(q.Name)
	tail := make([]byte, 4)
	binary.BigEndian.PutUint16(tail[0:2], q.Type)
	binary.BigEndian.PutUint16(tail[2:4], q.Class)
	return append(buf, tail...)
}

func BuildQuery(name string, qtype uint16) []byte {
	h := Header{
		ID:      uint16(rand.Intn(65535)),
		Flags:   0x0000, // no recursion desired for iterative
		QDCount: 1,
	}
	q := Question{Name: name, Type: qtype, Class: ClassIN}
	msg := encodeHeader(h)
	msg = append(msg, encodeQuestion(q)...)
	return msg
}

func ParseResponse(data []byte) (*Message, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("response too short: %d bytes", len(data))
	}

	msg := &Message{Header: decodeHeader(data)}
	offset := 12

	for i := 0; i < int(msg.Header.QDCount); i++ {
		name, newOff := decodeName(data, offset)
		offset = newOff
		if offset+4 > len(data) {
			return nil, fmt.Errorf("truncated question section")
		}
		qtype := binary.BigEndian.Uint16(data[offset : offset+2])
		qclass := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		offset += 4
		msg.Questions = append(msg.Questions, Question{Name: name, Type: qtype, Class: qclass})
	}

	parseRecords := func(count int) ([]Record, int, error) {
		var records []Record
		off := offset
		for i := 0; i < count; i++ {
			name, newOff := decodeName(data, off)
			off = newOff
			if off+10 > len(data) {
				return nil, off, fmt.Errorf("truncated record at offset %d", off)
			}
			rtype := binary.BigEndian.Uint16(data[off : off+2])
			rclass := binary.BigEndian.Uint16(data[off+2 : off+4])
			ttl := binary.BigEndian.Uint32(data[off+4 : off+8])
			rdlength := binary.BigEndian.Uint16(data[off+8 : off+10])
			off += 10

			if off+int(rdlength) > len(data) {
				return nil, off, fmt.Errorf("truncated rdata")
			}

			rdata := parseRData(data, off, rtype, rdlength)
			off += int(rdlength)

			records = append(records, Record{
				Name:  name,
				Type:  rtype,
				Class: rclass,
				TTL:   ttl,
				Data:  rdata,
			})
		}
		return records, off, nil
	}

	var err error
	msg.Answers, offset, err = parseRecords(int(msg.Header.ANCount))
	if err != nil {
		return nil, err
	}
	msg.Authority, offset, err = parseRecords(int(msg.Header.NSCount))
	if err != nil {
		return nil, err
	}
	msg.Additional, _, err = parseRecords(int(msg.Header.ARCount))
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func parseRData(data []byte, offset int, rtype uint16, rdlength uint16) string {
	switch rtype {
	case TypeA:
		if rdlength == 4 {
			return fmt.Sprintf("%d.%d.%d.%d",
				data[offset], data[offset+1], data[offset+2], data[offset+3])
		}
	case TypeAAAA:
		if rdlength == 16 {
			var parts []string
			for i := 0; i < 16; i += 2 {
				parts = append(parts, fmt.Sprintf("%x",
					binary.BigEndian.Uint16(data[offset+i:offset+i+2])))
			}
			return strings.Join(parts, ":")
		}
	case TypeCNAME, TypeNS:
		name, _ := decodeName(data, offset)
		return name
	case TypeMX:
		priority := binary.BigEndian.Uint16(data[offset : offset+2])
		name, _ := decodeName(data, offset+2)
		return fmt.Sprintf("%d %s", priority, name)
	}
	return fmt.Sprintf("<rdata %d bytes>", rdlength)
}

func TypeToString(t uint16) string {
	switch t {
	case TypeA:
		return "A"
	case TypeAAAA:
		return "AAAA"
	case TypeCNAME:
		return "CNAME"
	case TypeMX:
		return "MX"
	case TypeNS:
		return "NS"
	default:
		return fmt.Sprintf("TYPE%d", t)
	}
}

func StringToType(s string) uint16 {
	switch strings.ToUpper(s) {
	case "A":
		return TypeA
	case "AAAA":
		return TypeAAAA
	case "CNAME":
		return TypeCNAME
	case "MX":
		return TypeMX
	default:
		return TypeA
	}
}
```

### Cache

```go
// cache.go
package main

import (
	"sync"
	"time"
)

type CacheKey struct {
	Name string
	Type uint16
}

type CacheEntry struct {
	Records  []Record
	CachedAt time.Time
	OrigTTL  time.Duration
}

func (e *CacheEntry) RemainingTTL() time.Duration {
	remaining := e.OrigTTL - time.Since(e.CachedAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

type DNSCache struct {
	mu      sync.RWMutex
	entries map[CacheKey]*CacheEntry
}

func NewDNSCache() *DNSCache {
	return &DNSCache{
		entries: make(map[CacheKey]*CacheEntry),
	}
}

func (c *DNSCache) Get(name string, qtype uint16) ([]Record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := CacheKey{Name: name, Type: qtype}
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	remaining := entry.RemainingTTL()
	if remaining <= 0 {
		return nil, false
	}

	records := make([]Record, len(entry.Records))
	copy(records, entry.Records)
	for i := range records {
		records[i].TTL = uint32(remaining.Seconds())
	}
	return records, true
}

func (c *DNSCache) Put(name string, qtype uint16, records []Record) {
	if len(records) == 0 {
		return
	}

	minTTL := records[0].TTL
	for _, r := range records[1:] {
		if r.TTL < minTTL {
			minTTL = r.TTL
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[CacheKey{Name: name, Type: qtype}] = &CacheEntry{
		Records:  records,
		CachedAt: time.Now(),
		OrigTTL:  time.Duration(minTTL) * time.Second,
	}
}

func (c *DNSCache) Evict() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for key, entry := range c.entries {
		if entry.RemainingTTL() <= 0 {
			delete(c.entries, key)
			count++
		}
	}
	return count
}
```

### Resolver

```go
// resolver.go
package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

var rootServers = []string{
	"198.41.0.4",   // a.root-servers.net
	"199.9.14.201", // b.root-servers.net
	"192.33.4.12",  // c.root-servers.net
	"199.7.91.13",  // d.root-servers.net
}

type ResolveResult struct {
	Records    []Record
	CNAMEChain []string
	Server     string
	FromCache  bool
}

type Resolver struct {
	cache      *DNSCache
	timeout    time.Duration
	maxRetries int
}

func NewResolver() *Resolver {
	return &Resolver{
		cache:      NewDNSCache(),
		timeout:    2 * time.Second,
		maxRetries: 3,
	}
}

func (r *Resolver) Resolve(ctx context.Context, name string, qtype uint16) (*ResolveResult, error) {
	if records, ok := r.cache.Get(name, qtype); ok {
		return &ResolveResult{
			Records:   records,
			FromCache: true,
		}, nil
	}

	result, err := r.resolveIterative(ctx, name, qtype)
	if err != nil {
		return nil, err
	}

	if len(result.Records) > 0 {
		r.cache.Put(name, qtype, result.Records)
	}
	return result, nil
}

func (r *Resolver) resolveIterative(ctx context.Context, name string, qtype uint16) (*ResolveResult, error) {
	nameservers := rootServers
	result := &ResolveResult{}

	for depth := 0; depth < 20; depth++ {
		if records, ok := r.cache.Get(name, qtype); ok {
			result.Records = records
			result.FromCache = true
			return result, nil
		}

		var lastErr error
		var msg *Message

		for _, ns := range nameservers {
			var err error
			msg, err = r.queryWithRetry(ctx, ns, name, qtype)
			if err != nil {
				lastErr = err
				continue
			}
			result.Server = ns
			break
		}

		if msg == nil {
			if lastErr != nil {
				return nil, fmt.Errorf("all nameservers failed: %w", lastErr)
			}
			return nil, fmt.Errorf("no response from any nameserver")
		}

		if len(msg.Answers) > 0 {
			for _, ans := range msg.Answers {
				if ans.Type == TypeCNAME && qtype != TypeCNAME {
					result.CNAMEChain = append(result.CNAMEChain, ans.Data)
					name = ans.Data
					nameservers = rootServers
					continue
				}
				result.Records = append(result.Records, ans)
			}
			if len(result.Records) > 0 {
				return result, nil
			}
			continue
		}

		if len(msg.Authority) > 0 {
			nextNS := extractNextNameservers(msg)
			if len(nextNS) > 0 {
				nameservers = nextNS
				for _, auth := range msg.Authority {
					if auth.Type == TypeNS {
						r.cache.Put(auth.Name, TypeNS, []Record{auth})
					}
				}
				continue
			}
		}

		return result, nil
	}

	return nil, fmt.Errorf("max delegation depth exceeded for %s", name)
}

func extractNextNameservers(msg *Message) []string {
	nsNames := map[string]bool{}
	for _, auth := range msg.Authority {
		if auth.Type == TypeNS {
			nsNames[auth.Data] = true
		}
	}

	var ips []string
	for _, add := range msg.Additional {
		if add.Type == TypeA && nsNames[add.Name] {
			ips = append(ips, add.Data)
		}
	}
	return ips
}

func (r *Resolver) queryWithRetry(ctx context.Context, server, name string, qtype uint16) (*Message, error) {
	var lastErr error
	for attempt := 0; attempt < r.maxRetries; attempt++ {
		msg, err := r.sendQuery(ctx, server, name, qtype)
		if err == nil {
			return msg, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("query to %s failed after %d retries: %w", server, r.maxRetries, lastErr)
}

func (r *Resolver) sendQuery(ctx context.Context, server, name string, qtype uint16) (*Message, error) {
	query := BuildQuery(name, qtype)

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(r.timeout)
	}

	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP(server),
		Port: 53,
	})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", server, err)
	}
	defer conn.Close()

	conn.SetDeadline(deadline)

	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("write to %s: %w", server, err)
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read from %s: %w", server, err)
	}

	return ParseResponse(buf[:n])
}
```

### CLI Entry Point

```go
// main.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: resolve <domain> [A|AAAA|CNAME|MX]\n")
		os.Exit(1)
	}

	domain := os.Args[1]
	qtype := TypeA
	if len(os.Args) >= 3 {
		qtype = StringToType(os.Args[2])
	}

	resolver := NewResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	result, err := resolver.Resolve(ctx, domain, qtype)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(";; %s query for %s\n", TypeToString(qtype), domain)
	if result.FromCache {
		fmt.Println(";; (served from cache)")
	}
	fmt.Printf(";; Responded: %s\n", result.Server)
	fmt.Printf(";; Time: %v\n\n", elapsed)

	if len(result.CNAMEChain) > 0 {
		fmt.Println(";; CNAME chain:")
		prev := domain
		for _, cn := range result.CNAMEChain {
			fmt.Printf(";;   %s -> %s\n", prev, cn)
			prev = cn
		}
		fmt.Println()
	}

	fmt.Println(";; ANSWER SECTION:")
	for _, r := range result.Records {
		fmt.Printf("%-30s %d\tIN\t%s\t%s\n", r.Name, r.TTL, TypeToString(r.Type), r.Data)
	}
}
```

### Tests

```go
// dns_test.go
package main

import (
	"context"
	"testing"
	"time"
)

func TestEncodeName(t *testing.T) {
	encoded := encodeName("example.com")
	expected := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	if len(encoded) != len(expected) {
		t.Fatalf("expected %d bytes, got %d", len(expected), len(encoded))
	}
	for i := range expected {
		if encoded[i] != expected[i] {
			t.Fatalf("byte %d: expected %d, got %d", i, expected[i], encoded[i])
		}
	}
}

func TestDecodeName(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	name, offset := decodeName(data, 0)
	if name != "example.com" {
		t.Fatalf("expected example.com, got %s", name)
	}
	if offset != 13 {
		t.Fatalf("expected offset 13, got %d", offset)
	}
}

func TestDecodeNameWithPointer(t *testing.T) {
	data := []byte{
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, // offset 0-12
		3, 'w', 'w', 'w', 0xC0, 0x00, // offset 13-18: www + pointer to offset 0
	}
	name, _ := decodeName(data, 13)
	if name != "www.example.com" {
		t.Fatalf("expected www.example.com, got %s", name)
	}
}

func TestBuildQueryFormat(t *testing.T) {
	query := BuildQuery("example.com", TypeA)
	if len(query) < 12 {
		t.Fatal("query too short for header")
	}
	if query[4] != 0 || query[5] != 1 {
		t.Fatal("QDCount should be 1")
	}
}

func TestCachePutGet(t *testing.T) {
	cache := NewDNSCache()
	records := []Record{
		{Name: "example.com", Type: TypeA, TTL: 300, Data: "93.184.216.34"},
	}
	cache.Put("example.com", TypeA, records)

	got, ok := cache.Get("example.com", TypeA)
	if !ok || len(got) != 1 {
		t.Fatal("expected cache hit")
	}
	if got[0].Data != "93.184.216.34" {
		t.Fatalf("expected 93.184.216.34, got %s", got[0].Data)
	}
}

func TestCacheTTLDecay(t *testing.T) {
	cache := NewDNSCache()
	records := []Record{
		{Name: "test.com", Type: TypeA, TTL: 1, Data: "1.2.3.4"},
	}
	cache.Put("test.com", TypeA, records)

	time.Sleep(1100 * time.Millisecond)

	_, ok := cache.Get("test.com", TypeA)
	if ok {
		t.Fatal("expected cache miss after TTL expired")
	}
}

func TestCacheEvict(t *testing.T) {
	cache := NewDNSCache()
	records := []Record{
		{Name: "old.com", Type: TypeA, TTL: 1, Data: "1.1.1.1"},
	}
	cache.Put("old.com", TypeA, records)

	time.Sleep(1100 * time.Millisecond)

	evicted := cache.Evict()
	if evicted != 1 {
		t.Fatalf("expected 1 eviction, got %d", evicted)
	}
}

func TestResolverEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	resolver := NewResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := resolver.Resolve(ctx, "example.com", TypeA)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(result.Records) == 0 {
		t.Fatal("expected at least one record")
	}

	found := false
	for _, r := range result.Records {
		if r.Type == TypeA {
			found = true
			t.Logf("Resolved: %s -> %s (TTL %d)", r.Name, r.Data, r.TTL)
		}
	}
	if !found {
		t.Fatal("no A records in response")
	}

	// second query should hit cache
	result2, err := resolver.Resolve(ctx, "example.com", TypeA)
	if err != nil {
		t.Fatalf("cached resolve failed: %v", err)
	}
	if !result2.FromCache {
		t.Fatal("expected second query to be served from cache")
	}
}
```

### Running

```bash
# Run tests (skip network tests with -short)
go test -v -short ./...

# Run with network access
go test -v -run TestResolverEndToEnd ./...

# Use the CLI
go run . example.com A
go run . google.com MX
```

### Expected Output (CLI)

```
;; A query for example.com
;; Responded: 199.7.91.13
;; Time: 245ms

;; ANSWER SECTION:
example.com                    86400	IN	A	93.184.216.34
```

---

## Design Decisions

**Why iterative resolution instead of forwarding?** A forwarding resolver sends queries to a recursive resolver (like 8.8.8.8) and returns whatever it gets. An iterative resolver follows the delegation chain itself: root -> TLD -> authoritative. This is more educational and gives full control over caching at every level of the hierarchy.

**Why `RWMutex` for the cache but `Mutex` for the LRU challenge?** The DNS cache's `Get` is a true read (no reordering). Multiple goroutines can read concurrently without conflict. The LRU cache's `Get` modifies the linked list, making it a write operation. Different access patterns demand different locking strategies.

**Why 512-byte UDP buffer?** RFC 1035 specifies a maximum UDP DNS message size of 512 bytes. Responses larger than this set the TC (truncation) flag, indicating the client should retry over TCP. This implementation handles the common case; the Going Further section covers TCP fallback.

**Why walk from tail for expired entries?** Entries near the tail have not been accessed recently and are more likely to be expired. Starting from the tail makes the common case (most expired entries clustered at the tail) faster.

## Common Mistakes

**Not handling name compression.** DNS responses compress repeated domain names using pointers (two bytes with the top two bits set to 1). If you treat pointer bytes as label lengths, parsing silently produces garbage data. Always check the top two bits before interpreting a byte as a label length.

**Off-by-one in offset tracking.** When parsing records, the offset must advance by exactly the right amount for each field. A single off-by-one corrupts all subsequent records. Track offsets meticulously and validate bounds before every read.

**Ignoring the Additional section.** NS records in the Authority section contain nameserver domain names, not IP addresses. The matching IP addresses are in the Additional section (glue records). Without parsing Additional, you would need a separate resolution for each nameserver name, creating a chicken-and-egg problem.

**Using recursion desired flag with root servers.** Root servers do not perform recursion. If you set the RD flag, some servers ignore it, but others may return REFUSED. For iterative resolution, always send queries with RD=0.

## Performance Notes

- Each UDP round-trip to a nameserver adds 20-100ms of latency. Caching eliminates repeated trips for the same domain
- The cache uses `RWMutex`, allowing concurrent reads without contention. Write operations (cache inserts) are infrequent compared to reads
- For high-throughput resolvers, consider sharding the cache by domain name to reduce lock contention
- The 512-byte UDP limit means EDNS0 (RFC 6891) is needed for larger responses. This implementation does not support EDNS0

## Going Further

- Add TCP fallback: when a UDP response has the TC (truncation) flag set, retry the query over TCP with a 2-byte length prefix
- Implement EDNS0 (RFC 6891) to support UDP messages up to 4096 bytes, eliminating most TCP fallbacks
- Add DNSSEC validation: verify RRSIG records against DNSKEY records to authenticate responses
- Build a full DNS server that listens on port 53 and serves cached responses to local clients
- Implement negative caching: cache NXDOMAIN responses per RFC 2308 to avoid repeated queries for nonexistent domains
- Add DNS-over-HTTPS (DoH) or DNS-over-TLS (DoT) as transport options for privacy
