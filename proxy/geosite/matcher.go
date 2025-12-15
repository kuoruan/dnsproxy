// Package geosite provides functionality to load and match domains against
// v2fly geosite.dat files.
package geosite

import (
	"regexp"
	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
)

// Matcher matches domains against a geosite.
type Matcher struct {
	site   *routercommon.GeoSite
	code   string
	negate bool // Whether this is a negation rule (e.g., geolocation-!cn)

	// Lookup tables for optimized matching performance
	fullDomains map[string]struct{}
	rootDomains map[string]struct{}

	regexCache map[int]*regexp.Regexp
}

// NewMatcher creates a new geosite matcher.
func NewMatcher(site *routercommon.GeoSite, code string, negate bool) *Matcher {
	m := &Matcher{
		site:        site,
		code:        code,
		negate:      negate,
		fullDomains: make(map[string]struct{}),
		rootDomains: make(map[string]struct{}),
		regexCache:  make(map[int]*regexp.Regexp),
	}

	if site != nil {
		for _, d := range site.GetDomain() {
			switch d.GetType() {
			case routercommon.Domain_Full:
				domain := strings.ToLower(d.GetValue())
				if !strings.HasSuffix(domain, ".") {
					domain += "."
				}
				m.fullDomains[domain] = struct{}{}
			case routercommon.Domain_RootDomain:
				domain := strings.ToLower(d.GetValue())
				if !strings.HasSuffix(domain, ".") {
					domain += "."
				}
				m.rootDomains[domain] = struct{}{}
			}
		}
	}

	return m
}

// Match checks if the domain matches this geosite.
func (m *Matcher) Match(domain string) bool {
	domain = strings.ToLower(domain)
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}

	matched := m.matchDomain(domain)

	if m.negate {
		return !matched
	}
	return matched
}

// matchDomain performs the actual domain matching.
func (m *Matcher) matchDomain(domain string) bool {
	if m.site == nil {
		return false
	}

	if _, ok := m.fullDomains[domain]; ok {
		return true
	}

	parts := strings.Split(strings.TrimSuffix(domain, "."), ".")
	for i := 0; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".") + "."
		if _, ok := m.rootDomains[suffix]; ok {
			return true
		}
	}

	for i, d := range m.site.GetDomain() {
		switch d.GetType() {
		case routercommon.Domain_Plain:
			if strings.Contains(domain, strings.ToLower(d.GetValue())) {
				return true
			}

		case routercommon.Domain_Regex:
			re := m.getRegex(i, d.GetValue())
			if re != nil && re.MatchString(domain) {
				return true
			}
		}
	}

	return false
}

// getRegex retrieves or compiles a regex pattern.
func (m *Matcher) getRegex(index int, pattern string) *regexp.Regexp {
	if re, ok := m.regexCache[index]; ok {
		return re
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	m.regexCache[index] = re
	return re
}

// Code returns the geosite code.
func (m *Matcher) Code() string {
	return m.code
}

// IsNegate returns whether this is a negation rule.
func (m *Matcher) IsNegate() bool {
	return m.negate
}
