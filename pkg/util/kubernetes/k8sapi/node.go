package k8sapi

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type nodePatchRequest struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value bool   `json:"value"`
}

func CordonAndDrainNode(ctx context.Context, kubeClient kubernetes.Interface, node *v1.Node) error {
	nodePatchPayload := []nodePatchRequest{{
		Op:    "replace",
		Path:  "/spec/unschedulable",
		Value: true,
	}}

	nodePatchJson, _ := json.Marshal(nodePatchPayload)
	result, err := kubeClient.CoreV1().Nodes().Patch(ctx, node.Name, types.JSONPatchType, nodePatchJson, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to unschedule node")
	}

	fmt.Println(result)
	//Drain node
	return nil
}
