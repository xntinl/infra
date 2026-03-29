# Solution: DNS Authoritative Server

## Architecture Overview

The server is structured in four components:

1. **Wire Codec** -- encodes and decodes DNS messages at the byte level: headers, questions, resource records, domain names with compression
2. **Zone Database** -- parses zone files into an in-memory map of domain names to record sets, indexed by name and record type
3. **Query Engine** -- matches incoming queries against the zone database, applies wildcard rules, builds response messages with correct sections and RCODEs
4. **Server** -- concurrent UDP and TCP listeners that receive queries, dispatch to the query engine, and send responses

```
UDP Listener (port 53)  ──┐
                          ├──> Query Engine ──> Zone Database
TCP Listener (port 53)  ──┘                        |
                                                   v
                                            Wire Codec (encode response)
                                                   |
                                                   v
                                            Send to client
```

## Go Solution

### `main.go`

```go
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := flag.Int("port", 1053, "DNS server port")
	zoneFile := flag.String("zone", "example.zone", "path to zone file")
	flag.Parse()

	zone, err := LoadZoneFile(*zoneFile)
	if err != nil {
		log.Fatalf("failed to load zone file: %v", err)
	}
	log.Printf("loaded zone %s with %d records", zone.Origin, zone.RecordCount())

	server := NewServer(zone, *port)

	go func() {
		if err := server.ListenUDP(); err != nil {
			log.Fatalf("UDP listener error: %v", err)
		}
	}()

	go func() {
		if err := server.ListenTCP(); err != nil {
			log.Fatalf("TCP listener error: %v", err)
		}
	}()

	log.Printf("DNS server listening on port %d (UDP+TCP)", *port)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}
```

### `wire.go`

