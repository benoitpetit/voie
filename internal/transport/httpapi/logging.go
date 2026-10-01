package httpapi

import (
	"net/http"

	"github.com/benoitpetit/voie/utils"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) loggedStatus() int {
	if w.status == 0 {
		return http.StatusInternalServerError
	}
	return w.status
}

func requestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestLog := utils.NewRequestLogger(r.Method, r.URL.Path, "", "")
		requestLog.Start()
		tracked := &statusWriter{ResponseWriter: w}
		defer func() {
			requestLog.End(tracked.loggedStatus(), "request_id=%s %s %s", requestLog.ID, r.Method, r.URL.Path)
		}()
		next.ServeHTTP(tracked, r)
	})
}
