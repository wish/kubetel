package healthmonitor

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"
	goji "goji.io"
	"goji.io/pat"
)

type server struct {
}

func NewServer(port int, stopCh chan struct{}) error {
	mux := goji.NewMux()
	mux.Handle(pat.Get("/status"), StaticContentHandler("ok"))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 1 * time.Minute,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			if err.Error() != "http: Server closed" {
				log.Infof("Server error during ListenAndServe: %v", err)
				close(stopCh)
			}
		}
	}()

	log.Infof("Started server on %v", srv.Addr)

	<-stopCh
	log.Info("Shutting down http server")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	glog.V(1).Info("Server gracefully stopped")

	return nil
}
func StaticContentHandler(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(content)); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}