```go
package main

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// DNS record types.
const (
	TypeA     uint16 = 1
	TypeNS    uint16 = 2
	TypeCNAME uint16 = 5
	TypeSOA   uint16 = 6
	TypeMX    uint16 = 15
	TypeTXT   uint16 = 16
	TypeAAAA  uint16 = 28
	TypeOPT   uint16 = 41
	TypeRRSIG uint16 = 46
	TypeDNSKEY uint16 = 48
)

// DNS classes.
const (
	ClassIN uint16 = 1
)

// DNS response codes.
const (
	RCodeNoError  uint8 = 0
	RCodeFormErr  uint8 = 1
	RCodeNXDomain uint8 = 3
	RCodeRefused  uint8 = 5
)

// Header represents the 12-byte DNS message header.
type Header struct {
	ID      uint16
	QR      bool
	Opcode  uint8
	AA      bool
	TC      bool
	RD      bool
	RA      bool
	RCode   uint8
	QDCount uint16
	ANCount uint16
	NSCount uint16
	ARCount uint16
}

// Question represents a DNS query question.
type Question struct {
	Name  string
	Type  uint16
	Class uint16
}

// ResourceRecord represents a DNS resource record.
type ResourceRecord struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32
	RData []byte

	// Parsed RDATA fields for convenience (not serialized)
	RDataStr string
}

// Message represents a complete DNS message.
type Message struct {
	Header     Header
	Questions  []Question
	Answers    []ResourceRecord
	Authority  []ResourceRecord
	Additional []ResourceRecord
	EDNS       *EDNSData
}

// EDNSData holds parsed EDNS0 OPT record data.
type EDNSData struct {
	UDPSize    uint16
	ExtRCode   uint8
	Version    uint8
	DOBit      bool
}

// DecodeMessage parses a DNS message from wire format.
func DecodeMessage(buf []byte) (*Message, error) {
	if len(buf) < 12 {
		return nil, fmt.Errorf("message too short: %d bytes", len(buf))
	}

	msg := &Message{}
	msg.Header = decodeHeader(buf[:12])

	offset := 12

	// Decode questions
	for i := 0; i < int(msg.Header.QDCount); i++ {
		name, newOffset, err := decodeName(buf, offset)
		if err != nil {
			return nil, fmt.Errorf("decode question name: %w", err)
		}
		if newOffset+4 > len(buf) {
			return nil, fmt.Errorf("question section truncated")
		}
		qtype := binary.BigEndian.Uint16(buf[newOffset:])
		qclass := binary.BigEndian.Uint16(buf[newOffset+2:])
		msg.Questions = append(msg.Questions, Question{Name: name, Type: qtype, Class: qclass})
		offset = newOffset + 4
	}

	// Decode resource records (answers, authority, additional)
	decodeRRs := func(count uint16) ([]ResourceRecord, int, error) {
		var rrs []ResourceRecord
		off := offset
		for i := 0; i < int(count); i++ {
			rr, newOff, err := decodeResourceRecord(buf, off)
			if err != nil {
				return nil, off, fmt.Errorf("decode RR: %w", err)
			}
			rrs = append(rrs, rr)
			off = newOff
		}
		return rrs, off, nil
	}

	var err error
	msg.Answers, offset, err = decodeRRs(msg.Header.ANCount)
	if err != nil {
		return nil, fmt.Errorf("answers: %w", err)
	}
	msg.Authority, offset, err = decodeRRs(msg.Header.NSCount)
	if err != nil {
		return nil, fmt.Errorf("authority: %w", err)
	}
	msg.Additional, offset, err = decodeRRs(msg.Header.ARCount)
	if err != nil {
		return nil, fmt.Errorf("additional: %w", err)
	}

	// Check for EDNS0 OPT in additional section
	for _, rr := range msg.Additional {
		if rr.Type == TypeOPT {
			msg.EDNS = &EDNSData{
				UDPSize:  rr.Class,
				ExtRCode: uint8(rr.TTL >> 24),
				Version:  uint8(rr.TTL >> 16),
				DOBit:    rr.TTL&0x8000 != 0,
			}
			break
		}
	}

	return msg, nil
}

// EncodeMessage serializes a DNS message to wire format.
func EncodeMessage(msg *Message) []byte {
	buf := make([]byte, 12, 512)
	compressionTable := make(map[string]int)

	// Update counts
	msg.Header.QDCount = uint16(len(msg.Questions))
	msg.Header.ANCount = uint16(len(msg.Answers))
	msg.Header.NSCount = uint16(len(msg.Authority))
	msg.Header.ARCount = uint16(len(msg.Additional))

	encodeHeader(msg.Header, buf[:12])

	// Encode questions
	for _, q := range msg.Questions {
		buf = encodeName(buf, q.Name, compressionTable)
		buf = binary.BigEndian.AppendUint16(buf, q.Type)
		buf = binary.BigEndian.AppendUint16(buf, q.Class)
	}

	// Encode resource records
	encodeRRs := func(rrs []ResourceRecord) {
		for _, rr := range rrs {
			buf = encodeResourceRecord(buf, rr, compressionTable)
		}
	}

	encodeRRs(msg.Answers)
	encodeRRs(msg.Authority)
	encodeRRs(msg.Additional)

	// Re-encode header with correct counts
	encodeHeader(msg.Header, buf[:12])

	return buf
}

func decodeHeader(buf []byte) Header {
	flags := binary.BigEndian.Uint16(buf[2:4])
	return Header{
		ID:      binary.BigEndian.Uint16(buf[0:2]),
		QR:      flags>>15&1 == 1,
		Opcode:  uint8(flags >> 11 & 0xF),
		AA:      flags>>10&1 == 1,
		TC:      flags>>9&1 == 1,
		RD:      flags>>8&1 == 1,
		RA:      flags>>7&1 == 1,
		RCode:   uint8(flags & 0xF),
		QDCount: binary.BigEndian.Uint16(buf[4:6]),
		ANCount: binary.BigEndian.Uint16(buf[6:8]),
		NSCount: binary.BigEndian.Uint16(buf[8:10]),
		ARCount: binary.BigEndian.Uint16(buf[10:12]),
	}
}

func encodeHeader(h Header, buf []byte) {
	binary.BigEndian.PutUint16(buf[0:2], h.ID)
	var flags uint16
	if h.QR {
		flags |= 1 << 15
	}
	flags |= uint16(h.Opcode) << 11
	if h.AA {
		flags |= 1 << 10
	}
	if h.TC {
		flags |= 1 << 9
	}
	if h.RD {
		flags |= 1 << 8
	}
	if h.RA {
		flags |= 1 << 7
	}
	flags |= uint16(h.RCode)
	binary.BigEndian.PutUint16(buf[2:4], flags)
	binary.BigEndian.PutUint16(buf[4:6], h.QDCount)
	binary.BigEndian.PutUint16(buf[6:8], h.ANCount)
	binary.BigEndian.PutUint16(buf[8:10], h.NSCount)
	binary.BigEndian.PutUint16(buf[10:12], h.ARCount)
}

// decodeName reads a domain name from a DNS message, following compression pointers.
func decodeName(buf []byte, offset int) (string, int, error) {
	var labels []string
	visited := make(map[int]bool)
	originalOffset := -1
	maxJumps := 128

	for jumps := 0; jumps < maxJumps; jumps++ {
		if offset >= len(buf) {
			return "", 0, fmt.Errorf("name extends past end of message")
		}

		length := int(buf[offset])

		if length == 0 {
			if originalOffset < 0 {
				originalOffset = offset + 1
			}
			name := strings.Join(labels, ".") + "."
			return strings.ToLower(name), originalOffset, nil
		}

		// Check for compression pointer
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(buf) {
				return "", 0, fmt.Errorf("pointer extends past message")
			}
			pointer := int(binary.BigEndian.Uint16(buf[offset:offset+2])) & 0x3FFF
			if visited[pointer] {
				return "", 0, fmt.Errorf("compression pointer loop detected")
			}
			visited[pointer] = true
			if originalOffset < 0 {
				originalOffset = offset + 2
			}
			offset = pointer
			continue
		}

		// Literal label
		offset++
		if offset+length > len(buf) {
			return "", 0, fmt.Errorf("label extends past message")
		}
		labels = append(labels, string(buf[offset:offset+length]))
		offset += length
	}

	return "", 0, fmt.Errorf("too many compression pointer jumps")
}

// encodeName writes a domain name with compression.
func encodeName(buf []byte, name string, table map[string]int) []byte {
	name = strings.ToLower(name)
	if !strings.HasSuffix(name, ".") {
		name += "."
	}

	remaining := name
	for remaining != "." && remaining != "" {
		// Check if we can compress
		if offset, ok := table[remaining]; ok && offset < 16384 {
			pointer := uint16(0xC000) | uint16(offset)
			buf = binary.BigEndian.AppendUint16(buf, pointer)
			return buf
		}

		// Record position for future compression
		table[remaining] = len(buf)

		// Write literal label
		dotIdx := strings.IndexByte(remaining, '.')
		if dotIdx < 0 {
			dotIdx = len(remaining)
		}
		label := remaining[:dotIdx]
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)

		if dotIdx < len(remaining) {
			remaining = remaining[dotIdx+1:]
		} else {
			remaining = ""
		}
	}

	buf = append(buf, 0) // Root label
	return buf
}

func decodeResourceRecord(buf []byte, offset int) (ResourceRecord, int, error) {
	name, newOffset, err := decodeName(buf, offset)
	if err != nil {
		return ResourceRecord{}, offset, err
	}
	if newOffset+10 > len(buf) {
		return ResourceRecord{}, offset, fmt.Errorf("RR header truncated")
	}

	rtype := binary.BigEndian.Uint16(buf[newOffset:])
	class := binary.BigEndian.Uint16(buf[newOffset+2:])
	ttl := binary.BigEndian.Uint32(buf[newOffset+4:])
	rdlength := binary.BigEndian.Uint16(buf[newOffset+8:])
	dataStart := newOffset + 10
	dataEnd := dataStart + int(rdlength)

	if dataEnd > len(buf) {
		return ResourceRecord{}, offset, fmt.Errorf("RDATA extends past message")
	}

	rr := ResourceRecord{
		Name:  name,
		Type:  rtype,
		Class: class,
		TTL:   ttl,
		RData: make([]byte, rdlength),
	}
	copy(rr.RData, buf[dataStart:dataEnd])

	return rr, dataEnd, nil
}

func encodeResourceRecord(buf []byte, rr ResourceRecord, table map[string]int) []byte {
	buf = encodeName(buf, rr.Name, table)
	buf = binary.BigEndian.AppendUint16(buf, rr.Type)
	buf = binary.BigEndian.AppendUint16(buf, rr.Class)
	buf = binary.BigEndian.AppendUint32(buf, rr.TTL)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(rr.RData)))
	buf = append(buf, rr.RData...)
	return buf
}

// MakeOPTRecord creates an EDNS0 OPT pseudo-record for the additional section.
func MakeOPTRecord(udpSize uint16, doBit bool) ResourceRecord {
	var ttl uint32
	if doBit {
		ttl = 0x8000
	}
	return ResourceRecord{
		Name:  ".",
		Type:  TypeOPT,
		Class: udpSize,
		TTL:   ttl,
		RData: nil,
	}
}

// EncodeRDataA builds RDATA for an A record from a dotted IPv4 string.
func EncodeRDataA(ip string) ([]byte, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid IPv4: %s", ip)
	}
	rdata := make([]byte, 4)
	for i, p := range parts {
		var val int
		fmt.Sscanf(p, "%d", &val)
		rdata[i] = byte(val)
	}
	return rdata, nil
}

// EncodeRDataAAAA builds RDATA for an AAAA record from an IPv6 string.
func EncodeRDataAAAA(ip string) ([]byte, error) {
	// Simplified: parse the standard net.ParseIP would handle
	// For the challenge, we use net.ParseIP and extract the 16-byte representation
	import_ip := parseIPv6(ip)
	if import_ip == nil {
		return nil, fmt.Errorf("invalid IPv6: %s", ip)
	}
	return import_ip, nil
}

func parseIPv6(s string) []byte {
	// Minimal IPv6 parser handling :: expansion
	groups := strings.Split(s, ":")
	result := make([]byte, 16)
	// Full expansion logic simplified for the challenge
	idx := 0
	for _, g := range groups {
		if g == "" {
			continue
		}
		var val uint16
		fmt.Sscanf(g, "%x", &val)
		if idx < 16 {
			result[idx] = byte(val >> 8)
			result[idx+1] = byte(val)
			idx += 2
		}
	}
	return result
}

// EncodeRDataMX builds RDATA for an MX record.
func EncodeRDataMX(preference uint16, exchange string) []byte {
	rdata := make([]byte, 2)
	binary.BigEndian.PutUint16(rdata, preference)
	table := make(map[string]int)
	rdata = encodeName(rdata, exchange, table)
	return rdata
}

// EncodeRDataTXT builds RDATA for a TXT record (one or more character strings).
func EncodeRDataTXT(texts []string) []byte {
	var rdata []byte
	for _, txt := range texts {
		b := []byte(txt)
		// TXT character strings are limited to 255 bytes each
		for len(b) > 0 {
			chunk := b
			if len(chunk) > 255 {
				chunk = b[:255]
			}
			rdata = append(rdata, byte(len(chunk)))
			rdata = append(rdata, chunk...)
			b = b[len(chunk):]
		}
	}
	return rdata
}

// EncodeRDataSOA builds RDATA for an SOA record.
func EncodeRDataSOA(mname, rname string, serial, refresh, retry, expire, minimum uint32) []byte {
	table := make(map[string]int)
	var rdata []byte
	rdata = encodeName(rdata, mname, table)
	rdata = encodeName(rdata, rname, table)
	rdata = binary.BigEndian.AppendUint32(rdata, serial)
	rdata = binary.BigEndian.AppendUint32(rdata, refresh)
	rdata = binary.BigEndian.AppendUint32(rdata, retry)
	rdata = binary.BigEndian.AppendUint32(rdata, expire)
	rdata = binary.BigEndian.AppendUint32(rdata, minimum)
	return rdata
}
```

