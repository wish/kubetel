package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	clientset "github.com/wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	kcdutil "github.com/wish/kubetel/gok8s/kcdutil"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	batchv1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

//Controller object
type Controller struct {
	k8sClient kubernetes.Interface
	customCS  clientset.Interface

	kcdcInformer cache.SharedIndexInformer
	kcdcSynced   cache.InformerSynced

	k8sIF k8sinformers.SharedInformerFactory

	batchClient batchv1Client.BatchV1Interface

	queue     workqueue.RateLimitingInterface
	kcdStates map[string]string

	endpointMap map[string]string
}

//NewController Creates a new deyployment controller
func NewController(k8sClient kubernetes.Interface, customCS clientset.Interface, customIF informer.SharedInformerFactory, k8sIF k8sinformers.SharedInformerFactory) (*Controller, error) {

	kcdInformer := customIF.Custom().V1().KCDs()
	batchClient := k8sClient.BatchV1()

	c := &Controller{
		kcdcInformer: kcdInformer.Informer(),
		customCS:     customCS,
		kcdcSynced:   kcdInformer.Informer().HasSynced,
		k8sIF:        k8sIF,
		batchClient:  batchClient,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployController"),
		kcdStates:    make(map[string]string),
		endpointMap:  viper.GetStringMapString("tracker.appendpoints"),
	}

	c.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddKcd,
		UpdateFunc: c.trackKcd,
	})

	return c, nil
}

//Adds a new job request into the work queue
func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {

		runtime.HandleError(fmt.Errorf("error obtaining key for object being enqueue: %s", err.Error()))
		log.Errorf("Failed to obtain key for object being enqueue: %v", err)
		return
	}
	log.Tracef("Enququing object with key: %s", key)
	c.queue.AddRateLimited(key)
}

// Run starts the controller
func (c *Controller) Run(threadCount int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Controller")

	go c.kcdcInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.kcdcSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}

	log.Info("Cache sync completed")

	for i := 0; i < threadCount; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	log.Info("Started Controller")

	<-stopCh
	log.Info("Shutting down container version controller")
	return nil
}

func (c *Controller) runWorker() {
	log.Info("Running worker")
	for c.processNextItem() {
	}
}

//dequeues workqueue with retry if failed
func (c *Controller) processNextItem() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	c.queue.Done(key)

	err := c.processItem(key.(string))

	maxRetries := viper.GetInt("controller.maxretries")

	if err == nil {
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		log.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		runtime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	obj, exists, err := c.kcdcInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("Object does not exists")
	}
	kcd, ok := obj.(*customv1.KCD)
	if !ok {
		return err
	}

	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	name := kcd.Spec.Selector["kcdapp"]
	version := kcd.Status.CurrVersion
	jobName := fmt.Sprintf("deploy-tracker-%s-%s", name, version)

	//Delete existing job if it already completed
	jobsClient := c.batchClient.Jobs(namespace)
	var gracePeriodSeconds int64
	var propagationPolicy metav1.DeletionPropagation = metav1.DeletePropagationForeground
	exJob, err := jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		for _, condition := range exJob.Status.Conditions {
			if (condition.Type == "Complete" || condition.Type == "Failed") &&
				condition.Status == "True" {
				done := make(chan struct{})
				//Use a shared job informer to check when the Job is deleted
				jobInformer := c.k8sIF.Batch().V1().Jobs().Informer()
				jobInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
					DeleteFunc: func(obj interface{}) {
						job, ok := obj.(*batchv1.Job)
						if !ok {
							log.Errorf("Not a job object")
							return
						}
						//When job is completed close the channel this will end and the informer's goroutine and signal delete complete
						if job.Name == jobName {
							close(done)
						}
					},
				})
				go jobInformer.Run(done)
				log.Info("Deleting job")
				err = jobsClient.Delete(jobName, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					log.Warnf("failed to delete job %s with error: %s", jobName, err)
				}
				//This will block until the done channel is closed by the job informer
				<-done
			}
		}
	}

	//Check if there is a specific endpoint for the app else use default
	var endpoint string
	if endpoint, ok = c.endpointMap[name]; !ok {
		endpoint = viper.GetString("tracker.endpoint")
	}

	args := []string{"tracker",
		fmt.Sprintf("--cluster=%s", viper.GetString("cluster")),
		fmt.Sprintf("--region=%s", viper.GetString("region")),
		fmt.Sprintf("--log=%s", viper.GetString("log.level")),
		fmt.Sprintf("--use-config=false"),
		fmt.Sprintf("--tracker-kcdapp=%s", kcd.Spec.Selector["kcdapp"]),
		fmt.Sprintf("--tracker-version=%s", version),
		fmt.Sprintf("--tracker-namespace=%s", namespace),
		fmt.Sprintf("--tracker-endpointtype=%s", viper.GetString("tracker.endpointtype")),
		fmt.Sprintf("--tracker-endpoint=%s", endpoint),
		fmt.Sprintf("--tracker-maxretries=%d", viper.GetInt("tracker.maxretries")),
		fmt.Sprintf("--tracker-workercount=%d", viper.GetInt("tracker.workercount")),
		fmt.Sprintf("--server-port=%d", viper.GetInt("server.port")),
	}

	//Number of time job will retry upon failure
	var backoffLimit int32 = 10

	log.Infof("Creating job for : %s", jobName)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: jobName,
				},
				Spec: corev1.PodSpec{
					NodeSelector: viper.GetStringMapString("nodeSelector"),
					Containers: []corev1.Container{
						{
							Name:            fmt.Sprintf("%s-container", jobName),
							Image:           viper.GetString("image"),
							Args:            args,
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 443,
									Protocol:      "TCP",
								},
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      "TCP",
								},
							},
						},
					},
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "kubetel-tracker",
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}
	_, err = jobsClient.Create(job)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			log.Infof("Job : %s : already exists, skipping", jobName)
			return nil
		}
		return errors.Wrapf(err, "Failed to create job %s", job.Name)
	}
	return nil
}

func (c *Controller) trackKcd(oldObj interface{}, newObj interface{}) {

	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	oldKCD, ok := oldObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	if oldKCD.Status.CurrStatus == newKCD.Status.CurrStatus {
		return
	}
	if newKCD.Status.CurrStatus == c.kcdStates[newKCD.Name] {
		return
	}
	if newKCD.Status.CurrStatus == kcdutil.StatusProgressing && c.kcdStates[newKCD.Name] != kcdutil.StatusProgressing {
		c.enqueue(newObj)
	}

	c.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus

}

func (c *Controller) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	c.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if newKCD.Status.CurrStatus == kcdutil.StatusProgressing {
		c.enqueue(newObj)
	}
}
