// Copyright 2026 Su Yang
// SPDX-License-Identifier: Apache-2.0

package metalink

import (
	"bytes"
	"crypto/md5"  // #nosec G501 -- MirrorManager compatibility only; SHA256/SHA512 is mandatory.
	"crypto/sha1" // #nosec G505 -- MirrorManager compatibility only; SHA256/SHA512 is mandatory.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const namespace = "http://www.metalinker.org/"
const timestampNamespace = "http://fedorahosted.org/mirrormanager"

type metalinkXML struct {
	XMLName xml.Name  `xml:"http://www.metalinker.org/ metalink"`
	Version string    `xml:"version,attr"`
	Files   []fileXML `xml:"http://www.metalinker.org/ files>file"`
}
type fileXML struct {
	Name       string        `xml:"name,attr"`
	Sizes      []string      `xml:"http://www.metalinker.org/ size"`
	Timestamps []string      `xml:"http://fedorahosted.org/mirrormanager timestamp"`
	Hashes     []hashXML     `xml:"http://www.metalinker.org/ verification>hash"`
	URLs       []resourceXML `xml:"http://www.metalinker.org/ resources>url"`
}
type hashXML struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}
type resourceXML struct {
	Protocol   string `xml:"protocol,attr"`
	Type       string `xml:"type,attr"`
	Preference string `xml:"preference,attr"`
	Value      string `xml:",chardata"`
}
type mirror struct {
	base       *url.URL
	preference int
}
type metadata struct {
	size      int64
	timestamp string
	hashes    []hashXML
	digest    string
	mirrors   []mirror
}

func (c *Controller) parse(data []byte) (*metadata, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, elements, roots, resources := 0, 0, 0, 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Metalink XML")
		}
		if _, ok := tok.(xml.Directive); ok {
			return nil, fmt.Errorf("xml directives are not allowed")
		}
		switch token := tok.(type) {
		case xml.StartElement:
			depth++
			elements++
			if depth == 1 {
				roots++
			}
			if token.Name.Local == "url" {
				resources++
			}
			if depth > 16 || elements > 2048 || roots > 1 || resources > 256 || len(token.Attr) > 16 {
				return nil, fmt.Errorf("metalink XML structure exceeds limits")
			}
			switch token.Name.Local {
			case "metalink", "files", "file", "verification", "hash", "resources", "url", "size":
				if token.Name.Space != namespace {
					return nil, fmt.Errorf("invalid Metalink element namespace")
				}
			case "timestamp":
				if token.Name.Space != timestampNamespace {
					return nil, fmt.Errorf("invalid Metalink timestamp namespace")
				}
			}
			seen := map[xml.Name]bool{}
			for _, attr := range token.Attr {
				if seen[attr.Name] || len(attr.Value) > 4096 {
					return nil, fmt.Errorf("invalid XML attributes")
				}
				seen[attr.Name] = true
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return nil, fmt.Errorf("unexpected XML text")
			}
			if len(token) > 8192 {
				return nil, fmt.Errorf("metalink XML text exceeds limits")
			}
		}
	}
	var doc metalinkXML
	if err := xml.Unmarshal(data, &doc); err != nil || doc.Version != "3.0" || len(doc.Files) != 1 || doc.Files[0].Name != "repomd.xml" {
		return nil, fmt.Errorf("expected one Metalink v3 repomd.xml file")
	}
	f := doc.Files[0]
	if len(f.Sizes) != 1 || len(f.Timestamps) > 1 || len(f.Hashes) == 0 || len(f.Hashes) > 8 || len(f.URLs) == 0 || len(f.URLs) > 256 {
		return nil, fmt.Errorf("invalid Metalink file fields")
	}
	size, err := strconv.ParseInt(strings.TrimSpace(f.Sizes[0]), 10, 64)
	if err != nil || size <= 0 || size > c.cfg.MaxRepomdBytes {
		return nil, fmt.Errorf("invalid repomd.xml size")
	}
	m := &metadata{size: size}
	if len(f.Timestamps) == 1 {
		m.timestamp = strings.TrimSpace(f.Timestamps[0])
		if n, err := strconv.ParseUint(m.timestamp, 10, 64); err != nil || n == 0 {
			return nil, fmt.Errorf("invalid Metalink timestamp")
		}
	}
	seen := map[string]bool{}
	for _, hash := range f.Hashes {
		hash.Type = strings.ToLower(strings.TrimSpace(hash.Type))
		hash.Value = strings.ToLower(strings.TrimSpace(hash.Value))
		length := map[string]int{"md5": 16, "sha1": 20, "sha256": 32, "sha512": 64}[hash.Type]
		decoded, err := hex.DecodeString(hash.Value)
		if length == 0 || err != nil || len(decoded) != length || seen[hash.Type] {
			return nil, fmt.Errorf("invalid or duplicate Metalink hash")
		}
		seen[hash.Type] = true
		m.hashes = append(m.hashes, hash)
	}
	if !seen["sha256"] && !seen["sha512"] {
		return nil, fmt.Errorf("metalink requires SHA256 or SHA512")
	}
	sort.Slice(m.hashes, func(i, j int) bool { return m.hashes[i].Type < m.hashes[j].Type })
	for _, h := range m.hashes {
		if h.Type == "sha256" {
			m.digest = h.Type + "-" + h.Value
		}
	}
	if m.digest == "" {
		for _, h := range m.hashes {
			if h.Type == "sha512" {
				m.digest = h.Type + "-" + h.Value
			}
		}
	}
	unique := map[string]bool{}
	for _, resource := range f.URLs {
		value := strings.TrimSpace(resource.Value)
		if len(value) > 4096 {
			continue
		}
		u, err := parseURL(value)
		if err != nil || !strings.HasSuffix(u.Path, "/repodata/repomd.xml") {
			continue
		}
		host, err := normalizeHost(u.Hostname())
		if err != nil || !c.allowed[host] || (resource.Protocol != "" && resource.Protocol != u.Scheme) || (resource.Type != "" && resource.Type != u.Scheme) {
			continue
		}
		if c.outer != nil && c.outer.CheckURL(u) != nil {
			continue
		}
		preference := 1
		if resource.Preference != "" {
			preference, err = strconv.Atoi(resource.Preference)
			if err != nil || preference < 0 || preference > 100 {
				continue
			}
		}
		u.Path = strings.TrimSuffix(u.Path, "repodata/repomd.xml")
		if unique[u.String()] {
			continue
		}
		unique[u.String()] = true
		m.mirrors = append(m.mirrors, mirror{u, preference})
	}
	if len(m.mirrors) == 0 {
		return nil, fmt.Errorf("no allowed Metalink mirrors")
	}
	sort.Slice(m.mirrors, func(i, j int) bool {
		if m.mirrors[i].preference != m.mirrors[j].preference {
			return m.mirrors[i].preference > m.mirrors[j].preference
		}
		return m.mirrors[i].base.String() < m.mirrors[j].base.String()
	})
	return m, nil
}

