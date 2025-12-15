package geosite

// CompositeMatcher combines multiple Matchers with OR logic.
// Returns true when a domain matches any of the matchers.
//
// Use case: When a config line contains multiple geosite codes, for example:
//   [/geosite:google/geosite:cn/]8.8.8.8
// A CompositeMatcher is created to match domains from both google and cn.
type CompositeMatcher struct {
	matchers []*Matcher
	codes    []string
}

// NewCompositeMatcher creates a composite matcher.
func NewCompositeMatcher(matchers []*Matcher) *CompositeMatcher {
	codes := make([]string, 0, len(matchers))
	for _, m := range matchers {
		codes = append(codes, m.Code())
	}

	return &CompositeMatcher{
		matchers: matchers,
		codes:    codes,
	}
}

// Match checks if the domain matches any of the matchers (OR logic).
func (cm *CompositeMatcher) Match(domain string) bool {
	for _, m := range cm.matchers {
		if m.Match(domain) {
			return true
		}
	}
	return false
}

// Codes returns all geosite codes.
func (cm *CompositeMatcher) Codes() []string {
	return cm.codes
}
