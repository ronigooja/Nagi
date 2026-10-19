package capture

import "net/http"

func registerArchive(mux *http.ServeMux, s *Store, authorize func(http.ResponseWriter, *http.Request) bool, slots chan struct{}) {
}
