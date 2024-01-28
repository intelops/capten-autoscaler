package cloudprovider

import (
	"context"
	"intelops-scaler/pkg/cloudprovider/azure"
)

type CloudProvider interface {
	ScaleUp(ctx context.Context, count int64) error
	ScaleDown(ctx context.Context, nodeName string) error
}

func New(providerName, subscriptionID string) (CloudProvider, error) {
	return azure.New(subscriptionID)
}
