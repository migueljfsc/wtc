package server

import (
	_ "embed"
	"net/http"
)

// openapiJSON is the hand-authored OpenAPI 3.1 document describing the
// versioned /api/v1 query surface. It is the contract the portal's typed
// client is generated from. The drift test (openapi_test.go) asserts every
// route in apiRoutes() is documented here, so a new endpoint cannot ship
// without its spec entry — keep this file in step with the handlers.
//
//go:embed openapi.json
var openapiJSON []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(openapiJSON); err != nil {
		s.log.Error("write openapi", "error", err)
	}
}
