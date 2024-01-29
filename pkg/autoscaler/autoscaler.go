package autoscaler

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math"
	"os"
	"time"

	"intelops-scaler/pkg/cloudprovider"
	"intelops-scaler/pkg/util/kubernetes/drain"
	"intelops-scaler/pkg/util/kubernetes/k8sapi"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	kube_client "k8s.io/client-go/kubernetes"
	kube_record "k8s.io/client-go/tools/record"
)

const (
	masterNodeLabel = "node-role.kubernetes.io/control-plane"
)

type NodeInfo struct {
	CPU    float64
	Memory int64
	Min    int64
	Max    int64
}

type Autoscaler struct {
	// Listers.
	k8sapi.ListerRegistry
	// ClientSet interface.
	ClientSet kube_client.Interface
	// Recorder for recording events.
	Recorder      kube_record.EventRecorder
	Node          NodeInfo
	cloudProvider cloudprovider.CloudProvider
}

func NewAutoscaler(kubeClient kube_client.Interface,
	informerFactory informers.SharedInformerFactory,
	info NodeInfo,
	cloudProvider cloudprovider.CloudProvider) *Autoscaler {
	listerRegistry := k8sapi.NewListerRegistryWithDefaultListers(informerFactory)
	kubeEventRecorder := k8sapi.CreateEventRecorder(kubeClient, true)

	return &Autoscaler{
		ListerRegistry: listerRegistry,
		ClientSet:      kubeClient,
		Recorder:       kubeEventRecorder,
		Node:           info,
		cloudProvider:  cloudProvider,
	}
}

func (a *Autoscaler) Start(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if err := a.reconcile(ctx); err != nil {
				fmt.Println("err", err)
				return
			}

			fmt.Println("Tick at", t)
		}
	}
}

func (a *Autoscaler) reconcile(ctx context.Context) error {
	pods, err := a.AllPodLister().List()
	if err != nil {
		return err
	}

	nodes, err := a.getWorkerNodes()
	if err != nil {
		return fmt.Errorf("failed to get worker nodes: %w", err)
	}

	unscheduledPods := k8sapi.UnschedulablePods(pods)
	if len(unscheduledPods) > 0 {
		requiredCpu, requiredMemory := getRequirement(unscheduledPods)
		numOfNodesToScale := a.getNodesToScaleUp(requiredCpu, requiredMemory)
		fmt.Println(numOfNodesToScale)
		if err := a.scaleUp(ctx, numOfNodesToScale); err != nil {
			return fmt.Errorf("failed to scale up node: %v", err)
		}
	} else {
		requiredCpu, requiredMemory := getRequirement(pods)
		minNodesRequired := a.getMinNodesRequired(requiredCpu, requiredMemory)
		fmt.Println("minimum nodes required", minNodesRequired)
		if len(nodes) == int(minNodesRequired) {
			return nil
		}

		return a.scaleDown(ctx, minNodesRequired, nodes)
		//return nil
	}

	return nil
}

func getRequirement(allPods []*apiv1.Pod) (float64, int64) {
	requiredCpu, requiredMemory := float64(0), int64(0)
	for _, pod := range allPods {
		for _, c := range pod.Spec.Containers {
			//fmt.Println("container ", c.Name, "cpu", c.Resources.Requests.Cpu().AsApproximateFloat64())
			//fmt.Println("container ", c.Name, "memory", c.Resources.Requests.Memory().Value())
			requiredCpu += c.Resources.Requests.Cpu().AsApproximateFloat64()
			requiredMemory += c.Resources.Requests.Memory().Value()
		}
	}

	return requiredCpu, requiredMemory
}

func (a *Autoscaler) getNodesToScaleUp(requiredCpu float64, requiredMemory int64) int64 {
	nodeCount := a.getNodeCount(requiredCpu, requiredMemory)
	return min(nodeCount, a.Node.Max)
}

func (a *Autoscaler) getMinNodesRequired(requiredCpu float64, requiredMemory int64) int32 {
	nodeCount := a.getNodeCount(requiredCpu, requiredMemory)
	return int32(max(nodeCount, a.Node.Min))
}

func (a *Autoscaler) getNodeCount(requiredCpu float64, requiredMemory int64) int64 {
	cpuBasedNode := math.Ceil(requiredCpu / a.Node.CPU)
	memoryBasedNode := requiredMemory / a.Node.Memory

	fmt.Println("cpu based nodes", cpuBasedNode, requiredCpu, a.Node.CPU)
	return max(int64(cpuBasedNode), memoryBasedNode)
}

func (a *Autoscaler) getWorkerNodes() ([]*apiv1.Node, error) {
	nodes, err := a.AllNodeLister().List()
	if err != nil {
		return nil, err
	}

	filteredNode := make([]*apiv1.Node, 0)
	for _, node := range nodes {
		if _, ok := node.Labels[masterNodeLabel]; !ok {
			filteredNode = append(filteredNode, node)
		}
	}

	return filteredNode, nil
}

func (a *Autoscaler) scaleUp(ctxt context.Context, val int64) error {
	return a.cloudProvider.ScaleUp(ctxt, val)
}

func (a *Autoscaler) scaleDown(ctx context.Context, val int32, nodes []*apiv1.Node) error {
	nodesToScaleDown := int32(len(nodes)) - val
	if nodesToScaleDown == 0 {
		return nil
	}

	for i := int32(0); i < nodesToScaleDown; i++ {
		cordonHelper := drain.NewCordonHelper(nodes[i])
		err, patchErr := cordonHelper.PatchOrReplace(a.ClientSet, false)
		if err != nil || patchErr != nil {
			fmt.Println("failed to cordon node", nodes[i].Name)
			continue
		}

		drainHelper := drain.Helper{
			Ctx:                             ctx,
			Client:                          a.ClientSet,
			Force:                           false,
			GracePeriodSeconds:              10,
			IgnoreAllDaemonSets:             true,
			Timeout:                         10 * time.Second,
			DeleteEmptyDirData:              true,
			ChunkSize:                       0,
			DisableEviction:                 false,
			SkipWaitForDeleteTimeoutSeconds: 0,
			Out:                             os.Stdout,
			ErrOut:                          os.Stderr,
		}

		if err := drain.RunNodeDrain(&drainHelper, nodes[i].Name); err != nil {
			fmt.Println("failed to drain the node")
			continue
		}

		if err := a.cloudProvider.ScaleDown(ctx, nodes[i].Name); err != nil {
			fmt.Println("failed to scale down the node", nodes[i].Name)
			continue
		}

		err = a.ClientSet.CoreV1().Nodes().Delete(ctx, nodes[i].Name, metav1.DeleteOptions{})
		if err != nil {
			fmt.Println("failed to delete kuberentes node", nodes[i].Name)
			continue
		}
	}

	return nil
}
