package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/provider"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
)

type Events struct {
	alerts provider.Alerts
	logger log.Logger

	close chan bool

	getAlertStatus getAlertStatusFn
	route          *dispatch.Route

	mtx sync.RWMutex
}

type getAlertStatusFn func(model.Fingerprint) types.AlertStatus

func New(alerts provider.Alerts, sf getAlertStatusFn) *Events {
	return &Events{
		alerts:         alerts,
		close:          make(chan bool),
		getAlertStatus: sf,
	}
}

// Update sets the configuration string to a new value.
func (e *Events) Update(cfg *config.Config) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	e.route = dispatch.NewRoute(cfg.Route, nil)
	return nil
}

func (e *Events) Register(r *route.Router) {
	r.Get("/events", e.stream)
}

func (e *Events) Close() {
	close(e.close)
}

func (e *Events) stream(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)

	if !ok {
		level.Error(e.logger).Log("msg", "Server-sent events not supported", "err")
		http.Error(w, "Server-sent events not supported", http.StatusInternalServerError)
		return
	}

	it := e.alerts.(*mem.Alerts).SubscribeNewAlerts()
	defer it.Close()

	// make sure we send the header to browser
	fmt.Fprint(w, ": ping\n\n")
	flusher.Flush()

	alertSent := make(map[string]*dispatch.APIAlert)

	for {
		select {
		case <-w.(http.CloseNotifier).CloseNotify():
			return
		case <-time.After(time.Second * 10):
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case alert, ok := <-it.Next():
			if ok {
				fingerPrint := alert.Fingerprint()
				fingerPrintStr := fingerPrint.String()
				status := e.getAlertStatus(fingerPrint)

				// Don't send alerts if we have sent it already to the browser unless
				// unless state has changed.
				if val, ok := alertSent[fingerPrintStr]; (ok && val.Status.State != status.State) || !ok {

					routes := e.route.Match(alert.Labels)
					receivers := make([]string, 0, len(routes))
					for _, r := range routes {
						receivers = append(receivers, r.RouteOpts.Receiver)
					}

					apiAlert := &dispatch.APIAlert{
						Alert:       &alert.Alert,
						Status:      status,
						Receivers:   receivers,
						Fingerprint: alert.Fingerprint().String(),
					}
					alertSent[fingerPrintStr] = apiAlert

					b, err := json.Marshal(apiAlert)

					if err != nil {
						level.Error(e.logger).Log("msg", "Error marshalling JSON", "err", err)
						return
					}

					fmt.Fprint(w, "event: ")
					fmt.Fprintf(w, "alert-%s\n", apiAlert.Status.State)
					fmt.Fprint(w, "data: ")
					w.Write(b)
					fmt.Fprint(w, "\n\n")
					flusher.Flush()
				}
			}
		case <-e.close:
			return
		}
	}
}
