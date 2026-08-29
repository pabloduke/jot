package jot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// OKF v0.2 vocabulary. See https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md
const okfVersion = "0.2"

// Status values defined by OKF §Freshness.
const (
	StatusDraft      = "draft"
	StatusStable     = "stable"
	StatusDeprecated = "deprecated"
)

// Trust tiers are derived from the verified list, never stored.
const (
	TrustUnverified = "unverified"
	TrustMachine    = "machine-confirmed"
	TrustHuman      = "human-reviewed"
)

// Source is one entry of the OKF provenance family.
type Source struct {
	ID           string `yaml:"id,omitempty" json:"id,omitempty"`
	Resource     string `yaml:"resource" json:"resource"`
	Title        string `yaml:"title,omitempty" json:"title,omitempty"`
	Author       string `yaml:"author,omitempty" json:"author,omitempty"`
	UsageCount   int    `yaml:"usage_count,omitempty" json:"usage_count,omitempty"`
	LastModified string `yaml:"last_modified,omitempty" json:"last_modified,omitempty"`
}

// Attestation is the {by, at} actor mapping used by generated and verified.
type Attestation struct {
	By string `yaml:"by" json:"by"`
	At string `yaml:"at" json:"at"`
}

// IsHuman reports whether the actor is a person, which promotes the trust tier.
func (a Attestation) IsHuman() bool { return strings.HasPrefix(a.By, "human:") }

// ActorProcess is the actor string the CLI itself records.
const ActorProcess = "process:jot"

func decodeFrontmatter(text string) (map[string]any, error) {
	raw := map[string]any{}
	if strings.TrimSpace(text) == "" {
		return raw, nil
	}
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	normalized, _ := normalizeYAML(raw).(map[string]any)
	if normalized == nil {
		normalized = map[string]any{}
	}
	return normalized, nil
}

// normalizeYAML makes decoded frontmatter round-trippable. YAML resolves
// unquoted ISO 8601 scalars to time.Time, which would otherwise reach callers
// as Go's default time formatting and break every RFC 3339 check; nested maps
// can also decode with non-string keys.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeYAML(val)
		}
		return out
	default:
		return v
	}
}

func fmString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func fmStrings(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		// Tolerate a comma-joined scalar written by hand.
		var out []string
		for _, part := range strings.Split(t, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func remarshal(v any, target any) bool {
	b, err := yaml.Marshal(v)
	if err != nil {
		return false
	}
	return yaml.Unmarshal(b, target) == nil
}

func fmAttestation(m map[string]any, key string) *Attestation {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	var a Attestation
	if !remarshal(v, &a) || (a.By == "" && a.At == "") {
		return nil
	}
	return &a
}

// fmAttestations accepts either a single mapping or a list, per OKF §verified.
func fmAttestations(m map[string]any, key string) []Attestation {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	var list []Attestation
	if remarshal(v, &list) && len(list) > 0 {
		return list
	}
	var single Attestation
	if remarshal(v, &single) && (single.By != "" || single.At != "") {
		return []Attestation{single}
	}
	return nil
}

func fmSources(m map[string]any, key string) []Source {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	var list []Source
	if remarshal(v, &list) {
		return list
	}
	return nil
}

// trustTier derives the OKF consumer-side trust level from verified entries.
func trustTier(verified []Attestation) string {
	if len(verified) == 0 {
		return TrustUnverified
	}
	for _, a := range verified {
		if a.IsHuman() {
			return TrustHuman
		}
	}
	return TrustMachine
}

// frontmatterOrder is the emission order jot uses for keys it authors. Any key
// not listed is emitted afterwards in sorted order so unknown keys round-trip.
var frontmatterOrder = []string{
	"type", "title", "description", "resource", "tags",
	"status", "stale_after", "generated", "verified",
	"sources", "usage_window",
	"capture_id", "source_kind", "source_url", "timestamp",
}

func marshalFrontmatter(m map[string]any) (string, error) {
	seen := map[string]bool{}
	keys := make([]string, 0, len(m))
	for _, k := range frontmatterOrder {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	var out strings.Builder
	for _, k := range keys {
		b, err := yaml.Marshal(map[string]any{k: m[k]})
		if err != nil {
			return "", err
		}
		out.Write(b)
	}
	return out.String(), nil
}
