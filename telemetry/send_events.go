// Copyright © Microsoft <wastore@microsoft.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package telemetry

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ReportEvents processes and sends one or more generated telemetry events
// (JobStartedEvent / JobFinishedEvent) using the configured backend. It stops
// and returns on the first error.
func (r *Reporter) ReportEvents(ctx context.Context, events ...MetricEvent) error {
	for _, evt := range events {
		if err := r.ReportEvent(ctx, evt); err != nil {
			return fmt.Errorf("report %s: %w", evt.EventName(), err)
		}
	}
	return nil
}

// ReportEvent processes a single telemetry event into data points and sends
// them using the configured backend.
func (r *Reporter) ReportEvent(ctx context.Context, evt MetricEvent) error {
	switch r.cfg.Backend {
	case BackendOTel:
		return r.sendEventOTel(ctx, evt)
	case BackendAppInsights:
		return r.sendEventAppInsights(ctx, evt)
	default:
		return fmt.Errorf("unknown telemetry backend: %q", r.cfg.Backend)
	}
}

// ---------------------------------------------------------------------------
// App Insights path – hand-built envelopes, one per measurement.
// ---------------------------------------------------------------------------

func (r *Reporter) sendEventAppInsights(ctx context.Context, evt MetricEvent) error {
	endpoint, ikey, err := r.endpointAndKey()
	if err != nil {
		return err
	}

	envelopes := eventToEnvelopes(ikey, evt)
	if len(envelopes) == 0 {
		return nil
	}

	if _, err := postEnvelopes(ctx, r.httpClient(), endpoint, envelopes); err != nil {
		return err
	}

	log.Printf("telemetry: sent %s (%d measurements) to App Insights", evt.EventName(), len(envelopes))
	return nil
}

// eventToEnvelopes converts an event into one App Insights envelope per
// measurement. The Track API accepts a batch of envelopes, but only ingests the
// first metric when multiple metrics share one envelope.
func eventToEnvelopes(ikey string, evt MetricEvent) []appInsightsEnvelope {
	measurements := evt.measurements()
	envelopes := make([]appInsightsEnvelope, 0, len(measurements))
	timestamp := evt.timestamp().UTC().Format(time.RFC3339)
	properties := evt.attributes()
	for _, m := range measurements {
		envelopes = append(envelopes, appInsightsEnvelope{
			Name: "Microsoft.ApplicationInsights.Metric",
			Time: timestamp,
			IKey: ikey,
			Data: appInsightsData{
				BaseType: "MetricData",
				BaseData: appInsightsMetricData{
					Metrics:    []appInsightsMetric{{Name: m.Name, Value: m.Value, Count: m.Count}},
					Properties: properties,
				},
			},
		})
	}
	return envelopes
}
