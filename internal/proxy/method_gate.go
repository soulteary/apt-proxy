package proxy

import "net/http"

// Repository access only fetches objects. Reject other methods before routing,
// credential normalization or cache access, so a download client cannot borrow
// configured mirror credentials to modify a writable upstream repository.
func rejectRepositoryMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return false
	}
	w.Header().Set("Allow", "GET, HEAD")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "repository requests only support GET and HEAD", http.StatusMethodNotAllowed)
	return true
}
