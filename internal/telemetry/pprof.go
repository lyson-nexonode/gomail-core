package telemetry

import (
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on the default mux

	"go.uber.org/zap"
)

// StartPPROF starts the pprof HTTP server on a separate goroutine.
// The address must always be a localhost or internal address.
// Never expose this endpoint on a public network interface.
func StartPPROF(addr string, log *zap.Logger) {
	go func() {
		log.Info("pprof listening", zap.String("addr", addr))
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Error("pprof server failed", zap.Error(err))
		}
	}()
}
