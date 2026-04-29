package service

import "net/http"

// HTTPMiddleware wraps an http.Handler. Products supply middleware
// via Manager.UseHTTPMiddleware before Start; the Manager wraps the
// framework's HTTP mux with the chain at server boot.
//
// Application order is outermost-first: the slice element at index 0
// is the outermost wrapper — it sees the request first and the
// response last. The element at index len-1 is innermost, executing
// closest to the registered handler. This matches the convention
// established in ADR-030 and is the same direction the standard
// library's reverse-build composition pattern produces (see
// chainMiddleware below).
//
// The framework ships zero default middleware. Common cross-cutting
// concerns — auth, request logging, panic recovery, rate limiting,
// CORS — are product policy and live in product-shell middleware.
// Products plugging in identity-aware middleware should pair with
// agenticdispatch.WithIdentity to populate the ctx that
// agenticdispatch.IdentityFromRequest consumes downstream (the
// beta.22 helper pair).
type HTTPMiddleware func(http.Handler) http.Handler

// chainMiddleware wraps h with the supplied middleware chain. With
// an empty or nil slice it returns h unchanged so the no-middleware
// path stays a single bounds check + return — products that don't
// plug anything in pay nothing.
//
// Build direction is reverse so that mws[0] becomes the outermost
// wrapper after composition. Matches the documented
// "outermost-first" contract on HTTPMiddleware.
func chainMiddleware(h http.Handler, mws []HTTPMiddleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] == nil {
			continue
		}
		h = mws[i](h)
	}
	return h
}
