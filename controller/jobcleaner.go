package controller

import (
	"strings"
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
	if !strings.Contains("deploy-tracker-", job.Name) {
		return
	}

	for _, condition := range job.Status.Conditions {
		if condition.Type == "Complete" && condition.Status == "True" {
			go j.deleteJobAfter(job.Name, job.Namespace, 1)
		} else if condition.Type == "Failed" && condition.Status == "True" {
			go j.deleteJobAfter(job.Name, job.Namespace, 5)
		}
	}
}

func (j *JobCleaner) trackUpdateJob(oldObj interface{}, newObj interface{}) {
	j.cleanAddJob(newObj)
}

func (j *JobCleaner) deleteJobAfter(jobName, jobNamespace string, after int) {
	time.Sleep(time.Duration(after) * time.Minute)
	var gracePeriodSeconds int64
	var propagationPolicy metav1.DeletionPropagation = metav1.DeletePropagationForeground
	jobsClient := j.batchClient.Jobs(jobNamespace)
	exJob, err := jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		for _, condition := range exJob.Status.Conditions {
			if (condition.Type == "Complete" || condition.Type == "Failed") && condition.Status == "True" {
				err := jobsClient.Delete(jobName, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					log.Warnf("failed to delete job %s with error: %s", jobName, err)
				}
			}
		}

	}
}
