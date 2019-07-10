package controller

import (
	"fmt"
	"reflect"
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

	queue workqueue.RateLimitingInterface
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
	defer c.queue.Done(key)

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
	jobName := fmt.Sprintf("kubedeploy-tracker-%s-%s-%s", namespace, name, version)

	jobsClient := c.batchClient.Jobs(namespace)

	//Overwrite config settings set from flags
	args := []string{"tracker", fmt.Sprintf("--log=%s", viper.GetString("log.level")), fmt.Sprintf("--kcdapp=%s", kcd.Spec.Selector["kcdapp"]), fmt.Sprintf("--version=%s", version), fmt.Sprintf("--tracker-namespace=%s", namespace)}

	//If deployment is already being tracked do not create a new tracking job
	_, err = jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: "kubedeploy",
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: jobName,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  fmt.Sprintf("%s-container", jobName),
								Image: viper.GetString("image"),
								Args:  args,
							},
						},
						RestartPolicy: corev1.RestartPolicyOnFailure,
					},
				},
			},
		}
		_, err := jobsClient.Create(job)
		if err != nil {
			return errors.Wrapf(err, "Failed to create job %s", job.Name)
		}
	}
	log.Infof("Job : %s : already exists, skipping", jobName)
	return nil
}

func (c *Controller) trackKcd(oldObj interface{}, newObj interface{}) {
	if !reflect.DeepEqual(oldObj, newObj) {
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
			log.Trace("no change")
			return
		}
		if newKCD.Status.CurrStatus == kcdutil.StatusProgressing {
			log.Trace(newKCD)
			c.enqueue(newObj)
		}
	}
}
func (c *Controller) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	if newKCD.Status.CurrStatus == kcdutil.StatusProgressing {
		log.Trace(newKCD)
		c.enqueue(newObj)
	}
}
