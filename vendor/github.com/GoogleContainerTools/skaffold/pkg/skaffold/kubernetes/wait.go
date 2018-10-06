/*
Copyright 2018 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubernetes

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

func WaitForPodReady(pods corev1.PodInterface, podName string) error {
	logrus.Infof("Waiting for %s to be scheduled", podName)
	err := wait.PollImmediate(time.Millisecond*500, time.Second*30, func() (bool, error) {
		_, err := pods.Get(podName, meta_v1.GetOptions{
			IncludeUninitialized: true,
		})
		if err != nil {
			logrus.Infof("Getting pod %s", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	logrus.Infof("Waiting for %s to be ready", podName)
	return wait.PollImmediate(time.Millisecond*500, time.Minute*10, func() (bool, error) {
		pod, err := pods.Get(podName, meta_v1.GetOptions{
			IncludeUninitialized: true,
		})
		if err != nil {
			return false, fmt.Errorf("not found: %s", podName)
		}
		switch pod.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodSucceeded, v1.PodFailed:
			return false, fmt.Errorf("pod already in terminal phase: %s", pod.Status.Phase)
		case v1.PodUnknown, v1.PodPending:
			return false, nil
		}
		return false, fmt.Errorf("unknown phase: %s", pod.Status.Phase)
	})
}

func WaitForPodComplete(pods corev1.PodInterface, podName string, timeout time.Duration) error {
	logrus.Infof("Waiting for %s to be ready", podName)
	return wait.PollImmediate(time.Millisecond*500, timeout, func() (bool, error) {
		pod, err := pods.Get(podName, meta_v1.GetOptions{
			IncludeUninitialized: true,
		})
		if err != nil {
			logrus.Infof("Getting pod %s", err)
			return false, nil
		}
		switch pod.Status.Phase {
		case v1.PodSucceeded:
			return true, nil
		case v1.PodRunning:
			return false, nil
		case v1.PodFailed:
			return false, fmt.Errorf("pod already in terminal phase: %s", pod.Status.Phase)
		case v1.PodUnknown, v1.PodPending:
			return false, nil
		}
		return false, fmt.Errorf("unknown phase: %s", pod.Status.Phase)
	})
}

// WaitForDeploymentToStabilize waits till the Deployment has a matching generation/replica count between spec and status.
func WaitForDeploymentToStabilize(c kubernetes.Interface, ns, name string, timeout time.Duration) error {
	options := meta_v1.ListOptions{FieldSelector: fields.Set{
		"metadata.name":      name,
		"metadata.namespace": ns,
	}.AsSelector().String()}
	w, err := c.AppsV1().Deployments(ns).Watch(options)
	if err != nil {
		return err
	}
	_, err = watch.Until(timeout, w, func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Deleted:
			return false, apierrs.NewNotFound(schema.GroupResource{Resource: "deployments"}, "")
		}
		switch dp := event.Object.(type) {
		case *appsv1.Deployment:
			if dp.Name == name && dp.Namespace == ns &&
				dp.Generation <= dp.Status.ObservedGeneration &&
				*(dp.Spec.Replicas) == dp.Status.Replicas {
				return true, nil
			}
			glog.Infof("Waiting for deployment %s to stabilize, generation %v observed generation %v spec.replicas %d status.replicas %d",
				name, dp.Generation, dp.Status.ObservedGeneration, *(dp.Spec.Replicas), dp.Status.Replicas)
		}
		return false, nil
	})
	return err
}
