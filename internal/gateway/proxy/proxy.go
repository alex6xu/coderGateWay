package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/config"
)

// FreeProxy manages free API proxies
type FreeProxy struct {
	proxies []*ProxyInstance
	mu      sync.RWMutex
}

// ProxyInstance represents a proxy instance
type ProxyInstance struct {
	Name      string
	BaseURL   string
	Models    []string
	RateLimit int
	Proxy     *httputil.ReverseProxy
	Stats     *ProxyStats
}

// ProxyStats represents proxy statistics
type ProxyStats struct {
	TotalRequests  int64
	FailedRequests int64
	LastUsed       time.Time
}

// NewFreeProxy creates a new free proxy manager
func NewFreeProxy(configs []config.ProxyConfig) (*FreeProxy, error) {
	fp := &FreeProxy{
		proxies: make([]*ProxyInstance, 0, len(configs)),
	}

	for _, cfg := range configs {
		proxy, err := fp.createProxy(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create proxy %s: %w", cfg.Name, err)
		}
		fp.proxies = append(fp.proxies, proxy)
	}

	return fp, nil
}

// createProxy creates a proxy instance
func (fp *FreeProxy) createProxy(cfg config.ProxyConfig) (*ProxyInstance, error) {
	target, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Custom director to modify the request
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	return &ProxyInstance{
		Name:      cfg.Name,
		BaseURL:   cfg.BaseURL,
		Models:    cfg.Models,
		RateLimit: cfg.RateLimit,
		Proxy:     proxy,
		Stats:     &ProxyStats{},
	}, nil
}

// GetProxy returns a proxy for the given model
func (fp *FreeProxy) GetProxy(model string) (*ProxyInstance, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	// Find a proxy that supports this model
	for _, proxy := range fp.proxies {
		if fp.supportsModel(proxy, model) {
			return proxy, nil
		}
	}

	return nil, fmt.Errorf("no free proxy available for model: %s", model)
}

// supportsModel checks if a proxy supports a model
func (fp *FreeProxy) supportsModel(proxy *ProxyInstance, model string) bool {
	if len(proxy.Models) == 0 {
		return true
	}

	for _, m := range proxy.Models {
		if strings.EqualFold(m, model) {
			return true
		}
	}

	return false
}

// ServeHTTP serves HTTP requests through the proxy
func (fp *FreeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, model string) error {
	proxy, err := fp.GetProxy(model)
	if err != nil {
		return err
	}

	// Update stats
	proxy.Stats.TotalRequests++
	proxy.Stats.LastUsed = time.Now()

	// Serve the request
	proxy.Proxy.ServeHTTP(w, r)

	return nil
}

// GetStats returns proxy statistics
func (fp *FreeProxy) GetStats() []ProxyStatsResult {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	stats := make([]ProxyStatsResult, 0, len(fp.proxies))
	for _, proxy := range fp.proxies {
		stats = append(stats, ProxyStatsResult{
			Name:           proxy.Name,
			BaseURL:        proxy.BaseURL,
			Models:         proxy.Models,
			TotalRequests:  proxy.Stats.TotalRequests,
			FailedRequests: proxy.Stats.FailedRequests,
			LastUsed:       proxy.Stats.LastUsed,
		})
	}

	return stats
}

// ProxyStatsResult represents proxy statistics result
type ProxyStatsResult struct {
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	Models         []string  `json:"models"`
	TotalRequests  int64     `json:"total_requests"`
	FailedRequests int64     `json:"failed_requests"`
	LastUsed       time.Time `json:"last_used"`
}
