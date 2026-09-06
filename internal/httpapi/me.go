package httpapi

import "net/http"

func Me() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		applicationID, ok := ApplicationID(r.Context())
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"application_id": applicationID.String()})
	}
}
