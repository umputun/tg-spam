## routegroup [![Build Status](https://github.com/go-pkgz/routegroup/workflows/build/badge.svg)](https://github.com/go-pkgz/routegroup/actions) [![Go Report Card](https://goreportcard.com/badge/github.com/go-pkgz/routegroup)](https://goreportcard.com/report/github.com/go-pkgz/routegroup) [![Coverage Status](https://coveralls.io/repos/github/go-pkgz/routegroup/badge.svg?branch=master)](https://coveralls.io/github/go-pkgz/routegroup?branch=master) [![godoc](https://godoc.org/github.com/go-pkgz/routegroup?status.svg)](https://godoc.org/github.com/go-pkgz/routegroup)


`routegroup` is a tiny Go package providing a lightweight wrapper for efficient route grouping and middleware integration with the standard `http.ServeMux`.

## Features

- Simple and intuitive API for route grouping and route mounting.
- Lightweight, just about 100 LOC
- Easy middleware integration for individual routes or groups of routes.
- Seamless integration with Go's standard `http.ServeMux`.
- Fully compatible with the `http.Handler` interface and can be used as a drop-in replacement for `http.ServeMux`.
- No external dependencies.

## Install and update

`go get -u github.com/go-pkgz/routegroup`

## Usage

**Creating a New Route Group**

To start, create a new route group without a base path:

```go
func main() {
    mux := http.NewServeMux()
    group := routegroup.New(mux)
}
```

**Adding Routes with Middleware**

Add routes to your group, optionally with middleware:

```go
    group.Use(loggingMiddleware, corsMiddleware)
    group.Handle("/hello", helloHandler)
    group.Handle("/bye", byeHandler)
```
**Creating a Nested Route Group**

For routes under a specific path prefix `Mount` method can be used to create a nested group:

```go
    apiGroup := routegroup.Mount(mux, "/api")
    apiGroup.Use(loggingMiddleware, corsMiddleware)
    apiGroup.Handle("/v1", apiV1Handler)
    apiGroup.Handle("/v2", apiV2Handler)

```

**Complete Example**

Here's a complete example demonstrating route grouping and middleware usage:

```go
package main

import (
	"net/http"

	"github.com/go-pkgz/routegroup"
)

func main() {
	router := routegroup.New(http.NewServeMux())
	router.Use(loggingMiddleware)

	// handle the /hello route
	router.Handle("GET /hello", helloHandler)
	
	// create a new group for the /api path
	apiRouter := router.Mount("/api")
	// add middleware
	apiRouter.Use(loggingMiddleware, corsMiddleware)

	// route handling
	apiRouter.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, API!"))
	})

	// add another group with its own set of middlewares
	protectedGroup := router.Group()
	protectedGroup.Use(authMiddleware)
	protectedGroup.HandleFunc("GET /protected", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Protected API!"))
	})

	http.ListenAndServe(":8080", router)
}
```

**Applying Middleware to Specific Routes**

You can also apply middleware to specific routes inside the group without modifying the group's middleware stack:

```go
apiGroup.With(corsMiddleware, apiMiddleware).Handle("GET /hello", helloHandler)
```

**Alternative Usage with `Route`**

You can also use the `Route` method to add routes and middleware in a single function call:

```go
router := routegroup.New(http.NewServeMux())
router.Group().Route(func(b *routegroup.Bundle) {
    b.Use(loggingMiddleware, corsMiddleware)
    b.Handle("GET /hello", helloHandler)
    b.Handle("GET /bye", byeHandler)
})
http.ListenAndServe(":8080", group)
```

Important: The `Route` method does not create a new group by itself; it merely applies middleware and routes to the current group in a functional style. In the example provided, this is technically equivalent to sequentially calling the `Use` and `Handle` methods for the caller's group. While this may not seem intuitive, it is crucial to understand, as using the `Route` method might mistakenly appear to be a way to create a new (sub)group, which it is not. In 99% of cases, `Route` should be called after the creation of a sub-group, either by the `Mount` or `Group` methods.

For example, using `Route` in this manner is likely a mistake, as it will apply middleware to the root group, not to the newly created sub-group.

```go
group := routegroup.New(http.NewServeMux())
group.Route(func(b *routegroup.Bundle) {
    b.Use(loggingMiddleware, corsMiddleware)
    b.Route(func(sub *routegroup.Bundle) {
        sub.Handle("GET /hello", helloHandler)
    })
})
```

**Setting optional `NotFoundHandler`**

It is possible to set a custom `NotFoundHandler` for the group. This handler will be called when no other route matches the request:

```go
    group.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "404 page not found, something is wrong!", http.StatusNotFound)
    })
```