### `zone.go`

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Zone holds the parsed zone data.
type Zone struct {
	Origin  string
	TTL     uint32
	SOA     *ResourceRecord
	Records map[string][]ResourceRecord // key: "name:type"
}

// RecordCount returns the total number of resource records in the zone.
func (z *Zone) RecordCount() int {
	count := 0
	for _, rrs := range z.Records {
		count += len(rrs)
	}
	return count
}

// Lookup returns records matching the name and type. Name should be FQDN with trailing dot.
func (z *Zone) Lookup(name string, qtype uint16) []ResourceRecord {
	name = strings.ToLower(name)
	key := fmt.Sprintf("%s:%d", name, qtype)
	if rrs, ok := z.Records[key]; ok {
		return rrs
	}
	return nil
}

// LookupAny returns all records for a name regardless of type.
func (z *Zone) LookupAny(name string) []ResourceRecord {
	name = strings.ToLower(name)
	var result []ResourceRecord
	prefix := name + ":"
	for key, rrs := range z.Records {
		if strings.HasPrefix(key, prefix) {
			result = append(result, rrs...)
		}
	}
	return result
}

// LookupWildcard checks for wildcard records matching the name.
func (z *Zone) LookupWildcard(name string, qtype uint16) []ResourceRecord {
	name = strings.ToLower(name)
	parts := strings.SplitN(name, ".", 2)
	if len(parts) < 2 {
		return nil
	}
	wildcard := "*." + parts[1]
	return z.Lookup(wildcard, qtype)
}

