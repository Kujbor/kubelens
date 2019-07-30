package k8v1

import (
	"fmt"
	"sync"

	"github.com/kubelens/kubelens/api/errs"

	klog "github.com/kubelens/kubelens/api/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodDetail returns details for a pod
func (k *Client) PodDetail(options PodDetailOptions) (po *PodDetail, apiErr *errs.APIError) {
	po = &PodDetail{}

	clientset, err := k.wrapper.GetClientSet()

	if err != nil {
		klog.Trace()
		return nil, errs.InternalServerError(err.Error())
	}

	pod, err := clientset.CoreV1().Pods(options.Namespace).Get(options.Name, metav1.GetOptions{
		IncludeUninitialized: true,
	})

	if !options.UserCanAccess(pod.GetLabels()) {
		return nil, errs.Forbidden()
	}

	// po.Set(*pod, options.UserCanAccess)
	var st string
	if pod.Status.StartTime != nil {
		st = pod.Status.StartTime.String()
	}

	// remove environment variables for containers
	// that might use env vars for secrets. This could be added back
	// auth roles and such is figured out better.
	spec := &pod.Spec
	for i := range spec.Containers {
		if !options.UserRole.HasEnvVarsAccess(pod.GetLabels()) {
			spec.Containers[i].Env = nil
		}
	}

	// add the current pod
	po.Name = pod.GetName()
	po.Namespace = pod.GetNamespace()
	po.HostIP = pod.Status.HostIP
	po.PodIP = pod.Status.PodIP
	po.StartTime = st
	po.Phase = pod.Status.Phase
	po.PhaseMessage = pod.Status.Message
	po.ContainerStatus = pod.Status.ContainerStatuses
	po.Status = pod.Status
	po.Spec = *spec

	return po, nil
}

// PodOverview returns an overview of pods related to an application
func (k *Client) PodOverview(options PodOverviewOptions) (po *PodOverview, apiErr *errs.APIError) {
	if !options.UserCanAccess() {
		return nil, errs.Forbidden()
	}

	clientset, err := k.wrapper.GetClientSet()

	if err != nil {
		klog.Trace()
		return nil, errs.InternalServerError(err.Error())
	}

	list, err := clientset.CoreV1().Pods(options.Namespace).List(metav1.ListOptions{
		LabelSelector:        fmt.Sprintf("%s=%s", options.AppNameLabelKey, options.AppName),
		IncludeUninitialized: true,
		Limit:                options.GetLimit(),
	})

	if err != nil {
		return nil, errs.InternalServerError(err.Error())
	}

	po = &PodOverview{
		PodDetails: []*PodDetail{},
	}

	wg := sync.WaitGroup{}

	wg.Add(len(list.Items))

	for i, pod := range list.Items {
		go func(index int, pod v1.Pod) {
			defer wg.Done()
			if options.UserRole.HasNamespaceAccess(pod.GetNamespace()) &&
				options.UserRole.HasPodAccess(pod.GetLabels()) &&
				options.UserRole.Matches(pod.GetLabels(), &options.AppName) {
				// set common overivew fields on first pass
				if index == 0 {
					// try get the app/component name
					name, labelKey := getAppName(
						pod.GetLabels(),
						options.AppNameLabelKey,
						getDefaultSearchLabel(nil),
						pod.GetName(),
					)

					// set app overview and initialize pod quickview
					po.Name = Name{
						LabelKey: labelKey,
						Value:    name,
					}
					po.Namespace = pod.GetNamespace()
					po.ClusterName = pod.GetClusterName()
					po.DeployerLink = getDeployerLink(pod.GetName())
				}

				var st string
				if pod.Status.StartTime != nil {
					st = pod.Status.StartTime.String()
				}

				// remove environment variables for containers
				// that might use env vars for secrets. This could be added back
				// auth roles and such is figured out better.
				spec := &pod.Spec
				for i := range spec.Containers {
					if !options.UserRole.HasEnvVarsAccess(pod.GetLabels()) {
						spec.Containers[i].Env = nil
					}
				}

				// add the current pod
				po.PodDetails = append(po.PodDetails, &PodDetail{
					Name:            pod.GetName(),
					Namespace:       pod.GetNamespace(),
					HostIP:          pod.Status.HostIP,
					PodIP:           pod.Status.PodIP,
					StartTime:       st,
					Phase:           pod.Status.Phase,
					PhaseMessage:    pod.Status.Message,
					ContainerStatus: pod.Status.ContainerStatuses,
					Status:          pod.Status,
					Spec:            *spec,
				})
			}
		}(i, pod)
	}

	wg.Wait()

	return po, nil
}