func (m *metadata) verify(data []byte) bool {
	if int64(len(data)) != m.size {
		return false
	}
	for _, hash := range m.hashes {
		var digest []byte
		switch hash.Type {
		case "md5":
			sum := md5.Sum(data) // #nosec G401 -- Also verifies the mandatory SHA256/SHA512 digest.
			digest = sum[:]
		case "sha1":
			sum := sha1.Sum(data) // #nosec G401 -- Also verifies the mandatory SHA256/SHA512 digest.
			digest = sum[:]
		case "sha256":
			sum := sha256.Sum256(data)
			digest = sum[:]
		case "sha512":
			sum := sha512.Sum512(data)
			digest = sum[:]
		}
		if hex.EncodeToString(digest) != hash.Value {
			return false
		}
	}
	return true
}

func (c *Controller) render(s *snapshot) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<metalink xmlns="` + namespace + `" xmlns:mm0="` + timestampNamespace + `" version="3.0" type="dynamic"><files><file name="repomd.xml">`)
	if s.metadata.timestamp != "" {
		b.WriteString(`<mm0:timestamp>` + s.metadata.timestamp + `</mm0:timestamp>`)
	}
	b.WriteString(`<size>` + strconv.FormatInt(s.metadata.size, 10) + `</size><verification>`)
	for _, h := range s.metadata.hashes {
		b.WriteString(`<hash type="` + h.Type + `">` + h.Value + `</hash>`)
	}
	b.WriteString(`</verification><resources maxconnections="1"><url protocol="` + c.public.Scheme + `" type="` + c.public.Scheme + `" preference="100">`)
	_ = xml.EscapeText(&b, []byte(c.public.String()+"/metalink/repos/"+s.token+"/repodata/repomd.xml"))
	b.WriteString(`</url></resources></file></files></metalink>`)
	return b.Bytes()
}
