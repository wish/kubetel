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

	batchClient batchv1Client.BatchV1Interface

	queue     workqueue.RateLimitingInterface
	kcdStates map[string]string
}

//NewController Creates a new deyployment controller
func NewController(k8sClient kubernetes.Interface, customCS clientset.Interface, customIF informer.SharedInformerFactory) (*Controller, error) {

	kcdInformer := customIF.Custom().V1().KCDs()
	batchClient := k8sClient.BatchV1()

	c := &Controller{
		kcdcInformer: kcdInformer.Informer(),
		customCS:     customCS,
		kcdcSynced:   kcdInformer.Informer().HasSynced,
		batchClient:  batchClient,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployController"),
		kcdStates:    make(map[string]string),
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

	//TODO: move to config file
	maxRetries := 2

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

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	version := kcd.Status.CurrVersion
	jobName := fmt.Sprintf("deploy-tracker-%s-%s", name, version)

	jobsClient := c.batchClient.Jobs(namespace)

	args := []string{"tracker",
		fmt.Sprintf("--cluster=%s", viper.GetString("cluster")),
		fmt.Sprintf("--region=%s", viper.GetString("region")),
		fmt.Sprintf("--log=%s", viper.GetString("log.level")),
		fmt.Sprintf("--use-config=false"),
		fmt.Sprintf("--tracker-kcdapp=%s", kcd.Spec.Selector["kcdapp"]),
		fmt.Sprintf("--tracker-version=%s", version),
		fmt.Sprintf("--tracker-namespace=%s", namespace),
		fmt.Sprintf("--tracker-endpointtype=%s", viper.GetString("tracker.endpointtype")),
		fmt.Sprintf("--tracker-endpoint=%s", viper.GetString("tracker.endpoint")),
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
		if newKCD.Status.CurrStatus == c.kcdStates[newKCD.Name] {
			return
		}
		c.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	}
	c.enqueue(newObj)

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
