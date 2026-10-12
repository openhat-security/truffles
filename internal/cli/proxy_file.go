package cli

import (
	"fmt"

	"github.com/adamsiwiec/truffles/internal/proxy"
)

func applyProxyFile(pool *proxy.Pool, path string) error {
	if pool == nil || path == "" {
		return nil
	}
	candidates, trusted, err := proxy.LoadProxyFile(path)
	if err != nil {
		return fmt.Errorf("proxy file: %w", err)
	}
	pool.PrependCandidates(candidates)
	pool.AdmitTrusted(trusted)
	return nil
}
