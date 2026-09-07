package httpapi

import (
	"net/http"

	"github.com/swaggest/swgui/v5emb"
)

func OpenAPISpec(spec []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(spec)
	}
}

func Docs() http.Handler {
	return v5emb.New("hookline", "/openapi.yaml", "/docs/")
}
