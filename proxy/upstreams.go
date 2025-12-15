package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/AdguardTeam/dnsproxy/proxy/geosite"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/container"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
)

// UnqualifiedNames is a key for [UpstreamConfig.DomainReservedUpstreams] map to
// specify the upstreams only used for resolving domain names consisting of a
// single label.
const UnqualifiedNames = "unqualified_names"

// UpstreamConfig maps domain names to upstreams.
type UpstreamConfig struct {
	// DomainReservedUpstreams maps the domains to the upstreams.
	DomainReservedUpstreams map[string][]upstream.Upstream

	// SpecifiedDomainUpstreams maps the specific domain names to the upstreams.
	SpecifiedDomainUpstreams map[string][]upstream.Upstream

	// SubdomainExclusions is set of domains with subdomains exclusions.
	SubdomainExclusions *container.MapSet[string]

	// Upstreams is a list of default upstreams.
	Upstreams []upstream.Upstream

	// GeositeUpstreams is a list of geosite-based upstreams.
	GeositeUpstreams []*geositeUpstream
}

// GeositeMatcher defines the interface for geosite domain matchers.
// Implementations include single Matcher and CompositeMatcher (for OR logic).
type GeositeMatcher interface {
	Match(domain string) bool
}

// geositeCodeInfo encapsulates parsed information about a geosite code.
type geositeCodeInfo struct {
	// originalCode is the original geosite code (may contain negation marker)
	// Example: "geolocation-!cn"
	originalCode string

	// actualCode is the actual code used for loading (negation marker removed)
	// Example: "geolocation-cn" (from "geolocation-!cn")
	actualCode string

	// negate indicates whether this is a negation rule
	negate bool
}

// parseGeositeCode parses a geosite code and extracts negation information.
// Supports negation format: prefix-!suffix (e.g., "geolocation-!cn")
func parseGeositeCode(code string) geositeCodeInfo {
	info := geositeCodeInfo{
		originalCode: code,
		actualCode:   code,
		negate:       false,
	}

	// Support all !code formats, e.g., geolocation-!cn, category-!ads
	if idx := strings.Index(code, "-!"); idx != -1 {
		// Extract the actual code after removing !
		// For "geolocation-!cn", extract "geolocation-cn"
		info.actualCode = code[:idx+1] + code[idx+2:]
		info.negate = true
	}

	return info
}

// geositeUpstream represents a geosite rule and its corresponding upstreams.
type geositeUpstream struct {
	matcher   GeositeMatcher
	upstreams []upstream.Upstream
}

// type check
var _ io.Closer = (*UpstreamConfig)(nil)

