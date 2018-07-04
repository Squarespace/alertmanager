package stream

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/alertmanager/provider"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
)

type fakeAlerts struct {
	alerts   []*types.Alert
	finished chan struct{}
}

func newFakeAlerts(alerts []*types.Alert) *fakeAlerts {
	return &fakeAlerts{
		alerts:   alerts,
		finished: make(chan struct{}),
	}
}

func (f *fakeAlerts) GetPending() provider.AlertIterator          { return nil }
func (f *fakeAlerts) Get(model.Fingerprint) (*types.Alert, error) { return nil, nil }
func (f *fakeAlerts) Put(...*types.Alert) error                   { return nil }
func (f *fakeAlerts) Subscribe() provider.AlertIterator {
	ch := make(chan *types.Alert)
	done := make(chan struct{})
	go func() {
		for _, a := range f.alerts {
			ch <- a
		}

		ch <- &types.Alert{
			Alert: model.Alert{
				Labels:   model.LabelSet{},
				StartsAt: time.Now(),
			},
		}
		close(f.finished)
		<-done
	}()
	return provider.NewAlertIterator(ch, done, nil)
}

func TestEventStream(t *testing.T) {
	now := time.Now()
	a := &types.Alert{
		Alert: model.Alert{
			Labels:   model.LabelSet{"t": "1", "e": "f"},
			StartsAt: now.Add(-time.Minute),
			EndsAt:   now.Add(time.Hour),
		},
	}

	router := route.New()
	ap := newFakeAlerts([]*types.Alert{a})
	eventStream := New(ap)
	eventStream.Register(router)
	ts := httptest.NewServer(router)
	defer ts.Close()

	client := &http.Client{}
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/events", ts.URL), nil)

	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}

	r.Header.Set("Cache-Control", "no-cache")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(r)

	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}

	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	for {
		select {
		case <-ap.finished:
			eventStream.Close()
			return
		default:
			line, err := reader.ReadBytes('\n')

			if err != nil {
				fmt.Println(err)
				break
			}
			fmt.Println(err)
			fmt.Println(string(line))
		}
	}
}
