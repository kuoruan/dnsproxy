// Package geosite provides functionality to load and match domains against
// v2fly geosite.dat files.
//
// The geosite.dat file contains domain lists organized by categories (codes).
// Each category can include domains with different match types: full match,
// root domain match, plain substring match, and regex match.
package geosite

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

// Loader loads and manages geosite.dat files.
type Loader struct {
	logger   *slog.Logger
	sites    map[string]*routercommon.GeoSite
	fullList *routercommon.GeoSiteList
	filePath string
}

// NewLoader creates a new geosite loader.
// It looks for geosite.dat in the specified config directory.
func NewLoader(configDir string, logger *slog.Logger) (*Loader, error) {
	filePath := filepath.Join(configDir, "geosite.dat")
	return NewLoaderFromPath(filePath, logger)
}

// NewLoaderFromPath creates a new geosite loader from the specified file path.
func NewLoaderFromPath(filePath string, logger *slog.Logger) (*Loader, error) {
	if logger == nil {
		logger = slog.Default()
	}

	l := &Loader{
		logger:   logger,
		sites:    make(map[string]*routercommon.GeoSite),
		filePath: filePath,
	}

	return l, nil
}

// Load loads the geosite.dat file.
// If codes is nil, all sites are loaded.
func (l *Loader) Load(codes []string) error {
	if l.fullList == nil {
		data, err := os.ReadFile(l.filePath)
		if err != nil {
			if os.IsNotExist(err) {
				l.logger.Info("geosite.dat not found, geosite rules will be skipped", "path", l.filePath)
				return nil
			}
			return err
		}

		var geoSiteList routercommon.GeoSiteList
		if err := proto.Unmarshal(data, &geoSiteList); err != nil {
			return fmt.Errorf("unmarshalling geosite data: %w", err)
		}
		l.fullList = &geoSiteList
		l.logger.Info("geosite.dat loaded successfully", "path", l.filePath)
	}

	var interested map[string]bool
	if codes != nil {
		interested = make(map[string]bool)
		for _, c := range codes {
			interested[strings.ToUpper(c)] = true
		}
	}

	count := 0
	for _, site := range l.fullList.GetEntry() {
		code := strings.ToUpper(site.GetCountryCode())
		if interested != nil && !interested[code] {
			continue
		}

		if _, exists := l.sites[code]; exists {
			continue
		}

		l.sites[code] = site
		count++
	}

	if count > 0 {
		l.logger.Debug("loaded geosite codes", "count", count)
	}

	return nil
}

// GetSite retrieves geosite data for a specific code.
// The code lookup is case-insensitive.
func (l *Loader) GetSite(code string) (*routercommon.GeoSite, bool) {
	site, ok := l.sites[strings.ToUpper(code)]
	return site, ok
}

// GetSites retrieves multiple geosite data entries at once.
// Returns a map of successful loads and a slice of codes that weren't found.
//
// This is more efficient than calling GetSite multiple times when you need
// multiple geosite data entries, as it only reads the file once.
//
// Example:
//
//	sites, notFound := loader.GetSites([]string{"cn", "google", "twitter"})
//	// sites might contain "cn" and "google"
//	// notFound would be ["twitter"] if it doesn't exist
func (l *Loader) GetSites(codes []string) (sites map[string]*routercommon.GeoSite, notFound []string) {
	sites = make(map[string]*routercommon.GeoSite, len(codes))
	notFound = make([]string, 0)

	for _, code := range codes {
		upperCode := strings.ToUpper(code)
		if site, ok := l.sites[upperCode]; ok {
			sites[code] = site
		} else {
			notFound = append(notFound, code)
		}
	}

	return sites, notFound
}

// IsLoaded returns whether geosite.dat has been successfully loaded.
func (l *Loader) IsLoaded() bool {
	return len(l.sites) > 0
}
