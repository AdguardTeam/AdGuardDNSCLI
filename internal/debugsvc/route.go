package debugsvc

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/AdguardTeam/golibs/netutil/httputil"
)

// pathPrefixPrivate is used for indicating private API endpoints.
const pathPrefixPrivate = "/private"

// route registers all necessary handlers in mux.
func (svc *Service) route(mux *http.ServeMux) {
	mw := httputil.NewLogMiddleware(svc.logger, slog.LevelDebug)
	httputil.RoutePprof(httputil.RouterFunc(func(pattern string, h http.Handler) {
		routePattern := addPrefixToRoutePattern(pattern, pathPrefixPrivate)
		mux.Handle(routePattern, mw.Wrap(h))
	}))
}

// addPrefixToRoutePattern adds a prefix to a route pattern.  Originally route
// pattern looks like this:
//
//	GET /path/pattern
//
// So, pattern is split by blank space and prefix is being injected between
// parts of the route.  routePattern must not be an empty string.
func addPrefixToRoutePattern(routePattern, prefix string) (newRoutePat string) {
	method, pat, ok := strings.Cut(routePattern, " ")
	if !ok {
		panic(fmt.Sprintf("debugsvc: invalid route pattern: %q", routePattern))
	}

	newPathPattern, err := url.JoinPath(prefix, pat)
	if err != nil {
		panic(fmt.Sprintf("debugsvc: join path: %q with %q", prefix, pat))
	}

	newRoutePat = method + " " + newPathPattern

	return newRoutePat
}
