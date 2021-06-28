package cmd

import (
	"context"
	"sync"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/batch/v1"
	v1p "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var Cleanup = &cobra.Command{
	Use:   "cleanup",
	Short: "remove all failed jobs or pods",
}

func init() {
	// CleanupJobs removes all failed jobs in a given namespace
	Jobs := &cobra.Command{
		Use:       "jobs",
		Short:     "remove all failed jobs in a given namespace",
		ValidArgs: []string{"jobs"},
		Args:      cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {

			namespace, _ := cmd.Flags().GetString("namespace")

			if namespace == "" {
				namespace = metav1.NamespaceAll
			}

			p := getPlatform(cmd)
			clientSet, err := p.GetClientset()
			if err != nil {
				p.Fatalf("Failed to create the new k8s client: %v", err)
			}

			// gather the list of jobs from a namespace
			jobs, err := clientSet.BatchV1().Jobs(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				p.Fatalf("Failed to gather the list of jobs from namespace %v: %v", namespace, err)
			}

			// WaitGroup to synchronize go routines execution
			wg := sync.WaitGroup{}

			// loop through the list of jobs found
			for _, j := range jobs.Items {
				wg.Add(1)

				go func(j v1.Job, clientSet *kubernetes.Clientset) {

					// loop through the job object's status conditions
					for _, conditions := range j.Status.Conditions {

						// if the type of the JobCondition is equal to "Failed", delete the job
						if conditions.Type == "Failed" {
							p.Infof("Removing failed job %v from namespace %v. Failed reason: %v", j.Name, j.Namespace, conditions.Reason)
							if err = clientSet.BatchV1().Jobs(namespace).Delete(context.TODO(), j.Name, metav1.DeleteOptions{}); err != nil {
								p.Errorf("Failed to delete job: %v", err)
							}
							break
						}
					}
					wg.Done()
				}(j, clientSet)
			}

			wg.Wait()
		},
	}

	Pods := &cobra.Command{
		Use:       "pods",
		Short:     "Delete non running Pods",
		ValidArgs: []string{"jobs"},
		Args:      cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			p := getPlatform(cmd)
			client, err := p.GetClientset()
			if err != nil {
				p.Fatalf("unable to get clientset: %v", err)
			}

			if namespace == "" {
				namespace = metav1.NamespaceAll
			}

			// gather the list of Pods from all
			pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				p.Fatalf("Failed to gather the list of jobs from namespace %v: %v", namespace, err)
			}

			// WaitGroup to synchronize go routines execution
			wg := sync.WaitGroup{}

			for _, po := range pods.Items {
				wg.Add(1)

				go func(po v1p.Pod, clientSet *kubernetes.Clientset) {

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

	Jobs.Flags().String("namespace", "", "Namespace to cleanup failed jobs.")
	Pods.Flags().String("namespace", "", "Namespace to cleanup failed jobs.")
	Cleanup.AddCommand(Jobs, Pods)
}
