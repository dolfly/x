package stats

import (
	"sync"

	"github.com/dolfly/core/observer"
	"github.com/dolfly/core/observer/stats"
	xstats "github.com/dolfly/x/observer/stats"
)

type HandlerStats struct {
	service      string
	stats        map[string]stats.Stats
	resetTraffic bool
	mu           sync.RWMutex
}

func NewHandlerStats(service string, resetTraffic bool) *HandlerStats {
	return &HandlerStats{
		service:      service,
		stats:        make(map[string]stats.Stats),
		resetTraffic: resetTraffic,
	}
}

func (p *HandlerStats) Stats(client string) stats.Stats {
	if p == nil {
		return nil
	}

	p.mu.RLock()
	pstats := p.stats[client]
	p.mu.RUnlock()
	if pstats != nil {
		return pstats
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	pstats = p.stats[client]
	if pstats == nil {
		pstats = xstats.NewStats(p.resetTraffic)
	}
	p.stats[client] = pstats

	return pstats
}

func (p *HandlerStats) Events() (events []observer.Event) {
	if p == nil {
		return
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for k, v := range p.stats {
		if !v.IsUpdated() {
			continue
		}
		events = append(events, xstats.StatsEvent{
			Kind:         "handler",
			Service:      p.service,
			Client:       k,
			TotalConns:   v.Get(stats.KindTotalConns),
			CurrentConns: v.Get(stats.KindCurrentConns),
			InputBytes:   v.Get(stats.KindInputBytes),
			OutputBytes:  v.Get(stats.KindOutputBytes),
			TotalErrs:    v.Get(stats.KindTotalErrs),
		})
	}
	return
}
