package server

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/is-optimized/is-optimized/internal/scanner"
	"github.com/is-optimized/is-optimized/internal/store"
)

// A bounded projection of published public reports, not a history query.
// Only this document and at most directoryCapacity public report documents are read.
const directoryKey = "public_directory_v1"
const directoryCapacity = 24
const recentLimit = 6

var publicReportID = regexp.MustCompile(`^[a-f0-9]{36}$`)

type featuredSite struct{ Domain string }

// Curated links only. Adding a site never invents a report or a score.
var featuredSites = []featuredSite{{Domain: "sire.run"}, {Domain: "zati.agency"}}

type publicSite struct {
	ID          string    `json:"id"`
	Domain      string    `json:"domain"`
	CompletedAt time.Time `json:"completed_at"`
	Expires     time.Time `json:"expires"`
}
type publicDirectory struct {
	Sites []publicSite `json:"sites"`
}

func newerSite(a, b publicSite) bool {
	if a.CompletedAt.Equal(b.CompletedAt) {
		return a.ID > b.ID
	}
	return a.CompletedAt.After(b.CompletedAt)
}

// Called only after immutable report and latest-domain pointer publication.
// Retry-safe: the existing publishing recovery path can repeat the same update.
func (s *Server) indexPublicReport(ctx context.Context, snap Snapshot) error {
	return s.db.Update(ctx, directoryKey, func(raw json.RawMessage) (any, error) {
		candidate := publicSite{ID: snap.Report.ID, Domain: snap.Report.Domain, CompletedAt: snap.Report.CompletedAt, Expires: snap.Expires}
		var index publicDirectory
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &index); err != nil {
				return nil, err
			}
		}
		now := time.Now()
		sites := make([]publicSite, 0, directoryCapacity+1)
		for _, site := range index.Sites {
			if !site.Expires.After(now) {
				continue
			}
			if site.Domain == candidate.Domain {
				if newerSite(site, candidate) {
					candidate = site
				}
				continue
			}
			sites = append(sites, site)
		}
		if candidate.Expires.After(now) {
			sites = append(sites, candidate)
		}
		sort.Slice(sites, func(i, j int) bool { return newerSite(sites[i], sites[j]) })
		if len(sites) > directoryCapacity {
			sites = sites[:directoryCapacity]
		}
		return publicDirectory{Sites: sites}, nil
	})
}

// Revalidate the projection against published immutable snapshots. Attempts,
// unpublished evidence, expired reports, and contact records are never listed.
func (s *Server) recentPublicReports(ctx context.Context) ([]publicSite, error) {
	var index publicDirectory
	if err := s.db.Get(ctx, directoryKey, &index); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(index.Sites) > directoryCapacity {
		index.Sites = index.Sites[:directoryCapacity]
	}
	sort.Slice(index.Sites, func(i, j int) bool { return newerSite(index.Sites[i], index.Sites[j]) })
	now := time.Now()
	result := make([]publicSite, 0, recentLimit)
	seen := map[string]bool{}
	for _, site := range index.Sites {
		if !publicReportID.MatchString(site.ID) || !site.Expires.After(now) || seen[site.Domain] {
			continue
		}
		domain, err := scanner.NormalizeDomain(site.Domain)
		if err != nil || domain != site.Domain {
			continue
		}
		var snap Snapshot
		if err = s.db.Get(ctx, "report_"+site.ID, &snap); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if snap.Report.ID != site.ID || snap.Report.Domain != site.Domain || !snap.Report.CompletedAt.Equal(site.CompletedAt) || snap.Report.CompletedAt.IsZero() || snap.Report.CompletedAt.After(now) || !snap.Expires.After(now) {
			continue
		}
		site.Expires = snap.Expires
		result = append(result, site)
		seen[site.Domain] = true
		if len(result) == recentLimit {
			break
		}
	}
	return result, nil
}
