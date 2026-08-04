package graphclustering

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

type countingStructuralProvider struct {
	allEntityCalls atomic.Int32
}

func (p *countingStructuralProvider) GetAllEntityIDs(context.Context) ([]string, error) {
	p.allEntityCalls.Add(1)
	return []string{"test.graph.cluster.structural.node.a"}, nil
}

func (*countingStructuralProvider) GetNeighbors(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (*countingStructuralProvider) GetEdgeWeight(context.Context, string, string) (float64, error) {
	return 1, nil
}

func TestRunStructuralAndAnomalyDetection_SkipsWithoutInitializedOrchestrator(t *testing.T) {
	provider := &countingStructuralProvider{}
	component := &Component{graphProvider: provider}

	assert.True(t, component.runStructuralAndAnomalyDetection(context.Background()))
	assert.Zero(t, provider.allEntityCalls.Load(),
		"disabled or unsuccessfully initialized anomaly detection must perform no structural computation")
}