// ParseUpstreamsConfig returns an UpstreamConfig and nil error if the upstream
// configuration is valid.  Otherwise returns a partially filled UpstreamConfig
// and wrapped error containing lines with errors.  It also skips empty lines
// and comments (lines starting with "#").
//
// # Simple upstreams
//
// Single upstream per line.  For example:
//
//	1.2.3.4
//	3.4.5.6
//
// # Domain specific upstreams
//
//   - reserved upstreams: [/domain1/../domainN/]<upstreamString>
//   - subdomains only upstreams: [/*.domain1/../*.domainN]<upstreamString>
//
// Where <upstreamString> is one or many upstreams separated by space (e.g.
// `1.1.1.1` or `1.1.1.1 2.2.2.2`).
//
// More specific domains take priority over less specific domains.  To exclude
// more specific domains from reserved upstreams querying you should use the
// following syntax:
//
//	[/domain1/../domainN/]#
//
// So the following config:
//
//	[/host.com/]1.2.3.4
//	[/www.host.com/]2.3.4.5"
//	[/maps.host.com/news.host.com/]#
//	3.4.5.6
//
// will send queries for *.host.com to 1.2.3.4.  Except for *.www.host.com,
// which will go to 2.3.4.5.  And *.maps.host.com or *.news.host.com, which
// will go to default server 3.4.5.6 with all other domains.
//
// To exclude top level domain from reserved upstreams querying you could use
// the following:
//
//	'[/*.domain.com/]<upstreamString>'
//
// So the following config:
//
//	[/*.domain.com/]1.2.3.4
//	3.4.5.6
//
// will send queries for all subdomains *.domain.com to 1.2.3.4, but domain.com
// query will be sent to default server 3.4.5.6 as every other query.
//
// # Geosite upstreams
//
// If opts.GeositeDir is provided, you can use geosite rules:
//
//	[/geosite:google/]8.8.8.8
//	[/geosite:geolocation-!cn/]1.1.1.1
//
// TODO(e.burkov):  Consider supporting multiple upstreams in a single line for
// default upstream syntax.
func ParseUpstreamsConfig(
	lines []string,
	opts *upstream.Options,
) (conf *UpstreamConfig, err error) {
	if opts == nil {
		opts = &upstream.Options{}
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Initialize geosite loader if path is provided
	var geositeLoader *geosite.Loader
	if opts.GeositeDir != "" {
		geositeLoader, err = geosite.NewLoader(opts.GeositeDir, opts.Logger)
		if err != nil {
			opts.Logger.Warn("failed to initialize geosite loader", slogutil.KeyError, err)
			// Continue without geosite support, don't return error
			geositeLoader = nil
		}
	}

	p := &configParser{
		options:                  opts,
		logger:                   opts.Logger,
		upstreamsIndex:           map[string]upstream.Upstream{},
		domainReservedUpstreams:  map[string][]upstream.Upstream{},
		specifiedDomainUpstreams: map[string][]upstream.Upstream{},
		subdomainsOnlyUpstreams:  map[string][]upstream.Upstream{},
		subdomainsOnlyExclusions: container.NewMapSet[string](),
		geositeLoader:            geositeLoader,
		geositeUpstreams:         []*geositeUpstream{},
	}

	return p.parse(lines)
}

// ParseError is an error which contains an index of the line of the upstream
// list.
type ParseError struct {
	// err is the original error.
	err error

	// Idx is an index of the lines.  See [ParseUpstreamsConfig].
	Idx int
}

// type check
var _ error = (*ParseError)(nil)

// Error implements the [error] interface for *ParseError.
func (e *ParseError) Error() (msg string) {
	return fmt.Sprintf("parsing error at index %d: %s", e.Idx, e.err)
}

// type check
var _ errors.Wrapper = (*ParseError)(nil)

// Unwrap implements the [errors.Wrapper] interface for *ParseError.
func (e *ParseError) Unwrap() (unwrapped error) { return e.err }

// configParser collects the results of parsing an upstream config.
type configParser struct {
	// options contains upstream properties.
	options *upstream.Options

	// logger is used for logging during parsing.  It's never nil.
	logger *slog.Logger

	// upstreamsIndex is used to avoid creating duplicates of upstreams.
	upstreamsIndex map[string]upstream.Upstream

	// domainReservedUpstreams is a map of reserved domains and lists of
	// corresponding upstreams.
	domainReservedUpstreams map[string][]upstream.Upstream

	// specifiedDomainUpstreams is a map of excluded domains and lists of
	// corresponding upstreams.
	specifiedDomainUpstreams map[string][]upstream.Upstream

	// subdomainsOnlyUpstreams is a map of wildcard subdomains and lists of
	// corresponding upstreams.
	subdomainsOnlyUpstreams map[string][]upstream.Upstream

	// subdomainsOnlyExclusions is set of domains with subdomains exclusions.
	subdomainsOnlyExclusions *container.MapSet[string]

	// upstreams is a list of default upstreams.
	upstreams []upstream.Upstream

	// geositeLoader is used to load geosite data.
	geositeLoader *geosite.Loader

	// geositeUpstreams is a list of geosite-based upstreams.
	geositeUpstreams []*geositeUpstream
}

// parse returns UpstreamConfig and error if upstreams configuration is invalid.
func (p *configParser) parse(lines []string) (c *UpstreamConfig, err error) {
	var errs []error
	for i, l := range lines {
		if err = p.parseLine(i, l); err != nil {
			errs = append(errs, &ParseError{Idx: i, err: err})
		}
	}

	for host, ups := range p.subdomainsOnlyUpstreams {
		// Rewrite ups for wildcard subdomains to remove upper level domains
		// specs.
		p.domainReservedUpstreams[host] = ups
	}

	return &UpstreamConfig{
		Upstreams:                p.upstreams,
		DomainReservedUpstreams:  p.domainReservedUpstreams,
		SpecifiedDomainUpstreams: p.specifiedDomainUpstreams,
		SubdomainExclusions:      p.subdomainsOnlyExclusions,
		GeositeUpstreams:         p.geositeUpstreams,
	}, errors.Join(errs...)
}

// parseLine returns an error if upstream configuration line is invalid.
func (p *configParser) parseLine(idx int, confLine string) (err error) {
	if len(confLine) == 0 || confLine[0] == '#' {
		return nil
	}

	upstreams, domains, geositeCodes, err := splitConfigLine(confLine)
	if err != nil {
		// Don't wrap the error since it's informative enough as is.
		return err
	}

	// Handle geosite rules - process all codes from the same line together
	if len(geositeCodes) > 0 {
		if err = p.specifyGeositeUpstreams(geositeCodes, upstreams, idx); err != nil {
			return err
		}

		// If there are geosite codes and no domains, we are done (geosite-only rule)
		if len(domains) == 0 {
			return nil
		}
	}

	if upstreams[0] == "#" && len(domains) > 0 {
		p.excludeFromReserved(domains)

		return nil
	}

	for _, u := range upstreams {
		err = p.specifyUpstream(domains, u, idx)
		if err != nil {
			// Don't wrap the error since it's informative enough as is.
			return err
		}
	}

	return nil
}

// splitConfigLine parses upstream configuration line and returns list upstream
// addresses (one or many), list of domains for which this upstream is reserved
// (may be nil), and geosite codes if applicable.  It returns an error if the
// upstream format is incorrect.
func splitConfigLine(confLine string) (
	upstreams []string,
	domains []string,
	geositeCodes []string,
	err error,
) {
	if !strings.HasPrefix(confLine, "[/") {
		return []string{confLine}, nil, nil, nil
	}

	domainsLine, upstreamsLine, found := strings.Cut(confLine[len("[/"):], "/]")
	if !found || upstreamsLine == "" {
		return nil, nil, nil, errors.Error("wrong upstream format")
	}

	// split domains list
	for _, confHost := range strings.Split(domainsLine, "/") {
		if confHost == "" {
			// empty domain specification means `unqualified names only`
			domains = append(domains, UnqualifiedNames)

			continue
		}

		// Check if this is a geosite rule
		if strings.HasPrefix(confHost, "geosite:") {
			code := strings.TrimPrefix(confHost, "geosite:")
			if code != "" {
				geositeCodes = append(geositeCodes, code)
			}
			continue
		}

		host := strings.TrimPrefix(confHost, "*.")
		if err = netutil.ValidateDomainName(host); err != nil {
			return nil, nil, nil, err
		}

		domains = append(domains, strings.ToLower(confHost+"."))
	}

	return strings.Fields(upstreamsLine), domains, geositeCodes, nil
}

// specifyUpstream specifies the upstream for domains.
func (p *configParser) specifyUpstream(domains []string, u string, idx int) (err error) {
	dnsUpstream, ok := p.upstreamsIndex[u]
	// TODO(e.burkov):  Improve identifying duplicate upstreams.
	if !ok {
		// create an upstream
		dnsUpstream, err = upstream.AddressToUpstream(u, p.options.Clone())
		if err != nil {
			return fmt.Errorf("cannot prepare the upstream: %s", err)
		}

		// save to the index
		p.upstreamsIndex[u] = dnsUpstream
	}

	addr := dnsUpstream.Address()
	if len(domains) == 0 {
		// TODO(s.chzhen):  Handle duplicates.
		p.upstreams = append(p.upstreams, dnsUpstream)

		// TODO(s.chzhen):  Logs without index.
		p.logger.Debug("set upstream", "idx", idx, "addr", addr)
	} else {
		p.includeToReserved(dnsUpstream, domains)

		p.logger.Debug(
			"upstream is reserved",
			"idx", idx,
			"addr", addr,
			"domains_num", len(domains),
		)
	}

	return nil
}

// specifyGeositeUpstreams specifies the upstream for multiple geosite rules from the same line.
// All codes from the same configuration line are processed together and create a single
// geositeUpstream entry with a composite matcher.
func (p *configParser) specifyGeositeUpstreams(
	geositeCodes []string,
	upstreamAddrs []string,
	lineIdx int,
) (err error) {
	if p.geositeLoader == nil {
		p.logger.Warn(
			"geosite rules skipped: loader not initialized",
			"line", lineIdx,
			"codes", geositeCodes,
		)
		return nil
	}

	if len(geositeCodes) == 0 {
		return nil
	}

	// Create matchers for all codes
	matchers := p.createGeositeMatchers(geositeCodes, lineIdx)
	if len(matchers) == 0 {
		p.logger.Warn(
			"no valid geosite matchers created",
			"line", lineIdx,
			"codes", geositeCodes,
		)
		return nil
	}

	// Create upstream list
	upstreams, err := p.createUpstreams(upstreamAddrs)
	if err != nil {
		return fmt.Errorf("failed to create upstreams: %w", err)
	}

	// Create the appropriate matcher based on number of matchers
	finalMatcher := p.buildFinalMatcher(matchers)

	// Add to geosite upstreams list
	p.geositeUpstreams = append(p.geositeUpstreams, &geositeUpstream{
		matcher:   finalMatcher,
		upstreams: upstreams,
	})

	p.logger.Debug(
		"geosite upstream configured",
		"line", lineIdx,
		"codes", geositeCodes,
		"matchers", len(matchers),
		"upstreams", len(upstreams),
	)

	return nil
}

// createGeositeMatchers creates matchers for all geosite codes
func (p *configParser) createGeositeMatchers(codes []string, lineIdx int) []*geosite.Matcher {
	// Parse all codes to extract actual codes (removing negation markers)
	codeInfos := make([]geositeCodeInfo, 0, len(codes))
	actualCodes := make([]string, 0, len(codes))

	for _, code := range codes {
		info := parseGeositeCode(code)
		codeInfos = append(codeInfos, info)
		actualCodes = append(actualCodes, info.actualCode)
	}

	// Batch load all geosite data
	if err := p.geositeLoader.Load(actualCodes); err != nil {
		p.logger.Warn(
			"failed to load geosite data",
			"line", lineIdx,
			"error", err,
		)
		// Continue with whatever was loaded
	}

	// Batch get all sites
	sites, notFound := p.geositeLoader.GetSites(actualCodes)

	// Log not found codes
	if len(notFound) > 0 {
		p.logger.Warn(
			"some geosite codes not found",
			"line", lineIdx,
			"codes", notFound,
		)
	}

	// Create matchers for successfully loaded sites
	matchers := make([]*geosite.Matcher, 0, len(sites))
	for _, info := range codeInfos {
		if site, ok := sites[info.actualCode]; ok {
			matcher := geosite.NewMatcher(site, info.originalCode, info.negate)
			matchers = append(matchers, matcher)
		}
	}

	return matchers
}

// createUpstreams creates upstream instances from addresses
func (p *configParser) createUpstreams(addrs []string) ([]upstream.Upstream, error) {
	upstreams := make([]upstream.Upstream, 0, len(addrs))

	for _, addr := range addrs {
		u, err := p.getOrCreateUpstream(addr)
		if err != nil {
			return nil, fmt.Errorf("upstream %q: %w", addr, err)
		}
		upstreams = append(upstreams, u)
	}

	return upstreams, nil
}

// getOrCreateUpstream gets an existing upstream or creates a new one
func (p *configParser) getOrCreateUpstream(addr string) (upstream.Upstream, error) {
	if u, ok := p.upstreamsIndex[addr]; ok {
		return u, nil
	}

	u, err := upstream.AddressToUpstream(addr, p.options.Clone())
	if err != nil {
		return nil, err
	}

	p.upstreamsIndex[addr] = u
	return u, nil
}

// buildFinalMatcher creates the final matcher from a list of matchers
func (p *configParser) buildFinalMatcher(matchers []*geosite.Matcher) GeositeMatcher {
	if len(matchers) == 1 {
		// Single matcher, use it directly
		return matchers[0]
	}
	// Multiple matchers, use composite matcher (OR logic)
	return geosite.NewCompositeMatcher(matchers)
}

// excludeFromReserved excludes more specific domains from reserved upstreams
// querying.
func (p *configParser) excludeFromReserved(domains []string) {
	for _, host := range domains {
		if trimmed := strings.TrimPrefix(host, "*."); trimmed != host {
			p.subdomainsOnlyExclusions.Add(trimmed)
			p.subdomainsOnlyUpstreams[trimmed] = nil

			continue
		}

		p.domainReservedUpstreams[host] = nil
		p.specifiedDomainUpstreams[host] = nil
	}
}

// includeToReserved includes domains to reserved upstreams querying.
func (p *configParser) includeToReserved(dnsUpstream upstream.Upstream, domains []string) {
	for _, host := range domains {
		if strings.HasPrefix(host, "*.") {
			host = host[len("*."):]

			p.subdomainsOnlyExclusions.Add(host)
			p.logger.Debug("domain is added to exclusions list", "domain", host)

			p.subdomainsOnlyUpstreams[host] = append(p.subdomainsOnlyUpstreams[host], dnsUpstream)
		} else {
			p.specifiedDomainUpstreams[host] = append(p.specifiedDomainUpstreams[host], dnsUpstream)
		}

		p.domainReservedUpstreams[host] = append(p.domainReservedUpstreams[host], dnsUpstream)
	}
}

// validate returns an error if the upstreams aren't configured properly.  c
// considered valid if it contains at least a single default upstream.  Empty c
// causes [upstream.ErrNoUpstreams].
func (uc *UpstreamConfig) validate() (err error) {
	switch {
	case uc == nil:
		return errors.ErrNoValue
	case len(uc.Upstreams) > 0:
		return nil
	case len(uc.DomainReservedUpstreams) == 0 && len(uc.SpecifiedDomainUpstreams) == 0:
		return upstream.ErrNoUpstreams
	default:
		return fmt.Errorf("default upstreams: %w", errors.ErrNoValue)
	}
}

// ValidatePrivateConfig returns an error if uc isn't valid, or, treated as
// private upstreams configuration, contains specifications for invalid domains.
func ValidatePrivateConfig(uc *UpstreamConfig, privateSubnets netutil.SubnetSet) (err error) {
	if err = uc.validate(); err != nil {
		// Don't wrap the error since it's informative enough as is.
		return err
	}

	var errs []error
	for _, domain := range slices.Sorted(maps.Keys(uc.DomainReservedUpstreams)) {
		pref, extErr := netutil.ExtractReversedAddr(domain)
		switch {
		case extErr != nil:
			// Don't wrap the error since it's informative enough as is.
			errs = append(errs, extErr)
		case pref.Bits() == 0:
			// Allow private subnets for subdomains of the root domain.
		case !privateSubnets.Contains(pref.Addr()):
			errs = append(errs, fmt.Errorf("reversed subnet in %q is not private", domain))
		default:
			// Go on.
		}
	}

	return errors.Join(errs...)
}

// getUpstreamsForDomain returns the upstreams specified for resolving fqdn.  It
// always returns the default set of upstreams if the domain is not reserved for
// any other upstreams.
//
// More specific domains take priority over less specific ones.  For example, if
// the upstreams specified for the following domains:
//
//   - host.com
//   - www.host.com
//
// The request for mail.host.com will be resolved using the upstreams specified
// for host.com.
func (uc *UpstreamConfig) getUpstreamsForDomain(fqdn string) (ups []upstream.Upstream) {
	// 1. First check geosite rules (highest priority)
	if len(uc.GeositeUpstreams) > 0 {
		for _, gu := range uc.GeositeUpstreams {
			if gu.matcher.Match(fqdn) {
				return gu.upstreams
			}
		}
	}

	// 2. Then check domain-specific configuration
	if len(uc.DomainReservedUpstreams) == 0 {
		return uc.Upstreams
	}

	fqdn = strings.ToLower(fqdn)
	if uc.SubdomainExclusions.Has(fqdn) {
		return uc.lookupSubdomainExclusion(fqdn)
	}

	ups, ok := uc.lookupUpstreams(fqdn)
	if ok {
		return ups
	}

	if _, fqdn, _ = strings.Cut(fqdn, "."); fqdn == "" {
		fqdn = UnqualifiedNames
	}

	for fqdn != "" {
		if ups, ok = uc.lookupUpstreams(fqdn); ok {
			return ups
		}

		_, fqdn, _ = strings.Cut(fqdn, ".")
	}

	return uc.Upstreams
}

// getUpstreamsForDS is like [getUpstreamsForDomain], but intended for DS
// queries only, so that it matches fqdn without the first label.
//
// A DS RRset SHOULD be present at a delegation point when the child zone is
// signed.  The DS RRset MAY contain multiple records, each referencing a public
// key in the child zone used to verify the RRSIGs in that zone.  All DS RRsets
// in a zone MUST be signed, and DS RRsets MUST NOT appear at a zone's apex.
//
// See https://datatracker.ietf.org/doc/html/rfc4035#section-2.4
func (uc *UpstreamConfig) getUpstreamsForDS(fqdn string) (ups []upstream.Upstream) {
	_, fqdn, _ = strings.Cut(fqdn, ".")
	if fqdn == "" {
		return uc.Upstreams
	}

	return uc.getUpstreamsForDomain(fqdn)
}

// lookupSubdomainExclusion returns upstreams for the host from subdomain
// exclusions list.
func (uc *UpstreamConfig) lookupSubdomainExclusion(host string) (u []upstream.Upstream) {
	ups, ok := uc.SpecifiedDomainUpstreams[host]
	if ok && len(ups) > 0 {
		return ups
	}

	// Check if there is a spec for upper level domain.
	h := strings.SplitAfterN(host, ".", 2)
	ups, ok = uc.DomainReservedUpstreams[h[1]]
	if ok && len(ups) > 0 {
		return ups
	}

	return uc.Upstreams
}

// lookupUpstreams returns upstreams for a domain name.  It returns default
// upstream list for domain name excluded by domain reserved upstreams.
func (uc *UpstreamConfig) lookupUpstreams(name string) (ups []upstream.Upstream, ok bool) {
	ups, ok = uc.DomainReservedUpstreams[name]
	if !ok {
		return ups, false
	}

	if len(ups) == 0 {
		// The domain has been excluded from reserved upstreams querying.
		ups = uc.Upstreams
	}

	return ups, true
}

// Close implements the io.Closer interface for *UpstreamConfig.
func (uc *UpstreamConfig) Close() (err error) {
	closeErrs := closeAll(nil, uc.Upstreams...)

	for _, specUps := range []map[string][]upstream.Upstream{
		uc.DomainReservedUpstreams,
		uc.SpecifiedDomainUpstreams,
	} {
		domains := make([]string, 0, len(specUps))
		for domain := range specUps {
			domains = append(domains, domain)
		}

		slices.SortStableFunc(domains, strings.Compare)

		for _, domain := range domains {
			closeErrs = closeAll(closeErrs, specUps[domain]...)
		}
	}

	if len(closeErrs) > 0 {
		return fmt.Errorf("failed to close some upstreams: %w", errors.Join(closeErrs...))
	}

	return nil
}