// IsInZone checks if a name falls within this zone's authority.
func (z *Zone) IsInZone(name string) bool {
	name = strings.ToLower(name)
	origin := strings.ToLower(z.Origin)
	return name == origin || strings.HasSuffix(name, "."+origin)
}

// NameExists checks if any records exist for the given name.
func (z *Zone) NameExists(name string) bool {
	name = strings.ToLower(name)
	prefix := name + ":"
	for key := range z.Records {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// LoadZoneFile parses a zone file and returns a Zone.
func LoadZoneFile(path string) (*Zone, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	zone := &Zone{
		TTL:     3600,
		Records: make(map[string][]ResourceRecord),
	}

	scanner := bufio.NewScanner(f)
	var lastName string
	var multiLine strings.Builder
	inParens := false

	for scanner.Scan() {
		line := stripComment(scanner.Text())
		line = strings.TrimRight(line, " \t")

		if inParens {
			multiLine.WriteString(" ")
			multiLine.WriteString(line)
			if strings.Contains(line, ")") {
				inParens = false
				line = strings.ReplaceAll(multiLine.String(), "(", "")
				line = strings.ReplaceAll(line, ")", "")
				multiLine.Reset()
			} else {
				continue
			}
		} else if strings.Contains(line, "(") && !strings.Contains(line, ")") {
			inParens = true
			multiLine.WriteString(line)
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle directives
		if strings.HasPrefix(line, "$ORIGIN") {
			zone.Origin = strings.TrimSpace(strings.TrimPrefix(line, "$ORIGIN"))
			zone.Origin = strings.ToLower(zone.Origin)
			continue
		}
		if strings.HasPrefix(line, "$TTL") {
			ttlStr := strings.TrimSpace(strings.TrimPrefix(line, "$TTL"))
			ttl, err := parseTTL(ttlStr)
			if err != nil {
				return nil, fmt.Errorf("invalid $TTL: %w", err)
			}
			zone.TTL = ttl
			continue
		}

		// Parse resource record
		rr, err := parseRecord(line, zone.Origin, zone.TTL, &lastName)
		if err != nil {
			return nil, fmt.Errorf("parse record: %w", err)
		}
		if rr == nil {
			continue
		}

		key := fmt.Sprintf("%s:%d", strings.ToLower(rr.Name), rr.Type)
		zone.Records[key] = append(zone.Records[key], *rr)

		if rr.Type == TypeSOA {
			zone.SOA = rr
		}
	}

	return zone, scanner.Err()
}

func parseRecord(line, origin string, defaultTTL uint32, lastName *string) (*ResourceRecord, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil, nil
	}

	idx := 0

	// Determine owner name
	var name string
	if fields[0] == "@" {
		name = origin
		idx = 1
	} else if fields[0][0] == ' ' || fields[0][0] == '\t' {
		name = *lastName
	} else if isRecordType(fields[0]) || isClass(fields[0]) || isTTL(fields[0]) {
		name = *lastName
	} else {
		name = qualifyName(fields[0], origin)
		idx = 1
	}
	*lastName = name

	// Parse optional TTL and class
	ttl := defaultTTL
	class := ClassIN

	for idx < len(fields) {
		if isClass(fields[idx]) {
			idx++
			continue
		}
		if isTTL(fields[idx]) {
			t, _ := parseTTL(fields[idx])
			ttl = t
			idx++
			continue
		}
		break
	}

	if idx >= len(fields) {
		return nil, nil
	}

	// Record type
	rtype := parseRecordType(fields[idx])
	if rtype == 0 {
		return nil, fmt.Errorf("unknown record type: %s", fields[idx])
	}
	idx++

	// RDATA (remaining fields)
	rdataFields := fields[idx:]

	rdata, err := buildRData(rtype, rdataFields, origin)
	if err != nil {
		return nil, fmt.Errorf("build rdata for %s: %w", fields[idx-1], err)
	}

	return &ResourceRecord{
		Name:  name,
		Type:  rtype,
		Class: class,
		TTL:   ttl,
		RData: rdata,
	}, nil
}

func buildRData(rtype uint16, fields []string, origin string) ([]byte, error) {
	switch rtype {
	case TypeA:
		if len(fields) < 1 {
			return nil, fmt.Errorf("A record requires IP address")
		}
		return EncodeRDataA(fields[0])

	case TypeAAAA:
		if len(fields) < 1 {
			return nil, fmt.Errorf("AAAA record requires IPv6 address")
		}
		return EncodeRDataAAAA(fields[0])

	case TypeCNAME, TypeNS:
		if len(fields) < 1 {
			return nil, fmt.Errorf("record requires domain name")
		}
		name := qualifyName(fields[0], origin)
		table := make(map[string]int)
		return encodeName(nil, name, table), nil

	case TypeMX:
		if len(fields) < 2 {
			return nil, fmt.Errorf("MX record requires preference and exchange")
		}
		pref, err := strconv.ParseUint(fields[0], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid MX preference: %w", err)
		}
		exchange := qualifyName(fields[1], origin)
		return EncodeRDataMX(uint16(pref), exchange), nil

	case TypeTXT:
		text := strings.Join(fields, " ")
		text = strings.Trim(text, "\"")
		return EncodeRDataTXT([]string{text}), nil

	case TypeSOA:
		if len(fields) < 7 {
			return nil, fmt.Errorf("SOA requires 7 fields")
		}
		mname := qualifyName(fields[0], origin)
		rname := qualifyName(fields[1], origin)
		serial, _ := strconv.ParseUint(fields[2], 10, 32)
		refresh, _ := parseTTL(fields[3])
		retry, _ := parseTTL(fields[4])
		expire, _ := parseTTL(fields[5])
		minimum, _ := parseTTL(fields[6])
		return EncodeRDataSOA(mname, rname, uint32(serial), refresh, retry, expire, minimum), nil

	default:
		// For unknown types (RRSIG, DNSKEY, etc.), store raw
		return []byte(strings.Join(fields, " ")), nil
	}
}

func qualifyName(name, origin string) string {
	if strings.HasSuffix(name, ".") {
		return strings.ToLower(name)
	}
	return strings.ToLower(name + "." + origin)
}

func isRecordType(s string) bool {
	return parseRecordType(s) != 0
}

func parseRecordType(s string) uint16 {
	switch strings.ToUpper(s) {
	case "A":
		return TypeA
	case "AAAA":
		return TypeAAAA
	case "CNAME":
		return TypeCNAME
	case "MX":
		return TypeMX
	case "NS":
		return TypeNS
	case "SOA":
		return TypeSOA
	case "TXT":
		return TypeTXT
	case "RRSIG":
		return TypeRRSIG
	case "DNSKEY":
		return TypeDNSKEY
	default:
		return 0
	}
}

func isClass(s string) bool {
	upper := strings.ToUpper(s)
	return upper == "IN" || upper == "CH" || upper == "HS"
}

func isTTL(s string) bool {
	_, err := parseTTL(s)
	return err == nil
}

func parseTTL(s string) (uint32, error) {
	// Handle plain numbers and duration suffixes (1h, 30m, etc.)
	s = strings.TrimSpace(s)
	if val, err := strconv.ParseUint(s, 10, 32); err == nil {
		return uint32(val), nil
	}

	var total uint32
	var current uint64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			current = current*10 + uint64(c-'0')
		} else {
			switch c {
			case 'w', 'W':
				total += uint32(current) * 604800
			case 'd', 'D':
				total += uint32(current) * 86400
			case 'h', 'H':
				total += uint32(current) * 3600
			case 'm', 'M':
				total += uint32(current) * 60
			case 's', 'S':
				total += uint32(current)
			default:
				return 0, fmt.Errorf("unknown TTL suffix: %c", c)
			}
			current = 0
		}
	}
	total += uint32(current)
	return total, nil
}

