// Command openapi-split decomposes the canonical Qeet Logs OpenAPI document into
// four self-contained, bounded-context specs under api/openapi/ — and merges them
// back into a single document for tooling (codegen, Swagger UI) that wants one file.
//
//	cd tools/openapi-split
//	go run . split    # api/openapi/openapi.yaml -> api/openapi/{ingest,query,admin,operations}.yaml
//	go run . merge    # api/openapi/*.yaml        -> one doc on stdout
//	go run . verify   # assert each split file is self-contained
//
// The split is a ONE-TIME migration: after it runs, the four files ARE the source
// of truth and api/openapi/openapi.yaml is removed. The tool is kept in-repo so the
// bucketing is reproducible and documented, and so `merge` can feed codegen.
//
// This lives in a nested module (its own go.mod) so gopkg.in/yaml.v3 stays out of
// the production server's dependency graph.
//
// Correctness notes:
//   - Operates on a yaml.Node tree to preserve key order in the source files.
//   - Expands every YAML alias (if any) so no output file dangles, then clears anchors.
//   - Each output file carries the transitive $ref closure of the components it
//     needs (schemas/parameters/responses/…), plus all securitySchemes, plus the
//     document's top-level security requirement (Qeet Logs relies on a global
//     security block; only public routes override it per-operation).
//   - Each output file gets the servers appropriate to its host (ingest gateway vs
//     query/admin API).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// tagToFile maps every operation-level tag to its destination file (basename
// without extension). Exhaustive over the 22 tags currently in the spec; the tool
// fails loudly if an operation carries a tag absent from this map. Note the em-dash
// (U+2014) in the tag names — they must match the spec byte-for-byte.
var tagToFile = map[string]string{
	// ingest gateway (ingest.logs.qeet.in)
	"Ingest — Logs":    "ingest",
	"Ingest — Metrics": "ingest",
	"Ingest — Traces":  "ingest",
	// query & investigation (api.logs.qeet.in)
	"Query":      "query",
	"PromQL":     "query",
	"Changes":    "query",
	"Topology":   "query",
	"Timeline":   "query",
	"Incidents":  "query",
	"RCA":        "query",
	"Dashboards": "query", // overlays + public shared-dashboard read
	// admin (api.logs.qeet.in)
	"Admin — API Keys":       "admin",
	"Admin — Alerting":       "admin",
	"Admin — Retention":      "admin",
	"Admin — Transform":      "admin",
	"Admin — Erasure":        "admin",
	"Admin — Dashboards":     "admin",
	"Admin — Saved Searches": "admin",
	"Admin — Audit":          "admin",
	"Admin — DLQ":            "admin",
	"Admin — Quota":          "admin",
	// operations / system (api.logs.qeet.in)
	"Health": "operations",
}

var fileTitles = map[string]string{
	"ingest":     "Qeet Logs — Ingest API",
	"query":      "Qeet Logs — Query & Investigation API",
	"admin":      "Qeet Logs — Admin API",
	"operations": "Qeet Logs — Operations API",
}

var fileOrder = []string{"ingest", "query", "admin", "operations"}

type serverDef struct{ url, desc string }

// fileServers overrides each output file's top-level servers with the hosts that
// actually serve it — the ingest gateway vs the query/admin API.
var fileServers = map[string][]serverDef{
	"ingest": {
		{"https://ingest.logs.qeet.in", "Production — ingest gateway"},
		{"http://localhost:8101", "Local dev — ingest gateway"},
	},
	"query": {
		{"https://api.logs.qeet.in", "Production — query & admin"},
		{"http://localhost:8100", "Local dev — query API"},
	},
	"admin": {
		{"https://api.logs.qeet.in", "Production — query & admin"},
		{"http://localhost:8100", "Local dev — query API"},
	},
	"operations": {
		{"https://api.logs.qeet.in", "Production — query & admin"},
		{"http://localhost:8100", "Local dev — query API"},
	},
}

