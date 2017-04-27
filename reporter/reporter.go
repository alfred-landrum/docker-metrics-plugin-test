package reporter

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/matttproud/golang_protobuf_extensions/pbutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	analytics "github.com/segmentio/analytics-go"
	"github.com/tv42/httpunix"
)

const segmentKey = ""

type Reporter struct {
	sockpath string
	segment  *analytics.Client
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func (r *Reporter) Start() {
	if r.sockpath == "" {
		r.sockpath = "/run/docker/metrics.sock"
	}

	r.segment = analytics.New(segmentKey)

	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	r.cancel = cancel

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
			}

			r.doReport()
		}
	}()
}

func (r *Reporter) Stop() {
	r.cancel()
	r.wg.Wait()
}

func makeLabelMap(m *dto.Metric) map[string]interface{} {
	r := make(map[string]interface{})
	for _, lp := range m.Label {
		r[lp.GetName()] = lp.GetValue()
	}
	return r
}

// doReport gathers metrics from the docker metrics socket, and reports
// the whitelist metrics up to segment.
func (r *Reporter) doReport() {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()

	mfs, err := r.gather(ctx)
	if err != nil {
		logrus.WithError(err).Error("metrics gather error")
	}

	for _, mf := range mfs {
		if !(mf.GetName() == "engine_daemon_engine_info" && len(mf.Metric) == 1) {
			continue
		}
		labels := makeLabelMap(mf.Metric[0])
		id, _ := labels["id"].(string)
		logrus.WithFields(logrus.Fields(labels)).Info("engine_daemon_engine_info")
		err := r.segment.Identify(&analytics.Identify{
			UserId: id,
			Traits: labels,
		})
		if err != nil {
			logrus.WithError(err).Error("error reporting to segment")
		}
	}
}

// This metrics gathering code is based on prom2json:
// https://github.com/prometheus/prom2json

const acceptHeader = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3`

func (r *Reporter) gather(ctx context.Context) ([]*dto.MetricFamily, error) {
	var ret []*dto.MetricFamily

	u := &httpunix.Transport{
		DialTimeout:           100 * time.Millisecond,
		RequestTimeout:        1 * time.Second,
		ResponseHeaderTimeout: 1 * time.Second,
	}
	u.RegisterLocation("sockpath", r.sockpath)
	client := http.Client{Transport: u}

	req, err := http.NewRequest("GET", "http+unix://sockpath/metrics", nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Add("Accept", acceptHeader)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %v", resp.StatusCode)
	}

	mediatype, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	if mediatype == "application/vnd.google.protobuf" &&
		params["encoding"] == "delimited" &&
		params["proto"] == "io.prometheus.client.MetricFamily" {
		for {
			mf := &dto.MetricFamily{}
			if _, err = pbutil.ReadDelimited(resp.Body, mf); err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			ret = append(ret, mf)
		}
	} else {
		var parser expfmt.TextParser
		metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
		if err != nil {
			return nil, err
		}
		for _, mf := range metricFamilies {
			ret = append(ret, mf)
		}
	}

	return ret, nil
}
