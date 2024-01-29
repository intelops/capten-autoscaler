package azure

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
)

type client struct {
	subscriptionId string
	clientFactory  *armcompute.ClientFactory
}

func New(subscriptionId string) (*client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain a credential: %v", err)
	}

	clientFactory, err := armcompute.NewClientFactory(subscriptionId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %v", err)
	}

	return &client{
		subscriptionId: subscriptionId,
		clientFactory:  clientFactory,
	}, nil
}

func (c *client) ScaleUp(ctx context.Context, count int64) error {
	res, err := c.clientFactory.NewVirtualMachineScaleSetsClient().Get(ctx, "talosworker_group", "talosworker", &armcompute.VirtualMachineScaleSetsClientGetOptions{Expand: nil})
	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	}

	fmt.Println(*res.SKU.Capacity)
	// You could use response here. We use blank identifier for just demo purposes.
	fmt.Printf("%+v", res)
	*res.SKU.Capacity += count
	req := armcompute.VirtualMachineScaleSetUpdate{
		Identity:   nil,
		Plan:       nil,
		Properties: nil,
		SKU:        res.SKU,
		Tags:       nil,
	}

	updateRes, err := c.clientFactory.NewVirtualMachineScaleSetsClient().BeginUpdate(ctx, "talosworker_group", "talosworker", req, nil)
	if err != nil {
		return fmt.Errorf("failed to update the scale set: %v", err)
	}

	result, err := updateRes.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to scale up node: %w", err)
	}

	fmt.Printf("%+v", result)
	return nil
}

func (c *client) ScaleDown(ctx context.Context, nodeName string) error {
	vmName := ""
	pager := c.clientFactory.NewVirtualMachinesClient().NewListPager("talosworker_group", &armcompute.VirtualMachinesClientListOptions{})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Fatalf("failed to advance page: %v", err)
		}
		for _, v := range page.Value {
			// You could use page here. We use blank identifier for just demo purposes.
			if v.Properties != nil && v.Properties.OSProfile != nil && strings.EqualFold(*v.Properties.OSProfile.ComputerName, nodeName) {
				vmName = *v.Name
				break
			}
		}
	}

	if vmName == "" {
		return fmt.Errorf("failed to get the vmName for node: %s", nodeName)
	}
	
	poller, err := c.clientFactory.NewVirtualMachinesClient().BeginDelete(ctx, "talosworker_group",
		vmName,
		&armcompute.VirtualMachinesClientBeginDeleteOptions{ForceDeletion: to.Ptr(true)})
	if err != nil {
		return fmt.Errorf("failed to finish the request: %w", err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to pull the result: %w", err)
	}

	return nil
}
