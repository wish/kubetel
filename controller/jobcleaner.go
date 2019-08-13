package controller

import (
	"time"

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchv1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/cache"
)

//JobCleaner is JobCleaner
type JobCleaner struct {
	jobcInformer cache.SharedIndexInformer
	batchClient  batchv1Client.BatchV1Interface
}

//NewJobCleaner cleans old jobs
func NewJobCleaner(jobcInformer cache.SharedIndexInformer, batchClient batchv1Client.BatchV1Interface) *JobCleaner {

	j := &JobCleaner{
		jobcInformer: jobcInformer,
		batchClient:  batchClient,
	}
	jobcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    j.cleanAddJob,
		UpdateFunc: j.trackUpdateJob,
	})
	return j
}

func (j *JobCleaner) cleanAddJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		log.Errorf("Not a job object")
		return
	}
	var gracePeriodSeconds int64
	var propagationPolicy metav1.DeletionPropagation = metav1.DeletePropagationForeground
	jobsClient := j.batchClient.Jobs(job.Namespace)
	for _, condition := range job.Status.Conditions {
		if condition.Type == "Complete" && condition.Status == "True" {
			go func() {
				time.Sleep(time.Minute)
				err := jobsClient.Delete(job.Name, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					log.Warnf("failed to delete job %s with error: %s", job.Name, err)
				}
			}()
		} else if condition.Type == "Failed" && condition.Status == "True" {
			go func() {
				time.Sleep(5 * time.Minute)
				err := jobsClient.Delete(job.Name, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					log.Warnf("failed to delete job %s with error: %s", job.Name, err)
				}
			}()
		}
	}
}

func (j *JobCleaner) trackUpdateJob(oldObj interface{}, newObj interface{}) {
	j.cleanAddJob(newObj)
}
