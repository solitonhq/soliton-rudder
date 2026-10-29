package kube

// AddCatalogForTest registers a resource without running discovery. It exists so other packages' tests can use a
// Session against a fake API server; production code populates the catalog through Discover.
func (s *Session) AddCatalogForTest(key string, r Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalog[key] = r
}