var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: openapi-split <split|merge|verify>")
		os.Exit(2)
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		fatal(err)
	}
	switch os.Args[1] {
	case "split":
		fatalIf(doSplit(repoRoot))
	case "merge":
		fatalIf(doMerge(repoRoot, os.Stdout))
	case "verify":
		fatalIf(doVerify(repoRoot))
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q (want split|merge|verify)\n", os.Args[1])
		os.Exit(2)
	}
}

// ── split ────────────────────────────────────────────────────────────────────

func doSplit(repoRoot string) error {
	src := filepath.Join(repoRoot, "api", "openapi", "openapi.yaml")
	root, err := loadDoc(src)
	if err != nil {
		return err
	}
	expandAliases(root)
	clearAnchors(root)

	openapiNode := mapGet(root, "openapi")
	infoNode := mapGet(root, "info")
	securityNode := mapGet(root, "security")
	tagsNode := mapGet(root, "tags")
	pathsNode := mapGet(root, "paths")
	componentsNode := mapGet(root, "components")
	if pathsNode == nil || componentsNode == nil {
		return fmt.Errorf("%s: missing paths or components", src)
	}

	// Bucket each path into its file.
	filePaths := map[string][]*yaml.Node{} // file -> flat [key,val,key,val,...]
	fileTags := map[string]map[string]bool{}
	var conflicts []string
	for i := 0; i+1 < len(pathsNode.Content); i += 2 {
		pathKey := pathsNode.Content[i]
		pathVal := pathsNode.Content[i+1]
		file, tags, err := classifyPath(pathKey.Value, pathVal)
		if err != nil {
			conflicts = append(conflicts, err.Error())
			continue
		}
		filePaths[file] = append(filePaths[file], pathKey, pathVal)
		if fileTags[file] == nil {
			fileTags[file] = map[string]bool{}
		}
		for t := range tags {
			fileTags[file][t] = true
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("path classification problems:\n  %s", strings.Join(conflicts, "\n  "))
	}

	outDir := filepath.Join(repoRoot, "api", "openapi")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	total := 0
	for _, file := range fileOrder {
		entries := filePaths[file]
		paths := newMap()
		paths.Content = entries
		nPaths := len(entries) / 2
		total += nPaths

		// Transitive $ref closure for this file's paths.
		closure := map[string]map[string]bool{}
		collectRefs(paths, closure)
		resolveClosure(componentsNode, closure)

		components := buildComponents(componentsNode, closure)
		info := cloneNode(infoNode)
		setMapScalar(info, "title", fileTitles[file])
		tags := filterTags(tagsNode, fileTags[file])

		out := newMap()
		appendKV(out, "openapi", cloneNode(openapiNode))
		appendKV(out, "info", info)
		appendKVNode(out, "servers", buildServers(fileServers[file]))
		if securityNode != nil {
			appendKV(out, "security", cloneNode(securityNode))
		}
		if tags != nil {
			appendKVNode(out, "tags", tags)
		}
		appendKVNode(out, "paths", paths)
		appendKVNode(out, "components", components)

		dst := filepath.Join(outDir, file+".yaml")
		if err := writeDoc(dst, out); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d paths, %d schemas)\n", dst, nPaths, countSection(components, "schemas"))
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove monolith: %w", err)
	}
	fmt.Printf("removed %s — %d paths split across %d files\n", src, total, len(fileOrder))
	return nil
}

