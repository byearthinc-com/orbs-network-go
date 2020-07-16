// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

package metric

import (
	"context"
	"fmt"
	"github.com/orbs-network/govnr"
	"github.com/orbs-network/orbs-network-go/synchronization"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/scribe/log"
	"github.com/pkg/errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ROTATE_INTERVAL = 30 * time.Second
const AGGREGATION_SPAN = 10 * time.Minute

type Factory interface {
	NewHistogram(name string, maxValue int64) *Histogram
	NewLatency(name string, maxDuration time.Duration) *Histogram
	NewGauge(name string) *Gauge
	NewRate(name string) *Rate
	NewText(name string, defaultValue ...string) *Text
}

type Registry interface {
	Factory
	WithVirtualChainId(id primitives.VirtualChainId) Registry
	WithNodeAddress(nodeAddress primitives.NodeAddress) Registry
	Remove(metric metric)
	Get(metricName string) metric
	PeriodicallyRotate(ctx context.Context, logger log.Logger) govnr.ShutdownWaiter
	ExportAll() map[string]exportedMetric
	ExportPrometheus() string
}

type metric interface {
	Name() string
	Export() exportedMetric
	Value() interface{}
	exportPrometheus(labelString string) string
}

type exportedMetric interface {
}

type inMemoryRegistry struct {
	vcid        primitives.VirtualChainId
	nodeAddress primitives.NodeAddress
	mu          struct {
		sync.RWMutex
		metrics map[string]metric
	}
}

func NewRegistry() *inMemoryRegistry {
	r := &inMemoryRegistry{}
	r.mu.metrics = make(map[string]metric)
	return r
}

func (r *inMemoryRegistry) WithVirtualChainId(id primitives.VirtualChainId) Registry {
	r.vcid = id
	return r
}

func (r *inMemoryRegistry) WithNodeAddress(nodeAddress primitives.NodeAddress) Registry {
	r.nodeAddress = nodeAddress
	return r
}

func (r *inMemoryRegistry) Get(metricName string) metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, metric := range r.mu.metrics {
		if metric.Name() == metricName {
			return metric
		}
	}
	return nil
}

func (r *inMemoryRegistry) Remove(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%p", m)
	if _, isMetricInMap := r.mu.metrics[key]; isMetricInMap {
		delete(r.mu.metrics, key)
	}
}

func (r *inMemoryRegistry) register(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%p", m)
	if _, isMetricInMap := r.mu.metrics[key]; isMetricInMap {
		err := errors.Errorf("a metric with name %s is already registered", m.Name())
		panic(err)
	}

	r.mu.metrics[key] = m
}

func (r *inMemoryRegistry) NewRate(name string) *Rate {
	m := newRate(name)
	r.register(m)
	return m
}

func (r *inMemoryRegistry) NewGauge(name string) *Gauge {
	g := newGauge(name)
	r.register(g)
	return g
}

func (r *inMemoryRegistry) NewLatency(name string, maxDuration time.Duration) *Histogram {
	h := newHistogram(name, maxDuration.Nanoseconds(), int(AGGREGATION_SPAN/ROTATE_INTERVAL))
	r.register(h)
	return h
}

func (r *inMemoryRegistry) NewHistogram(name string, maxValue int64) *Histogram {
	h := newHistogram(name, maxValue, int(AGGREGATION_SPAN/ROTATE_INTERVAL))
	r.register(h)
	return h
}

func (r *inMemoryRegistry) NewText(name string, defaultValue ...string) *Text {
	m := newText(name, defaultValue...)
	r.register(m)
	return m
}

func (r *inMemoryRegistry) PeriodicallyRotate(ctx context.Context, logger log.Logger) govnr.ShutdownWaiter {
	return synchronization.NewPeriodicalTrigger(ctx, "Metric registry rotation trigger", synchronization.NewTimeTicker(ROTATE_INTERVAL), logger, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, m := range r.mu.metrics {
			switch m.(type) {
			case *Histogram: // only Histograms currently require rotating
				m.(*Histogram).Rotate()
			}
		}
	}, nil)
}

func (r *inMemoryRegistry) ExportAll() map[string]exportedMetric {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make(map[string]exportedMetric)
	for _, m := range r.mu.metrics {
		all[m.Name()] = m.Export()
	}

	return all
}

// For info on Prometheus labels, see: https://prometheus.io/docs/practices/naming/#labels
func (r *inMemoryRegistry) ExportPrometheus() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	labelsString := r.labelsString()

	var rows []string
	for _, metric := range r.mu.metrics {
		rows = append(rows, metric.exportPrometheus(labelsString))
	}

	return strings.Join(rows, "")
}

func (r *inMemoryRegistry) labelsString() string {
	var lables []string
	if r.vcid > 0 {
		lables = append(lables, fmt.Sprintf("vcid=\"%s\"", strconv.FormatUint(uint64(r.vcid), 10)))
	}
	if r.nodeAddress != nil {
		lables = append(lables, fmt.Sprintf("node=\"%s\"", r.nodeAddress.String()))
	}
	return strings.Join(lables, ",")
}