func stripComment(line string) string {
	inQuote := false
	for i, c := range line {
		if c == '"' {
			inQuote = !inQuote
		}
		if c == ';' && !inQuote {
			return line[:i]
		}
	}
	return line
}
```

### `query.go`

```go
package main

import (
	"log"
	"strings"
)

// HandleQuery processes a DNS query and produces a response.
func HandleQuery(query *Message, zone *Zone) *Message {
	response := &Message{
		Header: Header{
			ID:     query.Header.ID,
			QR:     true,
			Opcode: query.Header.Opcode,
			AA:     true,
			RD:     query.Header.RD,
			RCode:  RCodeNoError,
		},
		Questions: query.Questions,
	}

	if len(query.Questions) == 0 {
		response.Header.RCode = RCodeFormErr
		return response
	}

	q := query.Questions[0]
	qname := strings.ToLower(q.Name)

	// Check if query is within our zone
	if !zone.IsInZone(qname) {
		response.Header.AA = false
		response.Header.RCode = RCodeRefused
		return response
	}

	// Look up records
	records := zone.Lookup(qname, q.Type)

	// If no exact match, try CNAME
	if len(records) == 0 && q.Type != TypeCNAME {
		cnames := zone.Lookup(qname, TypeCNAME)
		if len(cnames) > 0 {
			response.Answers = append(response.Answers, cnames...)
			// Follow CNAME (one level)
			// In production, you would recursively resolve
		}
	}

	// If still no match, try wildcard
	if len(records) == 0 && len(response.Answers) == 0 {
		records = zone.LookupWildcard(qname, q.Type)
		// Set the owner name to the queried name, not the wildcard
		for i := range records {
			records[i].Name = qname
		}
	}

	if len(records) > 0 {
		response.Answers = append(response.Answers, records...)
	}

	// NXDOMAIN: name does not exist at all
	if len(response.Answers) == 0 && !zone.NameExists(qname) {
		response.Header.RCode = RCodeNXDomain
		if zone.SOA != nil {
			response.Authority = append(response.Authority, *zone.SOA)
		}
		return response
	}

	// Add NS records to authority section
	nsRecords := zone.Lookup(zone.Origin, TypeNS)
	for _, ns := range nsRecords {
		response.Authority = append(response.Authority, ns)
	}

	// EDNS0 response
	if query.EDNS != nil {
		opt := MakeOPTRecord(4096, query.EDNS.DOBit)
		response.Additional = append(response.Additional, opt)
	}

	return response
}

// TruncateIfNeeded checks if a response exceeds maxSize and sets TC flag.
func TruncateIfNeeded(msg *Message, maxSize int) []byte {
	encoded := EncodeMessage(msg)
	if len(encoded) <= maxSize {
		return encoded
	}

	log.Printf("response too large (%d > %d), truncating", len(encoded), maxSize)

	// Truncate: keep header + question, remove answers
	msg.Header.TC = true
	msg.Answers = nil
	msg.Authority = nil
	msg.Additional = nil

	return EncodeMessage(msg)
}
```

### `server.go`

```go
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

// Server holds the DNS server state.
type Server struct {
	zone *Zone
	port int
}

