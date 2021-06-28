/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

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
package cmd

import (
	"context"
	"sync"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// deletefailedCmd represents the deletefailed command
var Deletefailed = &cobra.Command{
	Use:   "deletefailed",
	Short: "Delete the non running Pods",
	Run: func(cmd *cobra.Command, args []string) {
		p := getPlatform(cmd)
		client, err := p.GetClientset()
		if err != nil {
			p.Fatalf("unable to get clientset: %v", err)
		}

		// gather the list of Pods from all
		pods, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			p.Fatalf("Failed to gather the list of jobs from namespace %v: %v", namespace, err)
		}

		// WaitGroup to synchronize go routines execution
		wg := sync.WaitGroup{}

		for _, po := range pods.Items {
			wg.Add(1)

			go func(po v1.Pod, clientSet *kubernetes.Clientset) {

				condition := po.Status.Phase

				// if the Condition of the Pod is not running, delete the Pod
				if condition != "Running" {
					p.Infof("Removing failed pod %v from namespace %v. Failed reason: %v", po.Name, po.Namespace, po.Status)
					if err = client.CoreV1().Pods(po.Namespace).Delete(context.TODO(), po.Name, metav1.DeleteOptions{}); err != nil {
						p.Errorf("Failed to delete pod: %v", err)
					}

				}

				wg.Done()
			}(po, client)
		}

		wg.Wait()

	},
}
