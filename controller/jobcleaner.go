package controller

import (
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchv1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/cache"
)

//JobCleaner is JobCleaner
type JobCleaner struct {
	jobcInformer  cache.SharedIndexInformer
	batchClient   batchv1Client.BatchV1Interface
	completeTTL   int
	failedTTL     int
	deletionSlate map[string]bool
	sync.Mutex
}

//NewJobCleaner cleans old jobs
func NewJobCleaner(jobcInformer cache.SharedIndexInformer, batchClient batchv1Client.BatchV1Interface) *JobCleaner {
	j := &JobCleaner{
		jobcInformer:  jobcInformer,
		batchClient:   batchClient,
		completeTTL:   viper.GetInt("controller.completejobttl"),
		failedTTL:     viper.GetInt("controller.failedjobttl"),
		deletionSlate: make(map[string]bool),
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
	if !strings.Contains(job.Name, "deploy-tracker-") {
		return
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == "Complete" && condition.Status == "True" {
			j.Lock()
			if slated, ok := j.deletionSlate[job.Name]; !ok || !slated {
				log.Tracef("JobCleaner: Job %s complete, slated for deletion in %d minutes", job.Name, j.completeTTL)
				j.deletionSlate[job.Name] = true
				go j.deleteJobAfter(job.Name, job.Namespace, j.completeTTL)
			}
			j.Unlock()
		} else if condition.Type == "Failed" && condition.Status == "True" {
			j.Lock()
			if slated, ok := j.deletionSlate[job.Name]; !ok || !slated {
				log.Tracef("JobCleaner: Job %s failed, slated for deletion in %d minutes", job.Name, j.failedTTL)
				j.deletionSlate[job.Name] = true
				go j.deleteJobAfter(job.Name, job.Namespace, j.failedTTL)
			}
			j.Unlock()
		}
	}
}

func (j *JobCleaner) trackUpdateJob(oldObj interface{}, newObj interface{}) {
	j.cleanAddJob(newObj)
}

func (j *JobCleaner) deleteJobAfter(jobName, jobNamespace string, after int) {
	time.Sleep(time.Duration(after) * time.Minute)

	defer func() {
		j.Lock()
		j.deletionSlate[jobName] = false
		j.Unlock()
	}()

	var gracePeriodSeconds int64
	var propagationPolicy metav1.DeletionPropagation = metav1.DeletePropagationForeground
	jobsClient := j.batchClient.Jobs(jobNamespace)
	exJob, err := jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		for _, condition := range exJob.Status.Conditions {
			if (condition.Type == "Complete" || condition.Type == "Failed") && condition.Status == "True" {
				log.Tracef("JobCleaner: Deleting job : %s", jobName)
				err := jobsClient.Delete(jobName, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					log.Warnf("JobCleaner: Failed to delete job %s with error: %s", jobName, err)
				}
				return
			}
		}
		log.Tracef("JobCleaner: Job %s was slated for deletion but was found running", jobName)
		return
	}
	log.Tracef("JobCleaner: Job %s was already deleted", jobName)
	return
}