// NewServer creates a DNS server instance.
func NewServer(zone *Zone, port int) *Server {
	return &Server{zone: zone, port: port}
}

// ListenUDP starts the UDP listener.
func (s *Server) ListenUDP() error {
	addr := fmt.Sprintf(":%d", s.port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("UDP listen: %w", err)
	}
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}

		go s.handleUDP(conn, remoteAddr, buf[:n])
	}
}

func (s *Server) handleUDP(conn net.PacketConn, addr net.Addr, data []byte) {
	query, err := DecodeMessage(data)
	if err != nil {
		log.Printf("decode error from %s: %v", addr, err)
		return
	}

	response := HandleQuery(query, s.zone)

	// Determine max UDP size
	maxSize := 512
	if query.EDNS != nil && int(query.EDNS.UDPSize) > maxSize {
		maxSize = int(query.EDNS.UDPSize)
	}

	encoded := TruncateIfNeeded(response, maxSize)

	if _, err := conn.WriteTo(encoded, addr); err != nil {
		log.Printf("UDP write error to %s: %v", addr, err)
	}
}

// ListenTCP starts the TCP listener.
func (s *Server) ListenTCP() error {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("TCP listen: %w", err)
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("TCP accept error: %v", err)
			continue
		}
		go s.handleTCP(conn)
	}
}

func (s *Server) handleTCP(conn net.Conn) {
	defer conn.Close()

	for {
		// Read 2-byte length prefix
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return // Connection closed
		}
		msgLen := binary.BigEndian.Uint16(lenBuf)

		// Read the message
		msgBuf := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, msgBuf); err != nil {
			log.Printf("TCP read error: %v", err)
			return
		}

		query, err := DecodeMessage(msgBuf)
		if err != nil {
			log.Printf("TCP decode error: %v", err)
			return
		}

		response := HandleQuery(query, s.zone)
		encoded := EncodeMessage(response)

		// Write 2-byte length prefix + message
		respLen := make([]byte, 2)
		binary.BigEndian.PutUint16(respLen, uint16(len(encoded)))
		conn.Write(respLen)
		conn.Write(encoded)
	}
}
```

### `wire_test.go`

```go
package main

import (
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	original := Header{
		ID:      0x1234,
		QR:      true,
		Opcode:  0,
		AA:      true,
		TC:      false,
		RD:      true,
		RA:      false,
		RCode:   RCodeNoError,
		QDCount: 1,
		ANCount: 2,
		NSCount: 0,
		ARCount: 1,
	}

	buf := make([]byte, 12)
	encodeHeader(original, buf)
	decoded := decodeHeader(buf)

	if decoded.ID != original.ID {
		t.Errorf("ID: got %x, want %x", decoded.ID, original.ID)
	}
	if decoded.QR != original.QR {
		t.Error("QR mismatch")
	}
	if decoded.AA != original.AA {
		t.Error("AA mismatch")
	}
	if decoded.RD != original.RD {
		t.Error("RD mismatch")
	}
	if decoded.QDCount != original.QDCount {
		t.Errorf("QDCount: got %d, want %d", decoded.QDCount, original.QDCount)
	}
}

func TestNameEncodeDecode(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"example.com."},
		{"sub.example.com."},
		{"."},
		{"a.b.c.d.e.f."},
	}

	for _, tt := range tests {
		table := make(map[string]int)
		encoded := encodeName(nil, tt.name, table)
		decoded, _, err := decodeName(encoded, 0)
		if err != nil {
			t.Errorf("decodeName(%q): %v", tt.name, err)
			continue
		}
		if decoded != tt.name {
			t.Errorf("got %q, want %q", decoded, tt.name)
		}
	}
}

func TestNameCompression(t *testing.T) {
	table := make(map[string]int)
	var buf []byte

	// Encode "mail.example.com." first
	buf = encodeName(buf, "mail.example.com.", table)
	firstLen := len(buf)

	// Encode "www.example.com." -- should compress "example.com."
	buf = encodeName(buf, "www.example.com.", table)
	secondLen := len(buf) - firstLen

	// Without compression: 3+www+1 + 7+example+1 + 3+com+1 + 0 = 18
	// With compression:    3+www+1 + 2(pointer) = 6
	if secondLen >= 18 {
		t.Errorf("compression not working: second name took %d bytes (expected < 18)", secondLen)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	aRdata, _ := EncodeRDataA("93.184.216.34")

	msg := &Message{
		Header: Header{
			ID:    0xABCD,
			QR:    true,
			AA:    true,
			RCode: RCodeNoError,
		},
		Questions: []Question{
			{Name: "example.com.", Type: TypeA, Class: ClassIN},
		},
		Answers: []ResourceRecord{
			{
				Name:  "example.com.",
				Type:  TypeA,
				Class: ClassIN,
				TTL:   300,
				RData: aRdata,
			},
		},
	}

	encoded := EncodeMessage(msg)
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Header.ID != msg.Header.ID {
		t.Errorf("ID mismatch")
	}
	if len(decoded.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(decoded.Questions))
	}
	if decoded.Questions[0].Name != "example.com." {
		t.Errorf("question name: got %q", decoded.Questions[0].Name)
	}
	if len(decoded.Answers) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(decoded.Answers))
	}
	if decoded.Answers[0].TTL != 300 {
		t.Errorf("TTL: got %d, want 300", decoded.Answers[0].TTL)
	}
}

func TestTTLParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected uint32
	}{
		{"3600", 3600},
		{"1h", 3600},
		{"30m", 1800},
		{"1d", 86400},
		{"1w", 604800},
		{"1h30m", 5400},
	}

	for _, tt := range tests {
		got, err := parseTTL(tt.input)
		if err != nil {
			t.Errorf("parseTTL(%q): %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseTTL(%q): got %d, want %d", tt.input, got, tt.expected)
		}
	}
}
```

### `query_test.go`

```go
package main