// classifyPath returns the destination file for a path and the set of tags its
// operations use. Errors if a tag is unmapped or operations span >1 file.
func classifyPath(path string, item *yaml.Node) (string, map[string]bool, error) {
	tags := map[string]bool{}
	files := map[string]bool{}
	for i := 0; i+1 < len(item.Content); i += 2 {
		method := strings.ToLower(item.Content[i].Value)
		if !httpMethods[method] {
			continue
		}
		op := item.Content[i+1]
		opTags := mapGet(op, "tags")
		if opTags == nil || len(opTags.Content) == 0 {
			return "", nil, fmt.Errorf("%s %s: operation has no tags", strings.ToUpper(method), path)
		}
		for _, t := range opTags.Content {
			tags[t.Value] = true
			file, ok := tagToFile[t.Value]
			if !ok {
				return "", nil, fmt.Errorf("%s %s: tag %q is not mapped to a file", strings.ToUpper(method), path, t.Value)
			}
			files[file] = true
		}
	}
	if len(files) == 0 {
		return "", nil, fmt.Errorf("%s: no documented HTTP methods", path)
	}
	if len(files) > 1 {
		var fs []string
		for f := range files {
			fs = append(fs, f)
		}
		sort.Strings(fs)
		return "", nil, fmt.Errorf("%s: operations span multiple files %v — needs manual assignment", path, fs)
	}
	for f := range files {
		return f, tags, nil
	}
	return "", nil, fmt.Errorf("%s: unreachable", path)
}

// ── merge ────────────────────────────────────────────────────────────────────

func doMerge(repoRoot string, out *os.File) error {
	dir := filepath.Join(repoRoot, "api", "openapi")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no specs in %s", dir)
	}
	sort.Strings(files)

	mergedPaths := newMap()
	seenPath := map[string]bool{}
	// section -> ordered name list + dedupe set, and name -> node
	sectionOrder := []string{"securitySchemes", "parameters", "responses", "requestBodies", "headers", "schemas"}
	compNodes := map[string]map[string]*yaml.Node{}
	compOrder := map[string][]string{}

	var openapiNode, infoNode, serversNode, securityNode *yaml.Node
	tagSeen := map[string]bool{}
	mergedTags := newSeq()

	for _, f := range files {
		root, err := loadDoc(f)
		if err != nil {
			return err
		}
		expandAliases(root)
		clearAnchors(root)
		if openapiNode == nil {
			openapiNode = mapGet(root, "openapi")
			infoNode = mapGet(root, "info")
			securityNode = mapGet(root, "security")
		}
		// Prefer the api-host servers for the merged doc; the ingest file's
		// servers are host-specific and captured per-operation instead.
		if s := mapGet(root, "servers"); s != nil && serversNode == nil {
			serversNode = s
		}
		if p := mapGet(root, "paths"); p != nil {
			for i := 0; i+1 < len(p.Content); i += 2 {
				k, v := p.Content[i], p.Content[i+1]
				if seenPath[k.Value] {
					return fmt.Errorf("duplicate path %q across files (expected disjoint)", k.Value)
				}
				seenPath[k.Value] = true
				mergedPaths.Content = append(mergedPaths.Content, k, v)
			}
		}
		if t := mapGet(root, "tags"); t != nil {
			for _, e := range t.Content {
				name := scalarChild(e, "name")
				if name != "" && !tagSeen[name] {
					tagSeen[name] = true
					mergedTags.Content = append(mergedTags.Content, e)
				}
			}
		}
		if c := mapGet(root, "components"); c != nil {
			for i := 0; i+1 < len(c.Content); i += 2 {
				section := c.Content[i].Value
				sec := c.Content[i+1]
				if compNodes[section] == nil {
					compNodes[section] = map[string]*yaml.Node{}
				}
				for j := 0; j+1 < len(sec.Content); j += 2 {
					name := sec.Content[j].Value
					if _, dup := compNodes[section][name]; !dup {
						compNodes[section][name] = sec.Content[j+1]
						compOrder[section] = append(compOrder[section], name)
					}
				}
			}
		}
	}

	components := newMap()
	for _, section := range sectionOrder {
		names := compOrder[section]
		if len(names) == 0 {
			continue
		}
		secMap := newMap()
		for _, name := range names {
			appendKVNode(secMap, name, compNodes[section][name])
		}
		appendKVNode(components, section, secMap)
	}

	merged := newMap()
	if openapiNode != nil {
		appendKV(merged, "openapi", cloneNode(openapiNode))
	}
	if infoNode != nil {
		info := cloneNode(infoNode)
		setMapScalar(info, "title", "Qeet Logs API")
		appendKV(merged, "info", info)
	}
	if serversNode != nil {
		appendKV(merged, "servers", cloneNode(serversNode))
	}
	if securityNode != nil {
		appendKV(merged, "security", cloneNode(securityNode))
	}
	if len(mergedTags.Content) > 0 {
		appendKVNode(merged, "tags", mergedTags)
	}
	appendKVNode(merged, "paths", mergedPaths)
	appendKVNode(merged, "components", components)

	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	if err := enc.Encode(merged); err != nil {
		return err
	}
	return enc.Close()
}