If a custom `NotFoundHandler` is not configured, `routegroup` will default to using a handler from the standard library (`http.NotFoundHandler()`). It is important to note that the `NotFoundHandler` serves as a catch-all route, which influences "Method Not Allowed"  (405) responses. Consequently, if an incorrect method is called, the response will be 404 (or the custom status specified by the `NotFoundHandler`) rather than 405. This behavior aligns with the standard `http.ServeMux` and [may be improved](https://github.com/golang/go/issues/65648) in future versions of Go.


### Using derived groups

In some instances, it's practical to create an initial group that includes a set of middlewares, and then derive all other groups from it. This approach guarantees that every group incorporates a common set of middlewares as a foundation, allowing each to add its specific middlewares. To facilitate this scenario, `routegroup` offers both `Bundle.Group` and `Bundle.Mount` methods, and it also implements the `http.Handler` interface. The following example illustrates how to use derived groups:

```go
// create a new bundle with a base set of middlewares
// note: the bundle is also http.Handler and can be passed to http.ListenAndServe
router := routegroup.New(http.NewServeMux()) 
router.Use(loggingMiddleware, corsMiddleware)

// add a new, derived group with its own set of middlewares
// this group will inherit the middlewares from the base group
apiGroup := router.Group()
apiGroup.Use(apiMiddleware)
apiGroup.Handle("GET /hello", helloHandler)
apiGroup.Handle("GET /bye", byeHandler)

// mount another group for the /admin path with its own set of middlewares, 
// using `Route` method to show the alternative usage.
// this group will inherit the middlewares from the base group as well
router.Mount("/admin").Route(func(b *routegroup.Bundle) {
    b.Use(adminMiddleware)
    b.Handle("POST /do", doHandler)
})

// start the server, passing the wrapped mux as the handler
http.ListenAndServe(":8080", router)
```
### Wrap Function

Sometimes route's group is not necessary, and all you need is to apply middleware(s) directly to a single route. In this case, `routegroup` provides a `Wrap` function that can be used to wrap a single `http.Handler` with one or more middlewares. Here's an example:

```go
mux := http.NewServeMux()
mux.HandleFunc("/hello", routegroup.Wrap(helloHandler, loggingMiddleware, corsMiddleware))
http.ListenAndServe(":8080", mux)
```

### Automatic registration of `NotFoundHandler` as catch-all route

`routegroup` automatically registers a `NotFoundHandler` as a catch-all route, which is invoked when no other route matches the request. This handler is wrapped with all the middlewares that are associated with the group. This functionality is beneficial for applying middleware to all routes, including those that are unknown. It practically enables the use of middlewares that should operate across all routes, such as logging.

In case you want to disable this behavior, you can use the `DisableNotFoundHandler()` function.

### HandleFiles helper

`routegroup` provides a helper function `HandleFiles` that can be used to serve static files from a directory. The function is a thin wrapper around the standard `http.FileServer` and can be used to serve files from a specific directory. Here's an example:

```go
// serve static files from the "assets/static" directory
router.HandleFiles("/static/", http.Dir("assets/static"))
```

## Real-world example

Here's an example of how `routegroup` can be used in a real-world application. The following code snippet is taken from a web service that provides a set of routes for user authentication, session management, and user management. The service also serves static files from the "assets/static" embedded file system.

```go

// Routes returns http.Handler that handles all the routes for the Service.
// It also serves static files from the "assets/static" directory.
// The rootURL option sets prefix for the routes.
func (s *Service) Routes() http.Handler {
	router := routegroup.Mount(http.NewServeMux(), s.rootURL) // make a bundle with the rootURL base path
	// add common middlewares
	router.Use(rest.Maybe(handlers.CompressHandler, func(*http.Request) bool { return !s.skipGZ }))
	router.Use(rest.Throttle(s.limitActiveReqs))
	router.Use(s.middleware.securityHeaders(s.skipSecurityHeaders))

	// prepare csrf middleware
	csrfMiddleware := s.middleware.csrf(s.skipCSRFCheck)

	// add open routes
	router.HandleFunc("GET /login", s.loginPageHandler)
	router.HandleFunc("POST /login", s.loginCheckHandler)
	router.HandleFunc("GET /logout", s.logoutHandler)

	// add routes with auth middleware
	router.Group().Route(func(auth *routegroup.Bundle) {
		auth.Use(s.middleware.Auth())
		auth.HandleFunc("GET /update", s.pwdUpdateHandler)
		auth.With(csrfMiddleware).HandleFunc("PUT /update", s.pwdUpdateHandler)
	})

	// add admin routes
	router.Mount("/admin").Route(func(admin *routegroup.Bundle) {
		admin.Use(s.middleware.Auth("admin"))
		admin.Use(s.middleware.AdminOnly)
		admin.HandleFunc("GET /", s.admin.renderHandler)
		admin.With(csrfMiddleware).Route(func(csrf *routegroup.Bundle) {
			csrf.HandleFunc("DELETE /sessions", s.admin.deleteSessionsHandler)
			csrf.HandleFunc("POST /user", s.admin.addUserHandler)
			csrf.HandleFunc("DELETE /user", s.admin.deleteUserHandler)
		})
	})

	router.HandleFunc("GET /static/*", s.fileServerHandlerFunc()) // serve static files
	return router
}

// fileServerHandlerFunc returns http.HandlerFunc that serves static files from the "assets/static" directory.
// prefix is set by the rootURL option.
func (s *Service) fileServerHandlerFunc() http.HandlerFunc {
    staticFS, err := fs.Sub(assets, "assets/static") // error is always nil
    if err != nil {
        panic(err) // should never happen we load from embedded FS
    }
    return func(w http.ResponseWriter, r *http.Request) {
        webFS := http.StripPrefix(s.rootURL+"/static/", http.FileServer(http.FS(staticFS)))
        webFS.ServeHTTP(w, r)
    }
}
```

## Contributing

Contributions to `routegroup` are welcome! Please submit a pull request or open an issue for any bugs or feature requests.

## License

`routegroup` is available under the MIT license. See the [LICENSE](https://github.com/go-pkgz/routegroup/blob/master/LICENSE) file for more info.
