package tygor

import "tygor.dev/internal"

// Routes returns route metadata for code generation.
// The return type is internal; this method is for use by tygorgen only.
func (a *App) Routes() internal.RouteMap {
	a.mu.RLock()
	defer a.mu.RUnlock()

	routes := make(internal.RouteMap)
	for name, method := range a.routes {
		md := method.Metadata()
		md.Name = name
		routes[name] = md
	}
	return routes
}
