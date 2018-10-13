package sli

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	measurev1alpha1 "github.com/slok/service-level-operator/pkg/apis/measure/v1alpha1"
	"github.com/slok/service-level-operator/pkg/log"
	promcli "github.com/slok/service-level-operator/pkg/service/client/prometheus"
)

const promCliTimeout = 2 * time.Second

// prometheus knows how to get SLIs from a prometheus backend.
type prometheus struct {
	cliFactory promcli.ClientFactory
	logger     log.Logger
}

// NewPrometheus returns a new prometheus SLI service.
func NewPrometheus(promCliFactory promcli.ClientFactory, logger log.Logger) Retriever {
	return &prometheus{
		cliFactory: promCliFactory,
		logger:     logger,
	}
}

// Retrieve satisfies Service interface..
func (p *prometheus) Retrieve(sli *measurev1alpha1.SLI) (Result, error) {
	cli, err := p.cliFactory.GetV1APIClient(sli.Prometheus.Address)
	if err != nil {
		return Result{}, err
	}

	// Get both metrics.
	res := Result{}

	promclictx, cancel := context.WithTimeout(context.Background(), promCliTimeout)
	defer cancel()

	// Make queries concurrently.
	g, ctx := errgroup.WithContext(promclictx)
	g.Go(func() error {
		res.TotalQ, err = p.getVectorMetric(ctx, cli, sli.Prometheus.TotalQuery)
		return err
	})
	g.Go(func() error {
		res.ErrorQ, err = p.getVectorMetric(ctx, cli, sli.Prometheus.ErrorQuery)
		return err
	})

	// Wait for the first error or until all of them have finished.
	err = g.Wait()
	if err != nil {
		return Result{}, err
	}

	return res, nil
}

func (p *prometheus) getVectorMetric(ctx context.Context, cli promv1.API, query string) (float64, error) {
	// Make the query.
	val, err := cli.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}

	if val == nil {
		return 0, fmt.Errorf("nil value received from prometheus")
	}

	// Only vectors are valid metrics.
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("received metric needs to be a vector, received: %s", val.Type())
	}
	mtr := val.(model.Vector)

	// We should have only one metric.
	if len(mtr) != 1 {
		return 0, fmt.Errorf("wrong samples length, should be one, gor: %d", len(mtr))
	}

	return float64(mtr[0].Value), nil
}