// ── verify ───────────────────────────────────────────────────────────────────

// doVerify asserts each split file is self-contained: every #/components ref
// resolves within that same file, and no YAML aliases survive.
func doVerify(repoRoot string) error {
	dir := filepath.Join(repoRoot, "api", "openapi")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no specs in %s", dir)
	}
	sort.Strings(files)

	var problems []string
	totalPaths := 0
	for _, f := range files {
		root, err := loadDoc(f)
		if err != nil {
			return err
		}
		if n := countAliases(root); n > 0 {
			problems = append(problems, fmt.Sprintf("%s: %d unexpanded YAML alias(es)", filepath.Base(f), n))
		}
		if p := mapGet(root, "paths"); p != nil {
			totalPaths += len(p.Content) / 2
		}
		components := mapGet(root, "components")
		refs := map[string]map[string]bool{}
		collectRefs(root, refs)
		for section, names := range refs {
			secNode := mapGet(components, section)
			for name := range names {
				if mapGet(secNode, name) == nil {
					problems = append(problems, fmt.Sprintf("%s: dangling $ref #/components/%s/%s", filepath.Base(f), section, name))
				}
			}
		}
		fmt.Printf("%-20s ok (%d paths)\n", filepath.Base(f), pathCount(root))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("verification failed:\n  %s", strings.Join(problems, "\n  "))
	}
	fmt.Printf("all %d files self-contained — %d paths total\n", len(files), totalPaths)
	return nil
}

func pathCount(root *yaml.Node) int {
	if p := mapGet(root, "paths"); p != nil {
		return len(p.Content) / 2
	}
	return 0
}

func countAliases(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	c := 0
	if n.Kind == yaml.AliasNode {
		c++
	}
	for _, ch := range n.Content {
		c += countAliases(ch)
	}
	return c
}

// ── yaml.Node helpers ──────────────────────────────────────────────────────────

func loadDoc(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s: not a YAML document", path)
	}
	return doc.Content[0], nil
}

func writeDoc(path string, root *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func newSeq() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"} }

func scalar(val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
}

// buildServers builds a `servers:` sequence node from a list of {url, description}.
func buildServers(defs []serverDef) *yaml.Node {
	seq := newSeq()
	for _, d := range defs {
		m := newMap()
		appendKV(m, "url", scalar(d.url))
		appendKV(m, "description", scalar(d.desc))
		seq.Content = append(seq.Content, m)
	}
	return seq
}

func appendKV(m *yaml.Node, key string, val *yaml.Node) {
	m.Content = append(m.Content, scalar(key), val)
}
func appendKVNode(m *yaml.Node, key string, val *yaml.Node) { appendKV(m, key, val) }

// mapGet returns the value node for key in a mapping node, or nil.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapScalar sets (or appends) key=val as a scalar in a mapping node.
func setMapScalar(m *yaml.Node, key, val string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = scalar(val)
			return
		}
	}
	appendKV(m, key, scalar(val))
}

func scalarChild(m *yaml.Node, key string) string {
	if v := mapGet(m, key); v != nil {
		return v.Value
	}
	return ""
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Anchor = ""
	c.Alias = nil
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, ch := range n.Content {
			c.Content[i] = cloneNode(ch)
		}
	}
	return &c
}

