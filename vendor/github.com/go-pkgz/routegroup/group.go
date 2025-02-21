// Package routegroup provides a way to group routes and applies middleware to them.
// Works with the standard library's http.ServeMux.
package routegroup

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Bundle represents a group of routes with associated middleware.
type Bundle struct {
	mux            *http.ServeMux                    // the underlying mux to register the routes to
	basePath       string                            // base path for the group
	middlewares    []func(http.Handler) http.Handler // middlewares stack
	rootRegistered struct {
		once                       sync.Once // used to register a not found handler for the root path if no / route is registered
		disableRootNotFoundHandler bool      // if true, the not found handler for the root path is not registered automatically
		notFound                   http.HandlerFunc
	}
}

// New creates a new Group.
func New(mux *http.ServeMux) *Bundle {
	return &Bundle{mux: mux}
}

// Mount creates a new group with a specified base path.
func Mount(mux *http.ServeMux, basePath string) *Bundle {
	return &Bundle{mux: mux, basePath: basePath}
}

// ServeHTTP implements the http.Handler interface
func (b *Bundle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.rootRegistered.once.Do(func() {
		if !b.rootRegistered.disableRootNotFoundHandler {
			// register a not found handler for the root path unless it's disabled
			// this is needed to be able to use middleware on all routes, for example logging
			notFoundHandler := http.NotFoundHandler()
			if b.rootRegistered.notFound != nil {
				notFoundHandler = b.rootRegistered.notFound
			}
			b.mux.HandleFunc("/", b.wrapMiddleware(notFoundHandler).ServeHTTP)
		}
	})
	b.mux.ServeHTTP(w, r)
}

// Group creates a new group with the same middleware stack as the original on top of the existing bundle.
func (b *Bundle) Group() *Bundle {
	return b.clone() // copy the middlewares to avoid modifying the original
}

// Mount creates a new group with a specified base path on top of the existing bundle.
func (b *Bundle) Mount(basePath string) *Bundle {
	g := b.clone() // copy the middlewares to avoid modifying the original
	g.basePath += basePath
	return g
}

// Use adds middleware(s) to the Group.
func (b *Bundle) Use(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) {
	b.middlewares = append(b.middlewares, middleware)
	b.middlewares = append(b.middlewares, more...)
}

// With adds new middleware(s) to the Group and returns a new Group with the updated middleware stack.
// The With method is similar to Use, but instead of modifying the current Group,
// it returns a new Group instance with the added middleware(s).
// This allows for creating chain of middleware without affecting the original Group.
func (b *Bundle) With(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) *Bundle {
	newMiddlewares := make([]func(http.Handler) http.Handler, len(b.middlewares), len(b.middlewares)+len(more)+1)
	copy(newMiddlewares, b.middlewares)
	newMiddlewares = append(newMiddlewares, middleware)
	newMiddlewares = append(newMiddlewares, more...)
	return &Bundle{mux: b.mux, basePath: b.basePath, middlewares: newMiddlewares}
}

// Handle adds a new route to the Group's mux, applying all middlewares to the handler.
func (b *Bundle) Handle(pattern string, handler http.Handler) {
	// for file server paths (ending with /), preserve the pattern and disable root not-found handler
	if strings.HasSuffix(pattern, "/") {
		if pattern == "/" {
			b.rootRegistered.disableRootNotFoundHandler = true
		}
		fullPath := b.basePath + pattern
		b.mux.Handle(fullPath, b.wrapMiddleware(handler))
		return
	}
	b.register(pattern, handler.ServeHTTP)
}

// HandleFiles is a helper to serve static files from a directory
func (b *Bundle) HandleFiles(pattern string, root http.FileSystem) {
	// normalize pattern to always have trailing slash
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}

	// build the full path for registration
	fullPath := b.basePath + pattern

	if pattern == "/" && b.basePath == "" {
		// root case - serve directly without stripping
		b.rootRegistered.disableRootNotFoundHandler = true
		b.mux.Handle("/", b.wrapMiddleware(http.FileServer(root)))
		return
	}

	// for both mounted groups and prefixed paths, strip the fullPath
	handler := http.StripPrefix(strings.TrimSuffix(fullPath, "/"), http.FileServer(root))
	b.mux.Handle(fullPath, b.wrapMiddleware(handler))
}

// HandleFunc registers the handler function for the given pattern to the Group's mux.
// The handler is wrapped with the Group's middlewares.
func (b *Bundle) HandleFunc(pattern string, handler http.HandlerFunc) {
	b.register(pattern, handler)
}

// Handler returns the handler and the pattern that matches the request.
// It always returns a non-nil handler, see http.ServeMux.Handler documentation for details.
func (b *Bundle) Handler(r *http.Request) (h http.Handler, pattern string) {
	return b.mux.Handler(r)
}

// DisableNotFoundHandler disables the automatic registration of a not found handler for the root path.
func (b *Bundle) DisableNotFoundHandler() { b.rootRegistered.disableRootNotFoundHandler = true }

// NotFoundHandler sets a custom handler for the root path if no / route is registered.
func (b *Bundle) NotFoundHandler(handler http.HandlerFunc) {
	b.rootRegistered.notFound = handler
}

// Matches non-space characters, spaces, then anything, i.e. "GET /path/to/resource"
var reGo122 = regexp.MustCompile(`^(\S+)\s+(.+)$`)

func (b *Bundle) register(pattern string, handler http.HandlerFunc) {
	matches := reGo122.FindStringSubmatch(pattern)
	var path, method string
	if len(matches) > 2 { // path in the form "GET /path/to/resource"
		method = matches[1]
		path = matches[2]
		pattern = method + " " + b.basePath + path
	} else { // path is just "/path/to/resource"
		path = pattern
		pattern = b.basePath + pattern
		// method is not set intentionally here, the request pattern had no method part
	}

	// if the pattern is the root path on / change it to /{$}
	// this is needed to be able to keep / as a catch-all route and apply middleware to it.
	// at the same time, it keeps handling the root request
	if pattern == "/" || path == "/" {
		if method != "" { // preserve the method part if it was set
			pattern = method + " " + b.basePath + "/{$}"
		} else {
			pattern = b.basePath + "/{$}" // no method part, just the path
		}
	}
	b.mux.HandleFunc(pattern, b.wrapMiddleware(handler).ServeHTTP)
}

// Route allows for configuring the Group inside the configureFn function.
func (b *Bundle) Route(configureFn func(*Bundle)) { configureFn(b) }

// wrapMiddleware applies the registered middlewares to a handler.
func (b *Bundle) wrapMiddleware(handler http.Handler) http.Handler {
	for i := len(b.middlewares) - 1; i >= 0; i-- {
		handler = b.middlewares[i](handler)
	}
	return handler
}

func (b *Bundle) clone() *Bundle {
	middlewares := make([]func(http.Handler) http.Handler, len(b.middlewares))
	copy(middlewares, b.middlewares)
	return &Bundle{mux: b.mux, basePath: b.basePath, middlewares: middlewares}
}

// Wrap directly wraps the handler with the provided middleware(s).
func Wrap(handler http.Handler, mw1 func(http.Handler) http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return mw1(handler) // apply the first middleware
}
