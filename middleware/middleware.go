// Package middleware provides HTTP middleware for use with standard net/http handlers.
//
// All middlewares follow the standard Go middleware signature:
//
//	func(http.Handler) http.Handler
//
// They can be composed using the [Chain] helper.
package middleware