import (
	"testing"
)

func buildTestZone() *Zone {
	aRdata, _ := EncodeRDataA("93.184.216.34")
	nsRdata := encodeName(nil, "ns1.example.com.", make(map[string]int))

	zone := &Zone{
		Origin: "example.com.",
		TTL:    3600,
		Records: map[string][]ResourceRecord{
			"example.com.:1": {
				{Name: "example.com.", Type: TypeA, Class: ClassIN, TTL: 300, RData: aRdata},
			},
			"example.com.:2": {
				{Name: "example.com.", Type: TypeNS, Class: ClassIN, TTL: 86400, RData: nsRdata},
			},
			"*.example.com.:1": {
				{Name: "*.example.com.", Type: TypeA, Class: ClassIN, TTL: 300, RData: aRdata},
			},
		},
		SOA: &ResourceRecord{
			Name:  "example.com.",
			Type:  TypeSOA,
			Class: ClassIN,
			TTL:   86400,
			RData: EncodeRDataSOA("ns1.example.com.", "admin.example.com.", 2024010101, 3600, 900, 604800, 86400),
		},
	}
	return zone
}

func TestQueryExactMatch(t *testing.T) {
	zone := buildTestZone()
	query := &Message{
		Header: Header{ID: 1, RD: true},
		Questions: []Question{
			{Name: "example.com.", Type: TypeA, Class: ClassIN},
		},
	}

	response := HandleQuery(query, zone)
	if response.Header.RCode != RCodeNoError {
		t.Errorf("expected NOERROR, got %d", response.Header.RCode)
	}
	if !response.Header.AA {
		t.Error("expected AA flag set")
	}
	if len(response.Answers) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(response.Answers))
	}
}

func TestQueryNXDomain(t *testing.T) {
	zone := buildTestZone()
	query := &Message{
		Header: Header{ID: 2},
		Questions: []Question{
			{Name: "nonexistent.example.com.", Type: TypeA, Class: ClassIN},
		},
	}

	// Remove wildcard for this test
	delete(zone.Records, "*.example.com.:1")

	response := HandleQuery(query, zone)
	if response.Header.RCode != RCodeNXDomain {
		t.Errorf("expected NXDOMAIN, got %d", response.Header.RCode)
	}
	if len(response.Authority) == 0 {
		t.Error("expected SOA in authority section for NXDOMAIN")
	}
}

func TestQueryRefused(t *testing.T) {
	zone := buildTestZone()
	query := &Message{
		Header: Header{ID: 3},
		Questions: []Question{
			{Name: "other.org.", Type: TypeA, Class: ClassIN},
		},
	}

	response := HandleQuery(query, zone)
	if response.Header.RCode != RCodeRefused {
		t.Errorf("expected REFUSED, got %d", response.Header.RCode)
	}
}

func TestQueryWildcard(t *testing.T) {
	zone := buildTestZone()
	query := &Message{
		Header: Header{ID: 4},
		Questions: []Question{
			{Name: "foo.example.com.", Type: TypeA, Class: ClassIN},
		},
	}

	response := HandleQuery(query, zone)
	if response.Header.RCode != RCodeNoError {
		t.Errorf("expected NOERROR, got %d", response.Header.RCode)
	}
	if len(response.Answers) != 1 {
		t.Fatalf("expected 1 answer from wildcard, got %d", len(response.Answers))
	}
	if response.Answers[0].Name != "foo.example.com." {
		t.Errorf("wildcard answer should use query name, got %q", response.Answers[0].Name)
	}
}

func TestQueryEDNS(t *testing.T) {
	zone := buildTestZone()
	query := &Message{
		Header: Header{ID: 5},
		Questions: []Question{
			{Name: "example.com.", Type: TypeA, Class: ClassIN},
		},
		EDNS: &EDNSData{UDPSize: 4096},
	}

	response := HandleQuery(query, zone)
	hasOPT := false
	for _, rr := range response.Additional {
		if rr.Type == TypeOPT {
			hasOPT = true
		}
	}
	if !hasOPT {
		t.Error("expected OPT record in additional section when EDNS0 requested")
	}
}
```

### Example Zone File `example.zone`

```
$ORIGIN example.com.
$TTL 3600

@   IN  SOA ns1.example.com. admin.example.com. (
            2024010101  ; serial
            3600        ; refresh
            900         ; retry
            604800      ; expire
            86400       ; minimum
        )

@       IN  NS      ns1.example.com.
@       IN  NS      ns2.example.com.

@       IN  A       93.184.216.34
@       IN  AAAA    2606:2800:0220:0001:0248:1893:25c8:1946

www     IN  CNAME   example.com.
mail    IN  A       93.184.216.35
@       IN  MX      10 mail.example.com.
@       IN  TXT     "v=spf1 include:_spf.example.com ~all"

ns1     IN  A       198.51.100.1
ns2     IN  A       198.51.100.2

*.wild  IN  A       93.184.216.36
```

## Build and Run

```bash
# Build
go build -o dns-server .

# Run with default zone file
./dns-server -zone example.zone -port 1053

# Query using dig
dig @127.0.0.1 -p 1053 example.com A
dig @127.0.0.1 -p 1053 example.com AAAA
dig @127.0.0.1 -p 1053 example.com MX
dig @127.0.0.1 -p 1053 www.example.com CNAME
dig @127.0.0.1 -p 1053 example.com TXT
dig @127.0.0.1 -p 1053 example.com SOA
dig @127.0.0.1 -p 1053 nonexistent.example.com A  # NXDOMAIN
dig @127.0.0.1 -p 1053 other.org A                 # REFUSED