// expandAliases replaces every AliasNode with an expanded deep copy of its anchor
// target, recursively (targets may themselves contain aliases).
func expandAliases(n *yaml.Node) {
	if n == nil {
		return
	}
	for i, ch := range n.Content {
		if ch.Kind == yaml.AliasNode {
			clone := cloneNode(ch.Alias)
			expandAliases(clone)
			n.Content[i] = clone
		} else {
			expandAliases(ch)
		}
	}
}

func clearAnchors(n *yaml.Node) {
	if n == nil {
		return
	}
	n.Anchor = ""
	for _, ch := range n.Content {
		clearAnchors(ch)
	}
}

// collectRefs walks a node tree and records every #/components/<section>/<name>
// reference into out[section][name].
func collectRefs(n *yaml.Node, out map[string]map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "$ref" && v.Kind == yaml.ScalarNode && strings.HasPrefix(v.Value, "#/components/") {
				rest := strings.TrimPrefix(v.Value, "#/components/")
				parts := strings.SplitN(rest, "/", 2)
				if len(parts) == 2 {
					if out[parts[0]] == nil {
						out[parts[0]] = map[string]bool{}
					}
					out[parts[0]][parts[1]] = true
				}
			}
			collectRefs(v, out)
		}
		return
	}
	for _, ch := range n.Content {
		collectRefs(ch, out)
	}
}

// resolveClosure expands the ref set to a fixpoint: each referenced component may
// itself reference others.
func resolveClosure(components *yaml.Node, closure map[string]map[string]bool) {
	for {
		grew := false
		next := map[string]map[string]bool{}
		for section, names := range closure {
			secNode := mapGet(components, section)
			if secNode == nil {
				continue
			}
			for name := range names {
				comp := mapGet(secNode, name)
				if comp == nil {
					continue
				}
				collectRefs(comp, next)
			}
		}
		for section, names := range next {
			if closure[section] == nil {
				closure[section] = map[string]bool{}
			}
			for name := range names {
				if !closure[section][name] {
					closure[section][name] = true
					grew = true
				}
			}
		}
		if !grew {
			return
		}
	}
}

// buildComponents emits a components node carrying the closure subset, in the
// original section + key order, plus ALL securitySchemes.
func buildComponents(orig *yaml.Node, closure map[string]map[string]bool) *yaml.Node {
	out := newMap()
	for i := 0; i+1 < len(orig.Content); i += 2 {
		section := orig.Content[i].Value
		secNode := orig.Content[i+1]
		want := closure[section]
		includeAll := section == "securitySchemes"
		if !includeAll && len(want) == 0 {
			continue
		}
		secOut := newMap()
		for j := 0; j+1 < len(secNode.Content); j += 2 {
			name := secNode.Content[j].Value
			if includeAll || want[name] {
				appendKVNode(secOut, name, secNode.Content[j+1])
			}
		}
		if len(secOut.Content) > 0 {
			appendKVNode(out, section, secOut)
		}
	}
	return out
}

// filterTags returns the subset of the top-level tags sequence whose names are used
// in this file, or nil if none match.
func filterTags(tagsNode *yaml.Node, used map[string]bool) *yaml.Node {
	if tagsNode == nil || tagsNode.Kind != yaml.SequenceNode {
		return nil
	}
	out := newSeq()
	for _, e := range tagsNode.Content {
		if used[scalarChild(e, "name")] {
			out.Content = append(out.Content, cloneNode(e))
		}
	}
	if len(out.Content) == 0 {
		return nil
	}
	return out
}

func countSection(components *yaml.Node, section string) int {
	s := mapGet(components, section)
	if s == nil {
		return 0
	}
	return len(s.Content) / 2
}

// ── misc ─────────────────────────────────────────────────────────────────────

// findRepoRoot walks up from the working directory to the first ancestor that
// contains an api/openapi directory (the repo root). This is robust to the tool
// living in its own nested module.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, "api", "openapi")); err == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("api/openapi not found from working directory")
		}
		dir = parent
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func fatalIf(err error) {
	if err != nil {
		fatal(err)
	}
}
