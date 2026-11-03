package api

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type countryTotal struct {
	Code  string `json:"country_code"`
	Name  string `json:"country_name"`
	Count int64  `json:"count"`
}

func (a *App) countryRanking(c *gin.Context) {
	ctx := c.Request.Context()
	a.dashboardMu.Lock()
	data, valid := a.cachedDashboard("country-ranking")
	epoch := a.countryEpoch
	a.dashboardMu.Unlock()
	if valid {
		ok(c, data)
		return
	}
	pending := a.countryFlights.DoChan(strconv.FormatUint(epoch, 10), func() (any, error) {
		a.dashboardMu.Lock()
		cached, valid := a.cachedDashboard("country-ranking")
		valid = valid && epoch == a.countryEpoch
		a.dashboardMu.Unlock()
		if valid {
			return cached, nil
		}
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		data, err := a.buildCountryRanking(lookupCtx)
		if err != nil {
			return nil, err
		}
		a.dashboardMu.Lock()
		if epoch == a.countryEpoch {
			a.saveDashboard("country-ranking", data)
		}
		a.dashboardMu.Unlock()
		return data, nil
	})
	select {
	case <-ctx.Done():
		return
	case result := <-pending:
		if result.Err != nil {
			fail(c, result.Err)
			return
		}
		ok(c, result.Val)
	}
}

func (a *App) invalidateCountryRanking() {
	a.dashboardMu.Lock()
	a.countryEpoch++
	delete(a.dashboardCache, "country-ranking")
	a.dashboardMu.Unlock()
}

func (a *App) buildCountryRanking(ctx context.Context) (gin.H, error) {
	// Aggregate events first; each source IP needs at most one GeoIP lookup.
	// Network lookups must not hold up the summary or login trend cache.
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	rows, err := a.db.QueryContext(ctx, a.db.Bind(`SELECT source_ip,COUNT(*) FROM ssh_events WHERE source_ip<>'' AND outcome='failure' AND occurred_at>=? AND occurred_at<=? GROUP BY source_ip`), since, now)
	if err != nil {
		return nil, err
	}
	sources := []map[string]any{}
	var total int64
	for rows.Next() {
		var address string
		var count int64
		if err = rows.Scan(&address, &count); err != nil {
			rows.Close()
			return nil, err
		}
		total += count
		sources = append(sources, map[string]any{"source_ip": address, "attempts": count})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	status := a.decorateEventCountries(ctx, sources)
	countries := map[string]*countryTotal{}
	var located int64
	for _, source := range sources {
		code := source["country_code"].(string)
		if code == "" {
			continue
		}
		count := source["attempts"].(int64)
		located += count
		if countries[code] == nil {
			countries[code] = &countryTotal{Code: code, Name: source["country_name"].(string)}
		}
		countries[code].Count += count
	}
	items := make([]countryTotal, 0, len(countries))
	for _, country := range countries {
		items = append(items, *country)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Code < items[j].Code
	})
	countryCount := len(items)
	if len(items) > 5 {
		items = items[:5]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return gin.H{"items": items, "total": total, "unlocated": total - located, "country_count": countryCount, "geoip_status": status, "from": since, "as_of": now}, nil
}