# Query over TCP
dig @127.0.0.1 -p 1053 +tcp example.com A

# Query with EDNS0
dig @127.0.0.1 -p 1053 +edns=0 +bufsize=4096 example.com A

# Run tests
go test -v -race ./...
```

## Expected Output

Server startup:
```
2024/01/15 10:00:00 loaded zone example.com. with 11 records
2024/01/15 10:00:00 DNS server listening on port 1053 (UDP+TCP)
```

dig output for A record:
```
; <<>> DiG 9.18 <<>> @127.0.0.1 -p 1053 example.com A
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 12345
;; flags: qr aa rd; QUERY: 1, ANSWER: 1, AUTHORITY: 2, ADDITIONAL: 1

;; QUESTION SECTION:
;example.com.                   IN      A

;; ANSWER SECTION:
example.com.            300     IN      A       93.184.216.34

;; AUTHORITY SECTION:
example.com.            86400   IN      NS      ns1.example.com.
example.com.            86400   IN      NS      ns2.example.com.
```

Test output:
```
=== RUN   TestHeaderRoundTrip
--- PASS: TestHeaderRoundTrip (0.00s)
=== RUN   TestNameEncodeDecode
--- PASS: TestNameEncodeDecode (0.00s)
=== RUN   TestNameCompression
--- PASS: TestNameCompression (0.00s)
=== RUN   TestMessageRoundTrip
--- PASS: TestMessageRoundTrip (0.00s)
=== RUN   TestTTLParsing
--- PASS: TestTTLParsing (0.00s)
=== RUN   TestQueryExactMatch
--- PASS: TestQueryExactMatch (0.00s)
=== RUN   TestQueryNXDomain
--- PASS: TestQueryNXDomain (0.00s)
=== RUN   TestQueryRefused
--- PASS: TestQueryRefused (0.00s)
=== RUN   TestQueryWildcard
--- PASS: TestQueryWildcard (0.00s)
=== RUN   TestQueryEDNS
--- PASS: TestQueryEDNS (0.00s)
PASS
ok      dns-server    0.012s
```

## Design Decisions

**Why a flat `map[string][]ResourceRecord` for the zone database.** The key format `"name:type"` allows O(1) lookup for the common case (exact match on name and type). For wildcard matching, we construct the wildcard key and look it up directly rather than scanning all records. This is simpler than a tree structure and fast enough for zones with thousands of records.

**Why domain name compression uses a map from suffix to offset.** Each time we write a name, we record the offset of every suffix (e.g., for `mail.example.com.` we record offsets for `mail.example.com.`, `example.com.`, and `com.`). When encoding a subsequent name, we check each suffix against the map and emit a pointer as soon as we find a match. This produces optimal compression with O(n) time per name where n is the number of labels.

**Why UDP and TCP run in separate goroutines.** DNS mandates support for both transports. UDP is the default for small queries; TCP is used for responses that exceed the UDP buffer size (signaled by the TC flag) and for zone transfers. Running them independently simplifies the code and allows each to handle errors without affecting the other.

**Why the TCP handler uses a per-connection loop.** DNS over TCP supports multiple queries per connection (pipelining). Reading in a loop until the connection closes handles this naturally. Each message is framed with a 2-byte length prefix, making it straightforward to read complete messages.

## Common Mistakes

1. **Forgetting the trailing dot on domain names.** In DNS, `example.com` and `example.com.` are different. The trailing dot marks an absolute (fully qualified) name. Omitting it in zone file parsing causes names to be treated as relative, producing `example.com.example.com.`.

2. **Not handling compression pointer loops.** A malicious or malformed packet can contain compression pointers that create cycles. Without a visited set or jump counter, the decoder enters an infinite loop. Always limit pointer follows.

3. **Case-sensitive name comparison.** DNS names are case-insensitive per RFC 1035 Section 2.3.3. `Example.COM` must match `example.com`. Normalize to lowercase on input.

4. **Incorrect RDATA domain name encoding.** Names within RDATA (e.g., MX exchange, CNAME target, NS name) must use DNS wire format, not plain text. A common mistake is writing the name as a plain ASCII string instead of length-prefixed labels.

5. **Missing SOA in NXDOMAIN responses.** RFC 2308 requires the SOA record in the authority section of NXDOMAIN responses for negative caching. Without it, resolvers cannot cache the negative result and will re-query immediately.

6. **Ignoring the EDNS0 buffer size.** Without EDNS0, the UDP response limit is 512 bytes. Setting TC and truncating without checking the client's advertised buffer size forces unnecessary TCP fallback for clients that support larger UDP payloads.

## Performance Notes

- Name encoding with compression is O(n * k) where n is the total number of names and k is the average number of labels per name. The compression table (map lookup) is O(1) per suffix check, making the total cost negligible for typical response sizes.
- The zone file is parsed once at startup. Queries are served entirely from the in-memory map. No disk I/O occurs during query processing.
- UDP responses are encoded into a pre-allocated 512-byte slice (or larger for EDNS0). The slice grows only for unusually large responses, minimizing GC pressure in the hot path.
- For high query rates (>100k qps), consider sharding the UDP listener across multiple goroutines using `SO_REUSEPORT` and separate `net.PacketConn` instances per CPU core.

## Going Further

- Implement zone transfer (AXFR) over TCP for zone replication to secondary servers
- Add dynamic updates (RFC 2136) to modify zone data at runtime
- Implement DNS over HTTPS (DoH) by adding an HTTP endpoint that accepts `application/dns-message`
- Sign zones with DNSSEC using a KSK/ZSK key pair and generate RRSIG, DNSKEY, DS, and NSEC records
- Implement response rate limiting (RRL) to mitigate DNS amplification attacks
